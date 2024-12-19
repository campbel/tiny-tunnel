package server

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"github.com/campbel/tiny-tunnel/log"
	"github.com/campbel/tiny-tunnel/sync"
	"github.com/campbel/tiny-tunnel/types"
	"github.com/campbel/tiny-tunnel/util"
	"golang.org/x/crypto/acme/autocert"
	"golang.org/x/net/websocket"
)

var (
	AllowIPHeader = http.CanonicalHeaderKey("X-TT-Allow-IP")
)

type Handler struct {
	hostname   string
	tunnels    *sync.Map[string, *Tunnel]
	baseRouter *http.ServeMux
}

func NewHandler(hostname string) *Handler {
	dict := sync.NewMap[string, *Tunnel]()
	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, "Welcome to Tiny Tunnel. See github.com/campbel/tiny-tunnel for more info.")
	})
	mux.HandleFunc("/register", func(w http.ResponseWriter, r *http.Request) {
		name := r.FormValue("name")
		if name == "" {
			http.Error(w, "name is required", http.StatusBadRequest)
			return
		}
		tunnel := NewTunnel(name, r.Header[AllowIPHeader])
		if !dict.SetNX(name, tunnel) {
			http.Error(w, "name is already used", http.StatusBadRequest)
			return
		}
		log.Info("registered tunnel", "name", name)
		createWebSocketHandler(tunnel).ServeHTTP(w, r)
		dict.Delete(name)
		log.Info("unregistered tunnel", "name", name)
	})

	return &Handler{
		hostname:   hostname,
		tunnels:    dict,
		baseRouter: mux,
	}
}

func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	name, ok := util.GetSubdomain(r, h.hostname)
	if !ok {
		h.baseRouter.ServeHTTP(w, r)
		return
	}
	if tunnel, ok := h.tunnels.Get(name); ok {
		if !util.AllowedIP(r, tunnel.AllowedIPs) {
			http.Error(w, "gtfo", http.StatusForbidden)
			return
		}
		// Determine if this is a websocket request
		if r.Header.Get("Upgrade") == "websocket" {

			responseChan := make(chan (types.Message))
			tunnel.Send(
				types.MessageKindWebsocketCreateRequest,
				types.WebsocketCreateRequest{
					Path:    r.URL.Path,
					Headers: r.Header,
					Origin:  r.Header.Get("Origin"),
				}.JSON(),
				responseChan,
			)
			responseMessage := <-responseChan
			if responseMessage.Kind != types.MessageKindWebsocketCreateResponse {
				http.Error(w, "there was an error processing your request", http.StatusInternalServerError)
				return
			}
			response := types.LoadWebsocketCreateResponse(responseMessage.Payload)
			log.Info("websocket create response", "session_id", response.SessionID)
			websocket.Handler(func(ws *websocket.Conn) {
				if !tunnel.WSSessions.SetNX(response.SessionID, ws) {
					log.Info("failed to set websocket session", "session_id", response.SessionID)
					return
				}
				// Listen for messages
				for {
					var buffer []byte
					if err := websocket.Message.Receive(ws, &buffer); err != nil {
						break
					}
					tunnel.Send(
						types.MessageKindWebsocketMessage,
						types.WebsocketMessage{
							SessionID: response.SessionID,
							Data:      buffer,
						}.JSON(),
						nil,
					)
				}
			}).ServeHTTP(w, r)

			return
		}

		// Plain HTTP request
		responseChan := make(chan (types.Message))
		tunnel.Send(
			types.MessageKindRequest,
			types.HTTPRequest{
				Method:    r.Method,
				Path:      r.URL.Path + "?" + r.URL.Query().Encode(),
				Headers:   r.Header,
				Body:      util.MustRead(r.Body),
				CreatedAt: time.Now(),
			}.JSON(),
			responseChan,
		)
		responseMessage := <-responseChan
		if responseMessage.Kind != types.MessageKindResponse {
			http.Error(w, "there was an error processing your request", http.StatusInternalServerError)
			return
		}
		response := types.LoadResponse(responseMessage.Payload)
		if response.Error != "" {
			http.Error(w, "there was an error processing your request", http.StatusInternalServerError)
			return
		}
		for k, v := range response.Headers {
			for _, v := range v {
				w.Header().Add(k, v)
			}
		}
		w.WriteHeader(response.Status)
		w.Write(response.Body)
		return
	}
	http.Error(w, "the specified service is unavailable", http.StatusServiceUnavailable)
}

func createWebSocketHandler(tunnel *Tunnel) http.Handler {
	return websocket.Handler(func(ws *websocket.Conn) {
		done := make(chan bool)
		// Read responses
		go func() {
			defer func() {
				done <- true
				log.Info("closing reads", "name", tunnel.ID)
			}()
			for {
				buffer := make([]byte, 1024)
				if err := websocket.Message.Receive(ws, &buffer); err != nil {
					log.Info("error reading response", "err", err.Error(), "name", tunnel.ID)
					return
				}
				message := types.LoadMessage(buffer)
				switch message.Kind {
				case types.MessageKindWebsocketCreateResponse:
					if responseChan, ok := tunnel.Responses.Get(message.ID); !ok {
						log.Info("response undeliverable", "id", message.ID, "name", tunnel.ID)
					} else {
						responseChan <- message
						tunnel.Responses.Delete(message.ID)
					}
				case types.MessageKindWebsocketMessage:
					message := types.LoadWebsocketMessage(message.Payload)
					wsConn, ok := tunnel.WSSessions.Get(message.SessionID)
					if !ok {
						log.Info("failed to get websocket connection", "session_id", message.SessionID)
						return
					}
					if err := websocket.Message.Send(wsConn, message.Data); err != nil {
						log.Info("failed to send message to websocket", "error", err.Error())
					}

				case types.MessageKindResponse:
					if responseChan, ok := tunnel.Responses.Get(message.ID); !ok {
						log.Info("response undeliverable", "id", message.ID, "name", tunnel.ID)
					} else {
						responseChan <- message
						tunnel.Responses.Delete(message.ID)
					}
				}
			}
		}()

		// Write messages
	LOOP:
		for {
			select {
			case <-done:
				break LOOP
			case msg := <-tunnel.sendChannel:
				if err := websocket.Message.Send(ws, msg.JSON()); err != nil {
					log.Info("error writing request", "err", err, "name", tunnel.ID)
					break LOOP
				}
			}
		}
		log.Info("closing writes", "name", tunnel.ID)
	})
}

func Serve(ctx context.Context, options ServeOptions) error {
	log.Info("starting server", "options", options)

	router := NewHandler(options.Hostname)

	server := &http.Server{
		Addr:    ":" + options.Port,
		Handler: router,
	}

	// automatic certificate creation for https
	if options.LetsEncrypt {
		m := &autocert.Manager{
			Cache:  autocert.DirCache("secret-dir"),
			Prompt: autocert.AcceptTOS,
			Email:  "campbel@hey.com",
		}
		server.TLSConfig = m.TLSConfig()
		go func() {
			if err := server.ListenAndServeTLS("", ""); err != nil {
				log.Error("error starting server", "err", err)
			}
		}()
	} else {
		go func() {
			if err := server.ListenAndServe(); err != nil {
				log.Error("error starting server", "err", err)
			}
		}()
	}

	<-ctx.Done()
	log.Info("shutting down server")
	shutdownCtx, _ := context.WithTimeout(context.Background(), 5*time.Second)
	return server.Shutdown(shutdownCtx)
}

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

	gws "github.com/gorilla/websocket"
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

		if err := tunnel.Run(w, r); err != nil {
			log.Error("error starting tunnel", "err", err)
		}

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
			http.Error(w, "", http.StatusForbidden)
			return
		}
		// Determine if this is a websocket request
		if r.Header.Get("Upgrade") == "websocket" {

			log.Info("requesting websocket create", "tunnel", tunnel.ID)
			responseChan := make(chan (types.Message))
			if err := tunnel.Send(
				types.MessageKindWebsocketCreateRequest,
				types.WebsocketCreateRequest{
					Path:    r.URL.Path,
					Headers: r.Header,
					Origin:  r.Header.Get("Origin"),
				}.JSON(),
				responseChan,
			); err != nil {
				http.Error(w, "there was an error processing your request", http.StatusInternalServerError)
				return
			}
			responseMessage := <-responseChan
			if responseMessage.Kind != types.MessageKindWebsocketCreateResponse {
				http.Error(w, "there was an error processing your request", http.StatusInternalServerError)
				return
			}
			response := types.LoadWebsocketCreateResponse(responseMessage.Payload)
			sessionID := response.SessionID

			upgrader := gws.Upgrader{
				CheckOrigin: func(r *http.Request) bool {
					// TODO: Implement a more secure check
					return true
				},
			}
			rawConn, err := upgrader.Upgrade(w, r, nil)
			if err != nil {
				return
			}
			defer rawConn.Close()

			conn := sync.NewWSConn(rawConn)

			if !tunnel.WSSessions.SetNX(sessionID, conn) {
				log.Info("failed to set websocket session", "session_id", sessionID)
				return
			}

			// Listen for messages
			for {

				mt, data, err := conn.ReadMessage()
				if err != nil {
					log.Info("error reading message", "err", err.Error(), "name", tunnel.ID, "session_id", sessionID)
					break
				}

				var wsMsg types.WebsocketMessage
				switch mt {
				case gws.BinaryMessage:
					wsMsg = types.NewBinaryWebsocketMessage(sessionID, data)
				case gws.TextMessage:
					wsMsg = types.NewStringWebsocketMessage(sessionID, string(data))
				}

				tunnel.Send(
					types.MessageKindWebsocketMessage,
					wsMsg.JSON(),
					nil,
				)
			}

			return
		}

		// Plain HTTP request
		responseChan := make(chan (types.Message))
		tunnel.Send(
			types.MessageKindHttpRequest,
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
		if responseMessage.Kind != types.MessageKindHttpResponse {
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

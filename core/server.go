package core

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/campbel/tiny-tunnel/internal/log"
	"github.com/campbel/tiny-tunnel/internal/sync"
	"github.com/gorilla/mux"
	"github.com/gorilla/websocket"
)

///
/// Server Handler
///

type ServerHandler struct {
	options  ServerOptions
	upgrader websocket.Upgrader
	tunnels  *sync.Map[string, *ServerTunnel]
}

func NewServerHandler(options ServerOptions) http.Handler {
	server := &ServerHandler{
		options: options,
		upgrader: websocket.Upgrader{
			CheckOrigin: func(r *http.Request) bool {
				return true
			},
		},
		tunnels: sync.NewMap[string, *ServerTunnel](),
	}

	router := mux.NewRouter()
	router.Host(fmt.Sprintf("{tunnel:[a-z]+}.%s", options.Hostname)).HandlerFunc(server.HandleTunnelRequest)
	router.HandleFunc("/register", server.HandleRegister)
	router.HandleFunc("/", server.HandleRoot)

	return router
}

func (s *ServerHandler) HandleRoot(w http.ResponseWriter, r *http.Request) {
	if r.Header.Get("X-TT-Tunnel") != "" {
		s.HandleTunnelRequest(w, r)
		return
	}

	fmt.Fprint(w, "Welcome to Tiny Tunnel. See github.com/campbel/tiny-tunnel for more info.")
}

func (s *ServerHandler) HandleRegister(w http.ResponseWriter, r *http.Request) {
	name := r.FormValue("name")
	if name == "" {
		http.Error(w, "name is required", http.StatusBadRequest)
		return
	}

	conn, err := s.upgrader.Upgrade(w, r, nil)
	if err != nil {
		http.Error(w, "", http.StatusInternalServerError)
		return
	}

	tunnel := NewServerTunnel(conn)
	if !s.tunnels.SetNX(name, tunnel) {
		http.Error(w, "name is already used", http.StatusBadRequest)
		return
	}
	log.Info("registered tunnel", "name", name)

	tunnel.Start(r.Context())

	s.tunnels.Delete(name)
	log.Info("unregistered tunnel", "name", name)
}

func (s *ServerHandler) HandleTunnelRequest(w http.ResponseWriter, r *http.Request) {
	tunnelID := mux.Vars(r)["tunnel"]
	if tunnelID == "" {
		tunnelID = r.Header.Get("X-TT-Tunnel")
	}

	if tunnelID == "" {
		http.Error(w, "tunnel name not provided", http.StatusBadRequest)
		return
	}

	tunnel, ok := s.tunnels.Get(tunnelID)
	if !ok {
		http.Error(w, "tunnel not found", http.StatusNotFound)
		return
	}
	tunnel.HandleHttpRequest(w, r)
}

///
/// Server Tunnel
///

type ServerTunnel struct {
	tunnel         *Tunnel
	websocketConns *sync.Map[string, *sync.WSConn]
}

func NewServerTunnel(conn *websocket.Conn) *ServerTunnel {
	server := &ServerTunnel{
		tunnel:         NewTunnel(conn),
		websocketConns: sync.NewMap[string, *sync.WSConn](),
	}

	server.tunnel.RegisterWebsocketMessageHandler(func(tunnel *Tunnel, id string, payload WebsocketMessagePayload) {
		log.Debug("handling websocket message", "payload", payload)
		conn, ok := server.websocketConns.Get(payload.SessionID)
		if !ok {
			return
		}
		err := conn.WriteMessage(payload.Kind, payload.Data)
		if err != nil {
			log.Error("failed to write websocket message", "error", err.Error())
		}
	})

	server.tunnel.RegisterWebsocketCloseHandler(func(tunnel *Tunnel, id string, payload WebsocketClosePayload) {
		log.Debug("handling websocket close", "payload", payload)
		conn, ok := server.websocketConns.Get(payload.SessionID)
		if !ok {
			return
		}
		if err := conn.Close(); err != nil {
			log.Error("failed to close websocket connection", "error", err.Error(), "payload", payload)
		}
		server.websocketConns.Delete(payload.SessionID)
	})

	return server
}

func (s *ServerTunnel) Start(ctx context.Context) {
	s.tunnel.Listen(ctx)
}

func (s *ServerTunnel) Stop() {
	s.tunnel.Close()
}

func (s *ServerTunnel) HandleHttpRequest(w http.ResponseWriter, r *http.Request) {
	if r.Header.Get("Upgrade") == "websocket" {
		s.HandleWebsocketRequest(w, r)
		return
	}

	responseChannel := make(chan Message)

	bodyBytes, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, "", http.StatusInternalServerError)
		return
	}

	start := time.Now()
	s.tunnel.Send(MessageKindHttpRequest, &HttpRequestPayload{
		Method:  r.Method,
		Path:    r.URL.Path + "?" + r.URL.Query().Encode(),
		Headers: r.Header,
		Body:    bodyBytes,
	}, responseChannel)

	response := <-responseChannel

	// If the response is not a HttpResponse, we need to return an error
	if response.Kind != MessageKindHttpResponse {
		http.Error(w, "", http.StatusInternalServerError)
		return
	}

	var responsePayload HttpResponsePayload
	if err := json.Unmarshal(response.Payload, &responsePayload); err != nil {
		http.Error(w, "", http.StatusInternalServerError)
		return
	}
	log.Debug("received response", "duration", time.Since(start), "status", responsePayload.Response.Status)

	for k, v := range responsePayload.Response.Headers {
		for _, vv := range v {
			w.Header().Add(k, vv)
		}
	}

	w.WriteHeader(responsePayload.Response.Status)
	w.Write(responsePayload.Response.Body)
}

func (s *ServerTunnel) HandleWebsocketRequest(w http.ResponseWriter, r *http.Request) {
	upgrader := websocket.Upgrader{
		CheckOrigin: func(r *http.Request) bool {
			return true
		},
	}

	rawConn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		http.Error(w, "", http.StatusInternalServerError)
		return
	}

	conn := sync.NewWSConn(rawConn)

	responseChannel := make(chan Message)
	s.tunnel.Send(MessageKindWebsocketCreateRequest, &WebsocketCreateRequestPayload{
		Origin: r.Header.Get("Origin"),
		Path:   r.URL.Path,
	}, responseChannel)

	response := <-responseChannel

	if response.Kind != MessageKindWebsocketCreateResponse {
		http.Error(w, "", http.StatusInternalServerError)
		return
	}

	var responsePayload WebsocketCreateResponsePayload
	if err := json.Unmarshal(response.Payload, &responsePayload); err != nil {
		http.Error(w, "", http.StatusInternalServerError)
		return
	}

	if !s.websocketConns.SetNX(responsePayload.SessionID, conn) {
		http.Error(w, "", http.StatusInternalServerError)
		return
	}
	defer func() {
		s.websocketConns.Delete(responsePayload.SessionID)
		conn.Close()
	}()

	for {
		messageType, message, err := conn.ReadMessage()
		if err != nil {
			log.Debug("app websocket connection closed", "sessionID", responsePayload.SessionID, "error", err.Error())
			if err := s.tunnel.Send(MessageKindWebsocketClose, &WebsocketClosePayload{
				SessionID: responsePayload.SessionID,
			}); err != nil {
				log.Error("failed to send websocket close", "error", err.Error())
			}
			return
		}

		if err := s.tunnel.Send(MessageKindWebsocketMessage, &WebsocketMessagePayload{
			SessionID: responsePayload.SessionID,
			Kind:      messageType,
			Data:      message,
		}); err != nil {
			if err == websocket.ErrCloseSent {
				conn.Close()
				log.Debug("tunnel websocket connection closed", "sessionID", responsePayload.SessionID, "error", err.Error())
				return
			}
			log.Error("failed to send websocket message", "error", err.Error())
			continue
		}
	}
}

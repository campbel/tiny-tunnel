package core

import (
	"encoding/json"
	"io"
	"net/http"

	"github.com/campbel/tiny-tunnel/log"
	"github.com/campbel/tiny-tunnel/sync"
	"github.com/gorilla/websocket"
)

type ServerTunnel struct {
	tunnel         *Tunnel
	websocketConns map[string]*sync.WSConn
}

func NewServerTunnel(conn *websocket.Conn) *ServerTunnel {
	server := &ServerTunnel{
		tunnel:         NewTunnel(conn),
		websocketConns: make(map[string]*sync.WSConn),
	}

	server.tunnel.SetWebsocketMessageHandler(func(tunnel *Tunnel, id string, payload WebsocketMessagePayload) {
		conn, ok := server.websocketConns[payload.SessionID]
		if !ok {
			return
		}
		err := conn.WriteMessage(payload.Kind, payload.Data)
		if err != nil {
			log.Error("failed to write websocket message", "error", err.Error())
		}
	})

	return server
}

func (s *ServerTunnel) Connect() {
	s.tunnel.Run()
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

	s.tunnel.Send(MessageKindHttpRequest, &HttpRequestPayload{
		Method:  r.Method,
		Path:    r.URL.Path,
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

	s.websocketConns[responsePayload.SessionID] = conn
	defer func() {
		delete(s.websocketConns, responsePayload.SessionID)
	}()

	for {
		messageType, message, err := conn.ReadMessage()
		if err != nil {
			break
		}

		s.tunnel.Send(MessageKindWebsocketMessage, &WebsocketMessagePayload{
			Kind: messageType,
			Data: message,
		})
	}
}

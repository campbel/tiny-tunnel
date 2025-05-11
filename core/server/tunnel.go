package server

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/campbel/tiny-tunnel/internal/log"
	"github.com/campbel/tiny-tunnel/internal/safe"

	"github.com/campbel/tiny-tunnel/core/protocol"
	"github.com/campbel/tiny-tunnel/core/shared"

	"github.com/gorilla/websocket"
)

type Tunnel struct {
	tunnel         *shared.Tunnel
	websocketConns *safe.Map[string, *safe.WSConn]
}

type TunnelOptions struct {
	HelloMessage string
}

func NewTunnel(conn *websocket.Conn, options TunnelOptions) *Tunnel {
	server := &Tunnel{
		tunnel:         shared.NewTunnel(conn),
		websocketConns: safe.NewMap[string, *safe.WSConn](),
	}

	if options.HelloMessage != "" {
		server.tunnel.Send(protocol.MessageKindText, &protocol.TextPayload{
			Text: options.HelloMessage,
		})
	}

	ticker := time.NewTicker(15 * time.Second)
	go func() {
		for range ticker.C {
			if server.tunnel.IsClosed() {
				return
			}
			server.tunnel.Send(protocol.MessageKindText, &protocol.TextPayload{
				Text: "ping",
			})
		}
	}()

	server.tunnel.RegisterTextHandler(func(tunnel *shared.Tunnel, id string, payload protocol.TextPayload) {
		if payload.Text == "pong" {
			log.Debug("received pong", "id", id)
			return
		}
		log.Debug("handling text message", "payload", payload)
	})

	server.tunnel.RegisterWebsocketMessageHandler(func(tunnel *shared.Tunnel, id string, payload protocol.WebsocketMessagePayload) {
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

	server.tunnel.RegisterWebsocketCloseHandler(func(tunnel *shared.Tunnel, id string, payload protocol.WebsocketClosePayload) {
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

func (s *Tunnel) Listen(ctx context.Context) {
	s.tunnel.Listen(ctx)
}

func (s *Tunnel) Close() {
	s.tunnel.Close()
}

// HandleSSERequest handles Server-Sent Events connections
// It establishes a streaming connection from client to server
func (s *Tunnel) HandleSSERequest(w http.ResponseWriter, r *http.Request) {
	// Set SSE headers
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("Access-Control-Allow-Origin", "*")

	// Flush headers immediately
	if f, ok := w.(http.Flusher); ok {
		f.Flush()
	} else {
		http.Error(w, "Streaming not supported", http.StatusInternalServerError)
		return
	}

	// Create response channel for initial setup
	responseChannel := make(chan protocol.Message)

	// Notify client about the SSE request
	s.tunnel.Send(protocol.MessageKindSSERequest, &protocol.SSERequestPayload{
		Path:    r.URL.Path + "?" + r.URL.Query().Encode(),
		Headers: r.Header,
	}, responseChannel)

	for response := range responseChannel {
		if response.Kind != protocol.MessageKindSSEMessage {
			http.Error(w, "", http.StatusInternalServerError)
			return
		}

		var sseMessage protocol.SSEMessagePayload
		if err := json.Unmarshal(response.Payload, &sseMessage); err != nil {
			http.Error(w, "", http.StatusInternalServerError)
			return
		}

		log.Debug("received SSE message", "data", sseMessage.Data)
		fmt.Fprintf(w, sseMessage.Data+"\n\n")
		if f, ok := w.(http.Flusher); ok {
			f.Flush()
		}
	}
	log.Debug("SSE connection closed")
}

func (s *Tunnel) HandleHttpRequest(w http.ResponseWriter, r *http.Request) {
	// Handle WebSocket requests
	if r.Header.Get("Upgrade") == "websocket" {
		s.HandleWebsocketRequest(w, r)
		return
	}

	// Detect SSE requests by Accept header or conventional path suffixes
	acceptHeader := r.Header.Get("Accept")
	if acceptHeader == "text/event-stream" ||
		strings.HasSuffix(r.URL.Path, "/events") ||
		strings.HasSuffix(r.URL.Path, "/sse") {
		log.Debug("detected SSE request", "path", r.URL.Path, "accept", acceptHeader)
		s.HandleSSERequest(w, r)
		return
	}

	// Process regular HTTP requests
	responseChannel := make(chan protocol.Message)

	bodyBytes, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, "", http.StatusInternalServerError)
		return
	}

	start := time.Now()
	s.tunnel.Send(protocol.MessageKindHttpRequest, &protocol.HttpRequestPayload{
		Method:  r.Method,
		Path:    r.URL.Path + "?" + r.URL.Query().Encode(),
		Headers: r.Header,
		Body:    bodyBytes,
	}, responseChannel)

	response := <-responseChannel

	// If the response is not a HttpResponse, we need to return an error
	if response.Kind != protocol.MessageKindHttpResponse {
		http.Error(w, "", http.StatusInternalServerError)
		return
	}

	var responsePayload protocol.HttpResponsePayload
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

func (s *Tunnel) HandleWebsocketRequest(w http.ResponseWriter, r *http.Request) {
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

	conn := safe.NewWSConn(rawConn)

	responseChannel := make(chan protocol.Message)
	s.tunnel.Send(protocol.MessageKindWebsocketCreateRequest, &protocol.WebsocketCreateRequestPayload{
		Origin: r.Header.Get("Origin"),
		Path:   r.URL.Path,
	}, responseChannel)

	response := <-responseChannel

	if response.Kind != protocol.MessageKindWebsocketCreateResponse {
		http.Error(w, "", http.StatusInternalServerError)
		return
	}

	var responsePayload protocol.WebsocketCreateResponsePayload
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
			if err := s.tunnel.Send(protocol.MessageKindWebsocketClose, &protocol.WebsocketClosePayload{
				SessionID: responsePayload.SessionID,
			}); err != nil {
				log.Error("failed to send websocket close", "error", err.Error())
			}
			return
		}

		if err := s.tunnel.Send(protocol.MessageKindWebsocketMessage, &protocol.WebsocketMessagePayload{
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

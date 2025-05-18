package server

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
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
	l              log.Logger
}

type TunnelOptions struct {
	HelloMessage string
}

func NewTunnel(conn *websocket.Conn, options TunnelOptions, l log.Logger) *Tunnel {
	server := &Tunnel{
		tunnel:         shared.NewTunnel(conn, l),
		websocketConns: safe.NewMap[string, *safe.WSConn](),
		l:              l,
	}

	if options.HelloMessage != "" {
		if err := server.tunnel.Send(protocol.MessageKindText, &protocol.TextPayload{
			Text: options.HelloMessage,
		}); err != nil {
			l.Error("failed to send hello message", "error", err.Error())
		}
	}

	ticker := time.NewTicker(15 * time.Second)
	go func() {
		for range ticker.C {
			if server.tunnel.IsClosed() {
				return
			}
			if err := server.tunnel.Send(protocol.MessageKindText, &protocol.TextPayload{
				Text: "ping",
			}); err != nil {
				l.Error("failed to send ping message", "error", err.Error())
			}
		}
	}()

	server.tunnel.RegisterTextHandler(func(tunnel *shared.Tunnel, id string, payload protocol.TextPayload) {
		if payload.Text == "pong" {
			l.Debug("received pong", "id", id)
			return
		}
		l.Debug("handling text message", "payload", payload)
	})

	server.tunnel.RegisterWebsocketMessageHandler(func(tunnel *shared.Tunnel, id string, payload protocol.WebsocketMessagePayload) {
		l.Debug("handling websocket message", "payload", payload)
		conn, ok := server.websocketConns.Get(payload.SessionID)
		if !ok {
			return
		}
		err := conn.WriteMessage(payload.Kind, payload.Data)
		if err != nil {
			l.Error("failed to write websocket message", "error", err.Error())
		}
	})

	server.tunnel.RegisterWebsocketCloseHandler(func(tunnel *shared.Tunnel, id string, payload protocol.WebsocketClosePayload) {
		l.Debug("handling websocket close", "payload", payload)
		conn, ok := server.websocketConns.Get(payload.SessionID)
		if !ok {
			return
		}
		if err := conn.Close(); err != nil {
			l.Error("failed to close websocket connection", "error", err.Error(), "payload", payload)
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

	// Create buffered response channel for better ordering
	responseChannel := make(chan protocol.Message, 100) // Buffer size to reduce chances of out-of-order delivery

	// Notify client about the SSE request
	clean, err := s.tunnel.SendWithResponseChannel(protocol.MessageKindSSERequest, &protocol.SSERequestPayload{
		Path:    r.URL.Path + "?" + r.URL.Query().Encode(),
		Headers: r.Header,
	}, responseChannel)
	if err != nil {
		s.l.Error("failed to send SSE request", "error", err.Error())
		return
	}
	defer clean()

	// Create a buffer to hold out-of-order messages until they can be delivered in order
	messageBuffer := make(map[int]protocol.SSEMessagePayload)
	expectedSequence := 0

	// Create a serialization point with a mutex to ensure ordered writes
	var writeMutex sync.Mutex

	// Function to handle writing SSE messages in a synchronized manner
	writeSSEMessage := func(data string) {
		writeMutex.Lock()
		defer writeMutex.Unlock()

		s.l.Debug("writing SSE message", "data", data)
		fmt.Fprintf(w, data+"\n\n")
		if f, ok := w.(http.Flusher); ok {
			f.Flush()
		}
	}

	// Function to process buffered messages in order
	processBufferedMessages := func() {
		writeMutex.Lock()
		defer writeMutex.Unlock()

		// Keep processing messages as long as we have the next expected sequence
		for {
			msg, ok := messageBuffer[expectedSequence]
			if !ok {
				break // Don't have the next message yet
			}

			// Write the message and remove it from the buffer
			s.l.Debug("writing buffered SSE message", "sequence", expectedSequence, "data", msg.Data)
			fmt.Fprintf(w, msg.Data+"\n\n")
			if f, ok := w.(http.Flusher); ok {
				f.Flush()
			}

			delete(messageBuffer, expectedSequence)
			expectedSequence++
		}
	}

	for response := range responseChannel {
		if response.Kind == protocol.MessageKindSSEClose {
			return
		}
		if response.Kind != protocol.MessageKindSSEMessage {
			s.l.Error("received unexpected message kind", "kind", response.Kind)
			return
		}

		var sseMessage protocol.SSEMessagePayload
		if err := json.Unmarshal(response.Payload, &sseMessage); err != nil {
			s.l.Error("failed to unmarshal SSE message", "error", err.Error())
			return
		}

		// Handle backward compatibility for older clients that don't use sequence numbers
		// Default sequential handling for legacy clients
		legacyClient := false

		// Check if this might be a message from an older client version
		if sseMessage.Sequence == 0 {
			// Look at other messages in the buffer to see if we have sequence numbers
			if len(messageBuffer) == 0 && expectedSequence == 0 {
				// This is likely the first message and it has no sequence
				// Assume this is an older client that doesn't support sequencing
				legacyClient = true
				s.l.Debug("detected legacy client without sequence numbers")
			}
		}

		if legacyClient {
			// For legacy clients, we just write messages in the order they arrive
			writeSSEMessage(sseMessage.Data)
		} else {
			// Standard sequence-based processing for newer clients
			if sseMessage.Sequence == expectedSequence {
				// This is the message we're expecting next, write it immediately
				writeSSEMessage(sseMessage.Data)
				expectedSequence++

				// Check if we have subsequent messages buffered
				processBufferedMessages()
			} else if sseMessage.Sequence > expectedSequence {
				// This message arrived early, buffer it for later
				s.l.Debug("buffering out-of-order SSE message", "sequence", sseMessage.Sequence, "expected", expectedSequence)
				messageBuffer[sseMessage.Sequence] = sseMessage
			} else {
				// This message is a duplicate or arrived very late (we already processed past this sequence)
				s.l.Warn("received outdated SSE message", "sequence", sseMessage.Sequence, "expected", expectedSequence)
			}
		}
	}
	s.l.Debug("SSE connection closed")
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
		s.l.Debug("detected SSE request", "path", r.URL.Path, "accept", acceptHeader)
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
	clean, err := s.tunnel.SendWithResponseChannel(protocol.MessageKindHttpRequest, &protocol.HttpRequestPayload{
		Method:  r.Method,
		Path:    r.URL.Path + "?" + r.URL.Query().Encode(),
		Headers: r.Header,
		Body:    bodyBytes,
	}, responseChannel)
	if err != nil {
		s.l.Error("failed to send HTTP request", "error", err.Error())
		return
	}
	defer clean()

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
	s.l.Debug("received response", "duration", time.Since(start), "status", responsePayload.Response.Status)

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
	clean, err := s.tunnel.SendWithResponseChannel(protocol.MessageKindWebsocketCreateRequest, &protocol.WebsocketCreateRequestPayload{
		Origin: r.Header.Get("Origin"),
		Path:   r.URL.Path,
	}, responseChannel)
	if err != nil {
		s.l.Error("failed to send websocket create request", "error", err.Error())
		return
	}
	defer clean()

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
			s.l.Debug("app websocket connection closed", "sessionID", responsePayload.SessionID, "error", err.Error())
			if err := s.tunnel.Send(protocol.MessageKindWebsocketClose, &protocol.WebsocketClosePayload{
				SessionID: responsePayload.SessionID,
			}); err != nil {
				s.l.Error("failed to send websocket close", "error", err.Error())
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
				s.l.Debug("tunnel websocket connection closed", "sessionID", responsePayload.SessionID, "error", err.Error())
				return
			}
			s.l.Error("failed to send websocket message", "error", err.Error())
			continue
		}
	}
}

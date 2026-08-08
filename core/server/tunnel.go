package server

import (
	"context"
	"encoding/json"
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
	l              log.Logger
}

type TunnelOptions struct{}

func NewTunnel(conn *websocket.Conn, options TunnelOptions, l log.Logger) *Tunnel {
	server := &Tunnel{
		tunnel:         shared.NewTunnel(conn, l),
		websocketConns: safe.NewMap[string, *safe.WSConn](),
		l:              l,
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

// SendText sends a text message (e.g. the welcome/ready announcement) to
// the tunnel client. Callers should only announce readiness after the
// tunnel is actually registered and routable.
func (s *Tunnel) SendText(text string) error {
	return s.tunnel.Send(protocol.MessageKindText, &protocol.TextPayload{Text: text})
}

func (s *Tunnel) Listen(ctx context.Context) {
	s.tunnel.Listen(ctx)
}

func (s *Tunnel) Close() {
	s.tunnel.Close()
}

// HandleHttpRequest proxies an HTTP request through the tunnel. The client
// responds either with a single buffered HttpResponse, or with a stream
// (HttpResponseStart, then HttpResponseChunk*, then HttpResponseEnd) for
// responses of unknown length (SSE, k8s watch streams, log follows, ...).
func (s *Tunnel) HandleHttpRequest(w http.ResponseWriter, r *http.Request) {
	// Handle WebSocket requests
	if r.Header.Get("Upgrade") == "websocket" {
		s.HandleWebsocketRequest(w, r)
		return
	}

	bodyBytes, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, "", http.StatusInternalServerError)
		return
	}

	path := r.URL.Path
	if r.URL.RawQuery != "" {
		path += "?" + r.URL.RawQuery
	}

	// Buffered so the tunnel read loop can deliver ahead of the consumer;
	// if it fills up, backpressure is applied to the tunnel.
	responseChannel := make(chan protocol.Message, 64)

	start := time.Now()
	requestID, clean, err := s.tunnel.SendWithResponseChannel(protocol.MessageKindHttpRequest, &protocol.HttpRequestPayload{
		Method:  r.Method,
		Path:    path,
		Headers: r.Header,
		Body:    bodyBytes,
	}, responseChannel)
	if err != nil {
		s.l.Error("failed to send HTTP request", "error", err.Error())
		http.Error(w, "", http.StatusBadGateway)
		return
	}
	defer clean()

	// Wait for the first response message
	var first protocol.Message
	select {
	case first = <-responseChannel:
	case <-r.Context().Done():
		// Downstream consumer went away before the client responded;
		// tell the client to cancel the upstream request.
		s.sendStreamCancel(requestID)
		return
	case <-s.tunnel.Done():
		http.Error(w, "tunnel closed", http.StatusBadGateway)
		return
	}

	switch first.Kind {
	case protocol.MessageKindHttpResponse:
		s.writeBufferedResponse(w, first, start)
	case protocol.MessageKindHttpResponseStart:
		s.writeStreamedResponse(w, r, requestID, first, responseChannel, start)
	default:
		s.l.Error("received unexpected message kind", "kind", first.Kind)
		http.Error(w, "", http.StatusInternalServerError)
	}
}

func (s *Tunnel) writeBufferedResponse(w http.ResponseWriter, msg protocol.Message, start time.Time) {
	var responsePayload protocol.HttpResponsePayload
	if err := json.Unmarshal(msg.Payload, &responsePayload); err != nil {
		http.Error(w, "", http.StatusInternalServerError)
		return
	}
	s.l.Debug("received response", "duration", time.Since(start), "status", responsePayload.Response.Status)

	if responsePayload.Response.Status == 0 {
		// The client failed to reach the target (error responses don't carry
		// a status). Return a gateway error rather than panicking on
		// WriteHeader(0).
		http.Error(w, "", http.StatusBadGateway)
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

func (s *Tunnel) writeStreamedResponse(w http.ResponseWriter, r *http.Request, requestID string, first protocol.Message, responseChannel chan protocol.Message, start time.Time) {
	var startPayload protocol.HttpResponseStartPayload
	if err := json.Unmarshal(first.Payload, &startPayload); err != nil {
		s.l.Error("failed to unmarshal stream start", "error", err.Error())
		http.Error(w, "", http.StatusInternalServerError)
		return
	}

	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "Streaming not supported", http.StatusInternalServerError)
		return
	}

	for k, v := range startPayload.Headers {
		// Let net/http manage framing of the streamed response itself.
		if strings.EqualFold(k, "Content-Length") || strings.EqualFold(k, "Transfer-Encoding") {
			continue
		}
		for _, vv := range v {
			w.Header().Add(k, vv)
		}
	}

	status := startPayload.Status
	if status == 0 {
		status = http.StatusOK
	}
	w.WriteHeader(status)
	flusher.Flush()

	s.l.Debug("stream started", "status", status, "duration", time.Since(start))

	for {
		select {
		case msg := <-responseChannel:
			switch msg.Kind {
			case protocol.MessageKindHttpResponseChunk:
				var chunk protocol.HttpResponseChunkPayload
				if err := json.Unmarshal(msg.Payload, &chunk); err != nil {
					s.l.Error("failed to unmarshal stream chunk", "error", err.Error())
					return
				}
				if _, err := w.Write(chunk.Data); err != nil {
					s.l.Debug("downstream write failed, cancelling stream", "error", err.Error())
					s.sendStreamCancel(requestID)
					s.drainUntilEnd(responseChannel)
					return
				}
				flusher.Flush()
			case protocol.MessageKindHttpResponseEnd:
				var end protocol.HttpResponseEndPayload
				if err := json.Unmarshal(msg.Payload, &end); err == nil && end.Error != "" {
					s.l.Error("stream ended with error", "error", end.Error)
				}
				s.l.Debug("stream ended", "duration", time.Since(start))
				return
			default:
				s.l.Error("received unexpected message kind during stream", "kind", msg.Kind)
				return
			}
		case <-r.Context().Done():
			// Downstream consumer disconnected; cancel upstream and drain
			// remaining messages so the tunnel read loop is never blocked
			// on our abandoned channel.
			s.l.Debug("downstream disconnected, cancelling stream", "request_id", requestID)
			s.sendStreamCancel(requestID)
			s.drainUntilEnd(responseChannel)
			return
		case <-s.tunnel.Done():
			return
		}
	}
}

func (s *Tunnel) sendStreamCancel(requestID string) {
	if s.tunnel.IsClosed() {
		return
	}
	if err := s.tunnel.Send(protocol.MessageKindHttpStreamCancel, &protocol.HttpStreamCancelPayload{
		RequestID: requestID,
	}); err != nil {
		s.l.Error("failed to send stream cancel", "error", err.Error())
	}
}

// drainUntilEnd consumes stream messages until the client acknowledges the
// end of the stream (or a timeout), keeping the tunnel read loop unblocked.
func (s *Tunnel) drainUntilEnd(responseChannel chan protocol.Message) {
	timeout := time.After(10 * time.Second)
	for {
		select {
		case msg := <-responseChannel:
			if msg.Kind == protocol.MessageKindHttpResponseEnd {
				return
			}
		case <-timeout:
			s.l.Warn("timed out draining stream after cancel")
			return
		case <-s.tunnel.Done():
			return
		}
	}
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

	responseChannel := make(chan protocol.Message, 1)
	_, clean, err := s.tunnel.SendWithResponseChannel(protocol.MessageKindWebsocketCreateRequest, &protocol.WebsocketCreateRequestPayload{
		Origin: r.Header.Get("Origin"),
		Path:   r.URL.Path,
	}, responseChannel)
	if err != nil {
		s.l.Error("failed to send websocket create request", "error", err.Error())
		return
	}
	defer clean()

	var response protocol.Message
	select {
	case response = <-responseChannel:
	case <-s.tunnel.Done():
		http.Error(w, "tunnel closed", http.StatusBadGateway)
		return
	}

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

package client

import (
	"bufio"
	"bytes"
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/campbel/tiny-tunnel/core/protocol"
	"github.com/campbel/tiny-tunnel/core/shared"
	"github.com/campbel/tiny-tunnel/core/stats"
	"github.com/campbel/tiny-tunnel/internal/log"
	"github.com/campbel/tiny-tunnel/internal/safe"
	"github.com/campbel/tiny-tunnel/internal/util"
	"github.com/google/uuid"
	"github.com/gorilla/websocket"
)

var (
	httpClient = &http.Client{
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			return http.ErrUseLastResponse
		},
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
		},
	}
)

func NewTunnel(ctx context.Context, options Options) (*shared.Tunnel, error) {
	tracker := options.Tracker
	if tracker == nil {
		tracker = new(stats.Tracker)
	}

	//
	// Create the client tunnel connection
	//
	conn, _, err := websocket.DefaultDialer.DialContext(ctx, options.URL(), options.ServerHeaders)
	if err != nil {
		return nil, err
	}
	tunnel := shared.NewTunnel(conn)

	//
	// Register client handlers
	//
	tunnel.RegisterTextHandler(func(tunnel *shared.Tunnel, id string, payload protocol.TextPayload) {
		fmt.Fprintf(options.Output(), "%s\n", payload.Text)
	})

	//
	// HTTP
	//
	// Requests are sent to the target and response send back to the server.
	// Each request is 1:1 to a response which makes this fairly trivial.
	//
	httpClient = &http.Client{
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			return http.ErrUseLastResponse
		},
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{InsecureSkipVerify: options.Insecure},
		},
	}

	tunnel.RegisterHttpRequestHandler(func(tunnel *shared.Tunnel, id string, payload protocol.HttpRequestPayload) {
		log.Debug("handling http request", "payload", payload)

		tracker.IncrementHttpRequest()

		url_ := options.Target + payload.Path
		req, err := http.NewRequest(payload.Method, url_, bytes.NewReader(payload.Body))
		if err != nil {
			log.Error("failed to create HTTP request", "error", err.Error())
			return
		}

		for k, v := range payload.Headers {
			for _, vv := range v {
				req.Header.Add(k, vv)
			}
		}

		resp, err := httpClient.Do(req)
		if err != nil {
			tracker.IncrementHttpResponse()
			tunnel.SendResponse(protocol.MessageKindHttpResponse, id, &protocol.HttpResponsePayload{Error: err})
			return
		}
		defer resp.Body.Close()

		bodyBytes, err := io.ReadAll(resp.Body)
		if err != nil {
			tracker.IncrementHttpResponse()
			tunnel.SendResponse(protocol.MessageKindHttpResponse, id, &protocol.HttpResponsePayload{Error: err})
			return
		}
		log.Debug("sending response", "status", resp.StatusCode, "headers", resp.Header)

		tracker.IncrementHttpResponse()
		tunnel.SendResponse(protocol.MessageKindHttpResponse, id, &protocol.HttpResponsePayload{Response: protocol.HttpResponse{
			Status:  resp.StatusCode,
			Headers: resp.Header,
			Body:    bodyBytes,
		}})
	})

	//
	// Websockets
	//
	// For websockets, we must establish connections and store a reference to them in the session map.
	// Each connection is given a session ID as its identifier and passed back to the server in the response.
	// The server will use this ID to send messages to the client in the future.
	//
	wsSessions := safe.NewMap[string, *safe.WSConn]()

	tunnel.RegisterWebsocketCreateRequestHandler(func(tunnel *shared.Tunnel, id string, payload protocol.WebsocketCreateRequestPayload) {
		log.Debug("handling websocket create request", "payload", payload)
		wsUrl, err := util.GetWebsocketURL(options.Target)
		if err != nil {
			tunnel.SendResponse(protocol.MessageKindWebsocketCreateResponse, id, &protocol.WebsocketCreateResponsePayload{Error: err})
			return
		}

		rawConn, resp, err := websocket.DefaultDialer.DialContext(ctx, wsUrl.String()+payload.Path, http.Header{"Origin": []string{payload.Origin}})
		if err != nil {
			tunnel.SendResponse(protocol.MessageKindWebsocketCreateResponse, id, &protocol.WebsocketCreateResponsePayload{Error: err})
			return
		}

		conn := safe.NewWSConn(rawConn)
		tracker.IncrementWebsocketConnection()

		sessionID := uuid.New().String()
		if ok := wsSessions.SetNX(sessionID, conn); !ok {
			tunnel.SendResponse(protocol.MessageKindWebsocketCreateResponse, id, &protocol.WebsocketCreateResponsePayload{Error: errors.New("session already exists")})
			return
		}

		tunnel.SendResponse(protocol.MessageKindWebsocketCreateResponse, id, &protocol.WebsocketCreateResponsePayload{
			SessionID: sessionID,
			HttpResponse: &protocol.HttpResponsePayload{Response: protocol.HttpResponse{
				Status:  resp.StatusCode,
				Headers: resp.Header,
			}},
		})

		go func() {
			log.Info("starting websocket read loop", "session_id", sessionID)
			defer func() {
				log.Info("closing websocket connection", "session_id", sessionID)
				conn.Close()
				wsSessions.Delete(sessionID)
				tracker.DecrementWebsocketConnection()
			}()

			for {
				mt, data, err := conn.ReadMessage()
				if err != nil {
					log.Error("exiting websocket read loop", "error", err.Error(), "session_id", sessionID)
					break
				}
				tracker.IncrementWebsocketMessageRecv()
				log.Debug("read ws message", "session_id", sessionID, "kind", mt, "data", string(data))
				if err := tunnel.Send(protocol.MessageKindWebsocketMessage, &protocol.WebsocketMessagePayload{SessionID: sessionID, Kind: mt, Data: data}); err != nil {
					log.Error("failed to send websocket message", "error", err.Error())
				}
			}
		}()
	})

	tunnel.RegisterWebsocketMessageHandler(func(tunnel *shared.Tunnel, id string, payload protocol.WebsocketMessagePayload) {
		log.Debug("handling websocket message", "payload", payload)
		conn, ok := wsSessions.Get(payload.SessionID)
		if !ok {
			log.Error("websocket session not found", "session_id", payload.SessionID)
			return
		}
		if err := conn.WriteMessage(payload.Kind, payload.Data); err != nil {
			log.Error("failed to write websocket message", "error", err.Error())
		}
		tracker.IncrementWebsocketMessageSent()
	})

	tunnel.RegisterWebsocketCloseHandler(func(tunnel *shared.Tunnel, id string, payload protocol.WebsocketClosePayload) {
		log.Debug("handling websocket close", "payload", payload)
		conn, ok := wsSessions.Get(payload.SessionID)
		if !ok {
			log.Error("websocket session not found", "session_id", payload.SessionID)
			return
		}
		if err := conn.Close(); err != nil {
			log.Error("failed to close websocket connection", "error", err.Error(), "payload", payload)
		}
		wsSessions.Delete(payload.SessionID)
	})

	//
	// Server-Sent Events
	//
	// SSE is more complex than HTTP, but simpler than websockets. We must establish the connection,
	// then read the data and send it back to the server until a connection close request is received
	// or until the connection is closed by the server.
	//
	tunnel.RegisterSSERequestHandler(func(tunnel *shared.Tunnel, id string, payload protocol.SSERequestPayload) {
		tracker.IncrementSseConnection()
		defer tracker.DecrementSseConnection()

		req, err := http.NewRequest(http.MethodGet, options.Target+payload.Path, nil)
		if err != nil {
			log.Error("failed to create SSE request", "error", err.Error())
			return
		}
		for k, v := range payload.Headers {
			for _, vv := range v {
				req.Header.Add(k, vv)
			}
		}

		resp, err := httpClient.Do(req)
		if err != nil {
			log.Error("failed to send SSE request", "error", err.Error())
			return
		}

		scanner := bufio.NewScanner(resp.Body)
		for scanner.Scan() {
			text := scanner.Text()
			if strings.HasPrefix(text, "data:") {
				tracker.IncrementSseMessageRecv()
			}
			tunnel.SendResponse(protocol.MessageKindSSEMessage, id, &protocol.SSEMessagePayload{Data: text})
		}

		tunnel.SendResponse(protocol.MessageKindSSEClose, id, &protocol.SSEClosePayload{})
		defer resp.Body.Close()
	})

	return tunnel, nil
}

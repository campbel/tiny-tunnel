package client

import (
	"bufio"
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

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

func NewTunnel(ctx context.Context, options Options, stateProvider stats.StateProvider, statsProvider stats.StatsProvider, l log.Logger) (*shared.Tunnel, error) {
	// Create the state manager
	stateProvider.SetStatus(stats.StatusConnecting)
	stateProvider.SetStatusMessage("Connecting to server...")

	// Create the client tunnel connection
	// Prepare headers
	headers := options.ServerHeaders
	if headers == nil {
		headers = http.Header{}
	}

	// Add auth token if available
	if token := options.GetResolvedToken(); token != "" {
		headers.Set("X-Auth-Token", token)
	}

	// Update state before connection attempt
	tunnelURL := options.URL()
	stateProvider.SetStatusMessage(fmt.Sprintf("Connecting to %s...", tunnelURL))

	conn, _, err := websocket.DefaultDialer.DialContext(ctx, tunnelURL, headers)
	if err != nil {
		stateProvider.SetStatus(stats.StatusError)
		stateProvider.SetStatusMessage(fmt.Sprintf("Failed to connect: %s", err.Error()))
		return nil, err
	}

	tunnel := shared.NewTunnel(conn, l)

	// Update state after successful connection
	stateProvider.SetStatus(stats.StatusConnected)
	stateProvider.SetStatusMessage("Connected successfully")

	// Register client handlers
	tunnel.RegisterTextHandler(func(tunnel *shared.Tunnel, id string, payload protocol.TextPayload) {
		if payload.Text == "ping" {
			l.Debug("received ping", "id", id)
			tunnel.Send(protocol.MessageKindText, &protocol.TextPayload{
				Text: "pong",
			})
			return
		}

		fmt.Fprintf(options.Output(), "%s\n", payload.Text)

		// capture welcome message
		if strings.HasPrefix(payload.Text, "Welcome to Tiny Tunnel!") {
			parts := strings.Split(payload.Text, " ")
			stateProvider.SetURL(parts[len(parts)-1])
		}
	})

	// HTTP
	// Requests are sent to the target and response send back to the server.
	// Each request is 1:1 to a response which makes this fairly trivial.
	httpClient = &http.Client{
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			return http.ErrUseLastResponse
		},
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{InsecureSkipVerify: options.Insecure},
		},
	}

	tunnel.RegisterHttpRequestHandler(func(tunnel *shared.Tunnel, id string, payload protocol.HttpRequestPayload) {
		l.Debug("handling http request", "payload", payload)

		// Track request time
		startTime := time.Now()
		statsProvider.IncrementHttpRequest()

		url_ := options.Target + payload.Path
		req, err := http.NewRequest(payload.Method, url_, bytes.NewReader(payload.Body))
		if err != nil {
			l.Error("failed to create HTTP request", "error", err.Error())
			return
		}

		for k, v := range payload.Headers {
			for _, vv := range v {
				req.Header.Add(k, vv)
			}
		}

		// We don't need to add token to HTTP requests as tunnel access doesn't require auth
		// The token is only needed for /register endpoint which is handled during websocket connection

		resp, err := httpClient.Do(req)
		if err != nil {
			statsProvider.IncrementHttpResponse()
			tunnel.SendResponse(protocol.MessageKindHttpResponse, id, &protocol.HttpResponsePayload{Error: err})
			l.Info(payload.Method, payload.Path, 0, time.Since(startTime), err)
			return
		}
		defer resp.Body.Close()

		bodyBytes, err := io.ReadAll(resp.Body)
		if err != nil {
			statsProvider.IncrementHttpResponse()
			tunnel.SendResponse(protocol.MessageKindHttpResponse, id, &protocol.HttpResponsePayload{Error: err})
			l.Info(payload.Method, payload.Path, resp.StatusCode, time.Since(startTime), err)
			return
		}

		// Calculate elapsed time
		elapsed := time.Since(startTime)
		l.Debug("sending response", "status", resp.StatusCode, "elapsed", elapsed)

		statsProvider.IncrementHttpResponse()
		l.Info("http request completed", "status", resp.StatusCode, "elapsed", elapsed, "method", payload.Method, "path", payload.Path)
		tunnel.SendResponse(protocol.MessageKindHttpResponse, id, &protocol.HttpResponsePayload{Response: protocol.HttpResponse{
			Status:  resp.StatusCode,
			Headers: resp.Header,
			Body:    bodyBytes,
		}})
	})

	// Websockets
	// For websockets, we must establish connections and store a reference to them in the session map.
	// Each connection is given a session ID as its identifier and passed back to the server in the response.
	// The server will use this ID to send messages to the client in the future.
	wsSessions := safe.NewMap[string, *safe.WSConn]()

	tunnel.RegisterWebsocketCreateRequestHandler(func(tunnel *shared.Tunnel, id string, payload protocol.WebsocketCreateRequestPayload) {
		l.Debug("handling websocket create request", "payload", payload)
		wsUrl, err := util.GetWebsocketURL(options.Target)
		if err != nil {
			tunnel.SendResponse(protocol.MessageKindWebsocketCreateResponse, id, &protocol.WebsocketCreateResponsePayload{Error: err})
			return
		}

		// Prepare headers for the WebSocket connection
		wsHeaders := http.Header{"Origin": []string{payload.Origin}}

		// We don't need to add token to WebSocket connections as tunnel access doesn't require auth
		// The token is only needed for /register endpoint which is handled during initial websocket connection

		rawConn, resp, err := websocket.DefaultDialer.DialContext(ctx, wsUrl.String()+payload.Path, wsHeaders)
		if err != nil {
			tunnel.SendResponse(protocol.MessageKindWebsocketCreateResponse, id, &protocol.WebsocketCreateResponsePayload{Error: err})
			return
		}

		conn := safe.NewWSConn(rawConn)
		statsProvider.IncrementWebsocketConnection()

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
			l.Info("starting websocket read loop", "session_id", sessionID)
			defer func() {
				l.Info("closing websocket connection", "session_id", sessionID)
				conn.Close()
				wsSessions.Delete(sessionID)
				statsProvider.DecrementWebsocketConnection()
			}()

			for {
				mt, data, err := conn.ReadMessage()
				if err != nil {
					l.Error("exiting websocket read loop", "error", err.Error(), "session_id", sessionID)
					break
				}
				statsProvider.IncrementWebsocketMessageRecv()
				l.Debug("read ws message", "session_id", sessionID, "kind", mt, "data", string(data))
				if err := tunnel.Send(protocol.MessageKindWebsocketMessage, &protocol.WebsocketMessagePayload{SessionID: sessionID, Kind: mt, Data: data}); err != nil {
					l.Error("failed to send websocket message", "error", err.Error())
				}
			}
		}()
	})

	tunnel.RegisterWebsocketMessageHandler(func(tunnel *shared.Tunnel, id string, payload protocol.WebsocketMessagePayload) {
		l.Debug("handling websocket message", "payload", payload)
		conn, ok := wsSessions.Get(payload.SessionID)
		if !ok {
			l.Error("websocket session not found", "session_id", payload.SessionID)
			return
		}
		if err := conn.WriteMessage(payload.Kind, payload.Data); err != nil {
			l.Error("failed to write websocket message", "error", err.Error())
		}
		statsProvider.IncrementWebsocketMessageSent()
	})

	tunnel.RegisterWebsocketCloseHandler(func(tunnel *shared.Tunnel, id string, payload protocol.WebsocketClosePayload) {
		l.Debug("handling websocket close", "payload", payload)
		conn, ok := wsSessions.Get(payload.SessionID)
		if !ok {
			l.Error("websocket session not found", "session_id", payload.SessionID)
			return
		}
		if err := conn.Close(); err != nil {
			l.Error("failed to close websocket connection", "error", err.Error(), "payload", payload)
		}
		wsSessions.Delete(payload.SessionID)
	})

	// Server-Sent Events
	tunnel.RegisterSSERequestHandler(func(tunnel *shared.Tunnel, id string, payload protocol.SSERequestPayload) {
		statsProvider.IncrementSseConnection()
		defer statsProvider.DecrementSseConnection()

		// Create a context for this SSE request that will be cancelled when the tunnel is closed
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()

		// Handle tunnel closure by canceling the context
		go func() {
			select {
			case <-tunnel.Done():
				cancel()
			case <-ctx.Done():
				// Context already cancelled
			}
		}()

		req, err := http.NewRequestWithContext(ctx, http.MethodGet, options.Target+payload.Path, nil)
		if err != nil {
			l.Error("failed to create SSE request", "error", err.Error())
			return
		}
		for k, v := range payload.Headers {
			for _, vv := range v {
				req.Header.Add(k, vv)
			}
		}

		// We don't need to add token to SSE requests as tunnel access doesn't require auth
		// The token is only needed for /register endpoint which is handled during initial websocket connection

		// Use a client with context
		client := &http.Client{
			Timeout: 10 * time.Second,
		}
		resp, err := client.Do(req)
		if err != nil {
			if ctx.Err() != nil {
				// Context was cancelled, this is expected
				return
			}
			l.Error("failed to send SSE request", "error", err.Error())
			return
		}
		defer resp.Body.Close()

		// Use a buffered reader for better performance
		reader := bufio.NewReaderSize(resp.Body, 4096)
		var messageBuilder strings.Builder

		// Use a mutex to synchronize sending messages and ensure order is preserved
		var sendMutex sync.Mutex

		// Track sequence number to ensure messages are ordered
		sequenceNumber := 0

		// Create a function to send messages in a synchronized way with context awareness
		sendMessage := func(message string) {
			if message == "" || tunnel.IsClosed() {
				return
			}

			sendMutex.Lock()
			defer sendMutex.Unlock()

			currentSequence := sequenceNumber
			sequenceNumber++

			if err := tunnel.SendResponse(protocol.MessageKindSSEMessage, id, &protocol.SSEMessagePayload{
				Data:     message,
				Sequence: currentSequence,
			}); err != nil {
				l.Error("failed to send SSE message", "error", err.Error())
			} else {
				statsProvider.IncrementSseMessageRecv()
			}
		}

		// Safely try to send a close message at the end
		sendClose := func() {
			if tunnel.IsClosed() {
				return
			}

			sendMutex.Lock()
			defer sendMutex.Unlock()

			if err := tunnel.SendResponse(protocol.MessageKindSSEClose, id, &protocol.SSEClosePayload{}); err != nil {
				l.Error("failed to send SSE close", "error", err.Error())
			}
		}
		defer sendClose()

		done := make(chan struct{})
		doneOnce := &sync.Once{}

		// Safely close the done channel
		closeDone := func() {
			doneOnce.Do(func() {
				close(done)
			})
		}

		// Handle context cancellation in a separate goroutine
		go func() {
			select {
			case <-ctx.Done():
				// Try to unblock any read by setting a deadline
				if deadline, ok := resp.Body.(interface{ SetReadDeadline(time.Time) error }); ok {
					_ = deadline.SetReadDeadline(time.Now())
				}
				closeDone()
			case <-done:
				// Reading finished normally
			}
		}()

		// Read using scanner with done channel for cancellation
		go func() {
			defer closeDone()

			scanner := bufio.NewScanner(reader)
			for scanner.Scan() {
				select {
				case <-ctx.Done():
					return
				default:
					line := scanner.Text()

					// Empty line indicates end of message
					if line == "" {
						message := messageBuilder.String()
						if message != "" {
							sendMessage(message)
							messageBuilder.Reset()
						}
						continue
					}

					// Add line to current message
					if messageBuilder.Len() > 0 {
						messageBuilder.WriteString("\n")
					}
					messageBuilder.WriteString(line)
				}
			}

			// Send any remaining message
			if messageBuilder.Len() > 0 {
				sendMessage(messageBuilder.String())
			}
		}()

		// Wait for either the context to be cancelled or reading to finish
		<-done
	})

	return tunnel, nil
}

// TestAuth verifies if the token is valid by making a request to the auth-test endpoint
func TestAuth(options Options) (map[string]any, error) {
	// Get server URL from the parsed URL in options
	serverURL, err := url.Parse(options.ServerHost)
	if err != nil || (serverURL.Scheme != "http" && serverURL.Scheme != "https") {
		// If parsing fails or no scheme, try to parse it properly
		serverURL, err = parseServerURL(options.ServerHost)
		if err != nil {
			return nil, fmt.Errorf("failed to parse server URL: %w", err)
		}
	}

	// Build the auth test URL
	authTestURL, err := url.JoinPath(serverURL.String(), "/api/auth-test")
	if err != nil {
		return nil, fmt.Errorf("failed to build auth test URL: %w", err)
	}

	// Create a request with auth token header
	req, err := http.NewRequest("GET", authTestURL, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	// Add auth token to header
	token := options.GetResolvedToken()
	if token == "" {
		return nil, fmt.Errorf("no authentication token available")
	}
	req.Header.Set("X-Auth-Token", token)

	// Make the request
	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to send request: %w", err)
	}
	defer resp.Body.Close()

	// Check status code
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("authentication failed with status code: %d", resp.StatusCode)
	}

	// Read and parse response
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}

	// Parse JSON response
	var result map[string]interface{}
	if err := json.Unmarshal(body, &result); err != nil {
		return nil, fmt.Errorf("failed to parse response: %w", err)
	}

	// Check if the token is valid
	valid, ok := result["valid"].(bool)
	if !ok {
		return nil, fmt.Errorf("invalid response format")
	}
	if !valid {
		return nil, fmt.Errorf("token is invalid")
	}

	return result, nil
}

// parseServerURL parses a server string into a URL
func parseServerURL(server string) (*url.URL, error) {
	// Check if server already has a scheme
	if !strings.HasPrefix(server, "http://") && !strings.HasPrefix(server, "https://") {
		// No scheme provided, check if it's localhost or IP
		if strings.HasPrefix(server, "localhost") || strings.HasPrefix(server, "127.0.0.1") {
			server = "http://" + server
		} else {
			server = "https://" + server
		}
	}

	// Parse URL
	parsedURL, err := url.Parse(server)
	if err != nil {
		return nil, fmt.Errorf("invalid server URL: %w", err)
	}

	return parsedURL, nil
}

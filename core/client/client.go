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
	"time"

	"github.com/campbel/tiny-tunnel/core/client/ui"
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
	// Create the state manager
	tunnelState := stats.NewTunnelState(options.Target, options.Name)
	tunnelState.SetStatus(stats.StatusConnecting)
	tunnelState.SetStatusMessage("Connecting to server...")

	// Use the state provider instead of a raw tracker
	stateProvider := stats.NewTunnelStateProvider(tunnelState)

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
	tunnelState.SetStatusMessage(fmt.Sprintf("Connecting to %s...", tunnelURL))

	conn, _, err := websocket.DefaultDialer.DialContext(ctx, tunnelURL, headers)
	if err != nil {
		tunnelState.SetStatus(stats.StatusError)
		tunnelState.SetStatusMessage(fmt.Sprintf("Failed to connect: %s", err.Error()))
		return nil, err
	}

	tunnel := shared.NewTunnel(conn)

	// Update state after successful connection
	tunnelState.SetStatus(stats.StatusConnected)
	tunnelState.SetStatusMessage("Connected successfully")

	// Register client handlers
	tunnel.RegisterTextHandler(func(tunnel *shared.Tunnel, id string, payload protocol.TextPayload) {
		if payload.Text == "ping" {
			log.Debug("received ping", "id", id)
			tunnel.Send(protocol.MessageKindText, &protocol.TextPayload{
				Text: "pong",
			})
			return
		}

		fmt.Fprintf(options.Output(), "%s\n", payload.Text)

		// capture welcome message
		if strings.HasPrefix(payload.Text, "Welcome to Tiny Tunnel!") {
			parts := strings.Split(payload.Text, " ")
			tunnelState.SetURL(parts[len(parts)-1])
		}

		// Add log entry to state
		tunnelState.AddLogEntry(stats.LogEntry{
			Timestamp: tunnel.LastReceiveTime(),
			Level:     "info",
			Message:   payload.Text,
		})
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
		log.Debug("handling http request", "payload", payload)

		// Track request time
		startTime := time.Now()
		stateProvider.IncrementHttpRequest()

		url_ := options.Target + payload.Path
		req, err := http.NewRequest(payload.Method, url_, bytes.NewReader(payload.Body))
		if err != nil {
			log.Error("failed to create HTTP request", "error", err.Error())
			// Log failed request
			logHttpRequest(tunnelState, payload.Method, payload.Path, 0, 0, err)
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
			stateProvider.IncrementHttpResponse()
			tunnel.SendResponse(protocol.MessageKindHttpResponse, id, &protocol.HttpResponsePayload{Error: err})
			// Log failed request
			logHttpRequest(tunnelState, payload.Method, payload.Path, 0, time.Since(startTime), err)
			return
		}
		defer resp.Body.Close()

		bodyBytes, err := io.ReadAll(resp.Body)
		if err != nil {
			stateProvider.IncrementHttpResponse()
			tunnel.SendResponse(protocol.MessageKindHttpResponse, id, &protocol.HttpResponsePayload{Error: err})
			// Log failed response reading
			logHttpRequest(tunnelState, payload.Method, payload.Path, resp.StatusCode, time.Since(startTime), err)
			return
		}

		// Calculate elapsed time
		elapsed := time.Since(startTime)
		log.Debug("sending response", "status", resp.StatusCode, "elapsed", elapsed)

		// Log successful request
		logHttpRequest(tunnelState, payload.Method, payload.Path, resp.StatusCode, elapsed, nil)

		stateProvider.IncrementHttpResponse()
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
		log.Debug("handling websocket create request", "payload", payload)
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
		stateProvider.IncrementWebsocketConnection()

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
				stateProvider.DecrementWebsocketConnection()
			}()

			for {
				mt, data, err := conn.ReadMessage()
				if err != nil {
					log.Error("exiting websocket read loop", "error", err.Error(), "session_id", sessionID)
					break
				}
				stateProvider.IncrementWebsocketMessageRecv()
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
		stateProvider.IncrementWebsocketMessageSent()
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

	// Server-Sent Events
	tunnel.RegisterSSERequestHandler(func(tunnel *shared.Tunnel, id string, payload protocol.SSERequestPayload) {
		stateProvider.IncrementSseConnection()
		defer stateProvider.DecrementSseConnection()

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

		// We don't need to add token to SSE requests as tunnel access doesn't require auth
		// The token is only needed for /register endpoint which is handled during initial websocket connection

		resp, err := httpClient.Do(req)
		if err != nil {
			log.Error("failed to send SSE request", "error", err.Error())
			return
		}

		scanner := bufio.NewScanner(resp.Body)
		for scanner.Scan() {
			text := scanner.Text()
			if strings.HasPrefix(text, "data:") {
				log.Info("received SSE message", "data", text)
				stateProvider.IncrementSseMessageRecv()
				if err := tunnel.SendResponse(protocol.MessageKindSSEMessage, id, &protocol.SSEMessagePayload{Data: text}); err != nil {
					log.Error("failed to send SSE message", "error", err.Error())
				}
			}
		}

		tunnel.SendResponse(protocol.MessageKindSSEClose, id, &protocol.SSEClosePayload{})
		defer resp.Body.Close()
	})

	// Store the state provider in the tunnel for access by the client
	tunnel.SetContext("state", tunnelState)

	return tunnel, nil
}

// StartTUI creates and starts a TUI for the given tunnel.
// It should be called after the tunnel is successfully established.
func StartTUI(ctx context.Context, tunnel *shared.Tunnel) error {
	// Get the state from the tunnel
	state, ok := tunnel.GetContext("state").(*stats.TunnelState)
	if !ok {
		return errors.New("tunnel state not found")
	}

	// Create and start the TUI
	tuiInstance := ui.NewTUI(state)
	return tuiInstance.Start()
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

// logHttpRequest logs detailed information about HTTP requests
func logHttpRequest(state *stats.TunnelState, method, path string, statusCode int, elapsed time.Duration, err error) {
	// Create a log entry with HTTP request details
	entry := stats.LogEntry{
		Timestamp: time.Now(),
		Level:     "info",
		Fields: map[string]interface{}{
			"method": method,
			"path":   path,
		},
	}

	// Format the message based on success or failure
	if err != nil {
		entry.Level = "error"
		if statusCode > 0 {
			entry.Message = fmt.Sprintf("%s %s - %d ERROR: %s (%.2fms)",
				method, path, statusCode, err.Error(), float64(elapsed.Microseconds())/1000)
		} else {
			entry.Message = fmt.Sprintf("%s %s - ERROR: %s",
				method, path, err.Error())
		}
	} else {
		// Format status code with color indicators
		var statusIndicator string
		if statusCode >= 200 && statusCode < 300 {
			statusIndicator = "✓" // Success
			entry.Level = "info"
		} else if statusCode >= 300 && statusCode < 400 {
			statusIndicator = "↪" // Redirect
			entry.Level = "info"
		} else if statusCode >= 400 && statusCode < 500 {
			statusIndicator = "⚠" // Client error
			entry.Level = "warn"
		} else {
			statusIndicator = "✗" // Server error
			entry.Level = "error"
		}

		// Format the duration
		durationMs := float64(elapsed.Microseconds()) / 1000

		entry.Message = fmt.Sprintf("%s %s - %d %s (%.2fms)",
			method, path, statusCode, statusIndicator, durationMs)

		entry.Fields["status"] = statusCode
		entry.Fields["duration_ms"] = durationMs
	}

	// Add the log entry to the state
	state.AddLogEntry(entry)
}

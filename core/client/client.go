package client

import (
	"bytes"
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"net/url"
	"strings"
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
	// Requests are sent to the target and the response is relayed back to the
	// server. Responses with a known length are buffered and sent as a single
	// HttpResponse message. Responses with unknown length (chunked transfer
	// encoding, SSE, k8s watch streams, log follows, ...) are streamed back
	// chunk-by-chunk as HttpResponseStart/Chunk/End messages.
	targetTLS, err := targetTLSConfig(options)
	if err != nil {
		return nil, err
	}
	tunnelHttpClient := &http.Client{
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			return http.ErrUseLastResponse
		},
		Transport: &http.Transport{
			TLSClientConfig: targetTLS,
		},
	}

	// activeStreams tracks in-flight streamed responses by request ID so the
	// server can cancel the upstream request when the downstream consumer
	// disconnects.
	activeStreams := safe.NewMap[string, context.CancelFunc]()

	tunnel.RegisterHttpRequestHandler(func(tunnel *shared.Tunnel, id string, payload protocol.HttpRequestPayload) {
		// Handlers run on the tunnel read loop; do the actual work in a
		// goroutine so slow targets don't block the tunnel.
		go handleHttpRequest(tunnel, id, payload, options, tunnelHttpClient, activeStreams, statsProvider, l)
	})

	tunnel.RegisterHttpStreamCancelHandler(func(tunnel *shared.Tunnel, id string, payload protocol.HttpStreamCancelPayload) {
		l.Debug("handling http stream cancel", "request_id", payload.RequestID)
		if cancel, ok := activeStreams.Get(payload.RequestID); ok {
			cancel()
		}
	})

	// Websockets
	// For websockets, we must establish connections and store a reference to them in the session map.
	// Each connection is given a session ID as its identifier and passed back to the server in the response.
	// The server will use this ID to send messages to the client in the future.
	wsSessions := safe.NewMap[string, *safe.WSConn]()

	tunnel.RegisterWebsocketCreateRequestHandler(func(tunnel *shared.Tunnel, id string, payload protocol.WebsocketCreateRequestPayload) {
		// Dialing the target can block; run async to keep the tunnel read loop free.
		go handleWebsocketCreateRequest(ctx, tunnel, id, payload, options, wsSessions, statsProvider, l)
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

	return tunnel, nil
}

// handleHttpRequest proxies a single HTTP request from the tunnel server to
// the local target, streaming the response back when its length is unknown.
func handleHttpRequest(
	tunnel *shared.Tunnel,
	id string,
	payload protocol.HttpRequestPayload,
	options Options,
	httpClient *http.Client,
	activeStreams *safe.Map[string, context.CancelFunc],
	statsProvider stats.StatsProvider,
	l log.Logger,
) {
	l.Debug("handling http request", "method", payload.Method, "path", payload.Path)

	startTime := time.Now()
	statsProvider.IncrementHttpRequest()

	// The request context is cancelled when the tunnel closes or when the
	// server tells us the downstream consumer went away (HttpStreamCancel).
	reqCtx, cancel := context.WithCancel(tunnel.Context())
	defer cancel()

	activeStreams.SetNX(id, cancel)
	defer activeStreams.Delete(id)

	url_ := options.Target + payload.Path
	req, err := http.NewRequestWithContext(reqCtx, payload.Method, url_, bytes.NewReader(payload.Body))
	if err != nil {
		l.Error("failed to create HTTP request", "error", err.Error())
		statsProvider.IncrementHttpResponse()
		tunnel.SendResponse(protocol.MessageKindHttpResponse, id, &protocol.HttpResponsePayload{Error: err})
		return
	}

	for k, v := range payload.Headers {
		for _, vv := range v {
			req.Header.Add(k, vv)
		}
	}

	resp, err := httpClient.Do(req)
	if err != nil {
		statsProvider.IncrementHttpResponse()
		tunnel.SendResponse(protocol.MessageKindHttpResponse, id, &protocol.HttpResponsePayload{Error: err})
		l.Info("http request failed", "method", payload.Method, "path", payload.Path, "elapsed", time.Since(startTime), "error", err.Error())
		return
	}
	defer resp.Body.Close()

	if isStreamingResponse(resp) {
		streamHttpResponse(tunnel, id, payload, resp, statsProvider, l, startTime)
		return
	}

	bodyBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		statsProvider.IncrementHttpResponse()
		tunnel.SendResponse(protocol.MessageKindHttpResponse, id, &protocol.HttpResponsePayload{Error: err})
		l.Info("http request failed", "method", payload.Method, "path", payload.Path, "status", resp.StatusCode, "elapsed", time.Since(startTime), "error", err.Error())
		return
	}

	elapsed := time.Since(startTime)
	statsProvider.IncrementHttpResponse()
	l.Info("http request completed", "status", resp.StatusCode, "elapsed", elapsed, "method", payload.Method, "path", payload.Path)
	tunnel.SendResponse(protocol.MessageKindHttpResponse, id, &protocol.HttpResponsePayload{Response: protocol.HttpResponse{
		Status:  resp.StatusCode,
		Headers: resp.Header,
		Body:    bodyBytes,
	}})
}

// isStreamingResponse reports whether a response should be relayed
// chunk-by-chunk instead of buffered. Anything without a known content length
// (chunked transfer encoding, connection-close streams) is streamed — this
// covers SSE, k8s watch/informer streams, and log follows alike.
func isStreamingResponse(resp *http.Response) bool {
	if strings.Contains(resp.Header.Get("Content-Type"), "text/event-stream") {
		return true
	}
	if resp.ContentLength < 0 {
		return true
	}
	for _, te := range resp.TransferEncoding {
		if te == "chunked" {
			return true
		}
	}
	return false
}

// streamHttpResponse relays the response body as raw byte chunks. Ordering is
// guaranteed by the tunnel (single websocket, synchronous dispatch).
func streamHttpResponse(
	tunnel *shared.Tunnel,
	id string,
	payload protocol.HttpRequestPayload,
	resp *http.Response,
	statsProvider stats.StatsProvider,
	l log.Logger,
	startTime time.Time,
) {
	statsProvider.IncrementSseConnection()
	defer statsProvider.DecrementSseConnection()
	statsProvider.IncrementHttpResponse()

	l.Info("http stream started", "status", resp.StatusCode, "method", payload.Method, "path", payload.Path)

	if err := tunnel.SendResponse(protocol.MessageKindHttpResponseStart, id, &protocol.HttpResponseStartPayload{
		Status:  resp.StatusCode,
		Headers: resp.Header,
	}); err != nil {
		l.Error("failed to send stream start", "error", err.Error())
		return
	}

	buf := make([]byte, 32*1024)
	for {
		n, err := resp.Body.Read(buf)
		if n > 0 {
			chunk := make([]byte, n)
			copy(chunk, buf[:n])
			if sendErr := tunnel.SendResponse(protocol.MessageKindHttpResponseChunk, id, &protocol.HttpResponseChunkPayload{Data: chunk}); sendErr != nil {
				l.Error("failed to send stream chunk", "error", sendErr.Error())
				return
			}
			statsProvider.IncrementSseMessageRecv()
		}
		if err != nil {
			endPayload := &protocol.HttpResponseEndPayload{}
			if err != io.EOF && !errors.Is(err, context.Canceled) {
				endPayload.Error = err.Error()
			}
			if !tunnel.IsClosed() {
				if sendErr := tunnel.SendResponse(protocol.MessageKindHttpResponseEnd, id, endPayload); sendErr != nil {
					l.Error("failed to send stream end", "error", sendErr.Error())
				}
			}
			l.Info("http stream ended", "method", payload.Method, "path", payload.Path, "elapsed", time.Since(startTime), "error", endPayload.Error)
			return
		}
	}
}

// targetTLSConfig builds the TLS config used for connections to the target
// (HTTP and websocket). Verification can be relaxed with TargetInsecure or
// pinned to a custom CA bundle with TargetCAFile. The legacy Insecure flag is
// honored for backwards compatibility.
func targetTLSConfig(options Options) (*tls.Config, error) {
	cfg := &tls.Config{InsecureSkipVerify: options.Insecure || options.TargetInsecure}
	if options.TargetCAFile != "" {
		pem, err := os.ReadFile(options.TargetCAFile)
		if err != nil {
			return nil, fmt.Errorf("read target CA file: %w", err)
		}
		pool := x509.NewCertPool()
		if !pool.AppendCertsFromPEM(pem) {
			return nil, fmt.Errorf("no valid certificates in target CA file %s", options.TargetCAFile)
		}
		cfg.RootCAs = pool
		cfg.InsecureSkipVerify = options.Insecure // CA given: verify against it unless server conn is insecure-forced
		if options.TargetInsecure {
			cfg.InsecureSkipVerify = true
		}
	}
	return cfg, nil
}

func handleWebsocketCreateRequest(
	ctx context.Context,
	tunnel *shared.Tunnel,
	id string,
	payload protocol.WebsocketCreateRequestPayload,
	options Options,
	wsSessions *safe.Map[string, *safe.WSConn],
	statsProvider stats.StatsProvider,
	l log.Logger,
) {
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

		targetTLS, tlsErr := targetTLSConfig(options)
		if tlsErr != nil {
			tunnel.SendResponse(protocol.MessageKindWebsocketCreateResponse, id, &protocol.WebsocketCreateResponsePayload{Error: tlsErr})
			return
		}
		wsDialer := &websocket.Dialer{
			Proxy:            http.ProxyFromEnvironment,
			HandshakeTimeout: 45 * time.Second,
			TLSClientConfig:  targetTLS,
		}
		rawConn, resp, err := wsDialer.DialContext(ctx, wsUrl.String()+payload.Path, wsHeaders)
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

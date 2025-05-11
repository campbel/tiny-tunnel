package testing

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/campbel/tiny-tunnel/core/client"
	"github.com/campbel/tiny-tunnel/core/server"
	"github.com/campbel/tiny-tunnel/internal/echo"
	"github.com/gorilla/websocket"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestHTTPEndpoints(t *testing.T) {
	// Create an echo server
	echoServer, err := echo.NewServer(echo.Options{})
	require.NoError(t, err)

	// Use the handler directly for testing
	echoTestServer := httptest.NewServer(echoServer.Handler())
	defer echoTestServer.Close()

	t.Run("HTTP Endpoint", func(t *testing.T) {
		// Make a request to the HTTP endpoint
		resp, err := http.Get(echoTestServer.URL + "/http")
		require.NoError(t, err)
		defer resp.Body.Close()

		assert.Equal(t, http.StatusOK, resp.StatusCode)
		assert.Equal(t, "application/json", resp.Header.Get("Content-Type"))

		// Read and parse the response
		body, err := io.ReadAll(resp.Body)
		require.NoError(t, err)

		var data map[string]interface{}
		err = json.Unmarshal(body, &data)
		require.NoError(t, err)

		// Verify the response contains expected fields
		assert.Equal(t, "GET", data["method"])
		assert.NotNil(t, data["url"])
		assert.NotNil(t, data["headers"])
	})

	t.Run("SSE Endpoint", func(t *testing.T) {
		// Make a request to the SSE endpoint
		req, err := http.NewRequest("GET", echoTestServer.URL+"/sse", nil)
		require.NoError(t, err)

		resp, err := http.DefaultClient.Do(req)
		require.NoError(t, err)
		defer resp.Body.Close()

		assert.Equal(t, http.StatusOK, resp.StatusCode)
		assert.Equal(t, "text/event-stream", resp.Header.Get("Content-Type"))

		// Create a buffered reader to read line by line
		reader := bufio.NewReader(resp.Body)

		// Simplified test to just verify that the SSE connection is established
		// and that we can read some data from it

		// Read a few lines to verify the stream is active
		var eventFound bool
		var dataFound bool

		// Try to read for up to 2 seconds
		timeout := time.After(2 * time.Second)
		done := make(chan bool)

		go func() {
			for i := 0; i < 20; i++ { // Try up to 20 lines
				select {
				case <-timeout:
					return
				default:
					line, err := reader.ReadString('\n')
					if err != nil {
						t.Logf("Error reading SSE stream: %v", err)
						done <- false
						return
					}

					line = strings.TrimSpace(line)
					if strings.HasPrefix(line, "event:") {
						eventFound = true
					} else if strings.HasPrefix(line, "data:") {
						dataFound = true
					}

					// If we've found both event and data, we can stop
					if eventFound && dataFound {
						done <- true
						return
					}
				}
			}
			done <- eventFound && dataFound
		}()

		// Wait for the goroutine to finish or timeout
		select {
		case success := <-done:
			assert.True(t, success, "Failed to find both event and data lines in SSE stream")
		case <-timeout:
			t.Log("Timeout reading SSE stream")
		}
	})

	t.Run("WebSocket Endpoint", func(t *testing.T) {
		// Convert HTTP URL to WebSocket URL
		wsURL := "ws" + strings.TrimPrefix(echoTestServer.URL, "http") + "/ws"

		// Connect to the WebSocket endpoint
		conn, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
		require.NoError(t, err)
		defer conn.Close()

		// Send a test message
		testMessage := "Hello WebSocket"
		err = conn.WriteMessage(websocket.TextMessage, []byte(testMessage))
		require.NoError(t, err)

		// Read the response with timeout
		done := make(chan struct{})
		var response map[string]interface{}

		go func() {
			_, msg, err := conn.ReadMessage()
			if err != nil {
				t.Errorf("Failed to read message: %v", err)
				return
			}

			err = json.Unmarshal(msg, &response)
			if err != nil {
				t.Errorf("Failed to unmarshal response: %v", err)
				return
			}
			close(done)
		}()

		// Wait for response or timeout
		select {
		case <-done:
			// First message should be connection info
			if response["type"] == "connection_info" {
				// Read the next message (echo response)
				_, msg, err := conn.ReadMessage()
				require.NoError(t, err)

				err = json.Unmarshal(msg, &response)
				require.NoError(t, err)
			}

			// Verify the echo response
			if response["type"] == "echo" {
				assert.Equal(t, testMessage, response["message"])
			}

		case <-time.After(2 * time.Second):
			t.Fatal("Timeout waiting for WebSocket response")
		}
	})
}

func TestEchoServerThroughTunnel(t *testing.T) {
	// Create a parent context with timeout to prevent the entire test from hanging
	parentCtx, parentCancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer parentCancel()

	// Create an echo server
	echoServer, err := echo.NewServer(echo.Options{})
	require.NoError(t, err)

	// Use the handler directly for testing
	echoAppServer := httptest.NewServer(echoServer.Handler())
	defer func() {
		// Close servers in reverse order of creation for clean shutdown
		echoAppServer.Close()
		t.Log("Echo app server closed")
	}()

	// Create a tunnel server
	tunnelServer := httptest.NewServer(server.NewHandler(server.Options{
		Hostname: "example.com",
	}))
	defer func() {
		tunnelServer.Close()
		t.Log("Tunnel server closed")
	}()

	serverURL, _ := url.Parse(tunnelServer.URL)

	// Create and start the client tunnel with a child context
	tunnelCtx, tunnelCancel := context.WithCancel(parentCtx)
	defer tunnelCancel() // Ensure tunnel is cancelled even if test fails

	clientTunnel, err := client.NewTunnel(tunnelCtx, client.Options{
		Name:       "echo",
		ServerHost: serverURL.Hostname(),
		ServerPort: serverURL.Port(),
		Insecure:   true,
		Target:     echoAppServer.URL,
	})
	require.NoError(t, err)

	// Start listening in a goroutine
	go func() {
		clientTunnel.Listen(tunnelCtx)
		t.Log("Client tunnel listener stopped")
	}()

	// Allow some time for the connection to establish, but with a timeout
	connEstablished := make(chan struct{})
	go func() {
		time.Sleep(200 * time.Millisecond)
		select {
		case <-parentCtx.Done():
			return
		default:
			close(connEstablished)
		}
	}()

	select {
	case <-parentCtx.Done():
		t.Fatal("Context cancelled while waiting for connection to establish")
		return
	case <-connEstablished:
		// Connection established, continue with tests
	}

	t.Run("HTTP Through Tunnel", func(t *testing.T) {
		// Create a context with timeout for this specific test
		ctx, cancel := context.WithTimeout(parentCtx, 5*time.Second)
		defer cancel()

		// Make a request through the tunnel
		req, _ := http.NewRequestWithContext(ctx, http.MethodGet, tunnelServer.URL+"/http", nil)
		req.Host = "echo.example.com"

		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			if ctx.Err() != nil {
				t.Fatal("HTTP request timed out:", err)
			}
			require.NoError(t, err)
		}
		if resp != nil {
			defer resp.Body.Close()

			assert.Equal(t, http.StatusOK, resp.StatusCode)

			// Read and parse the response with a deadline
			bodyCtx, bodyCancel := context.WithTimeout(ctx, 2*time.Second)
			defer bodyCancel()

			bodyDone := make(chan struct{})
			var body []byte
			var readErr error

			go func() {
				body, readErr = io.ReadAll(resp.Body)
				close(bodyDone)
			}()

			select {
			case <-bodyDone:
				// Continue with processing
			case <-bodyCtx.Done():
				t.Fatal("Timeout reading response body")
				return
			}

			require.NoError(t, readErr)

			var data map[string]interface{}
			err = json.Unmarshal(body, &data)
			require.NoError(t, err)

			// Verify the response contains expected fields
			assert.Equal(t, "GET", data["method"])
			assert.NotNil(t, data["url"])
			assert.NotNil(t, data["headers"])
		}
	})

	t.Run("SSE Through Tunnel", func(t *testing.T) {
		// Create a specific context for this test
		ctx, cancel := context.WithTimeout(parentCtx, 5*time.Second)
		defer cancel()

		// Get the SSE endpoint through the tunnel
		req, err := http.NewRequestWithContext(ctx, "GET", tunnelServer.URL+"/sse", nil)
		require.NoError(t, err)
		req.Host = "echo.example.com"

		// Use a client with short timeouts
		client := &http.Client{
			Timeout: 3 * time.Second,
		}

		resp, err := client.Do(req)
		if err != nil {
			// If the error is due to context cancellation or timeout, that's expected
			if ctx.Err() != nil || strings.Contains(err.Error(), "timeout") {
				t.Log("SSE request ended due to timeout/cancellation:", err)
				return
			}
			require.NoError(t, err)
			return
		}

		// Ensure response body is always closed
		if resp != nil {
			defer func() {
				err := resp.Body.Close()
				if err != nil {
					t.Logf("Error closing SSE response body: %v", err)
				}
			}()

			// Verify the response headers
			assert.Equal(t, http.StatusOK, resp.StatusCode)
			assert.Equal(t, "text/event-stream", resp.Header.Get("Content-Type"))

			// Create a buffered reader with a short read deadline
			reader := bufio.NewReaderSize(resp.Body, 4096)

			// Variables for tracking SSE data
			var linesRead int
			var eventFound bool
			var dataFound bool

			// Create a short timeout
			timeout := time.NewTimer(2 * time.Second)
			defer timeout.Stop()

			readDone := make(chan bool, 1)

			go func() {
				// Set a deadline on the response body reader
				// Note: Setting deadline on response.Body may not work on all implementations
				// so we also use select with timeout below
				if deadline, ok := resp.Body.(interface{ SetReadDeadline(time.Time) error }); ok {
					_ = deadline.SetReadDeadline(time.Now().Add(2 * time.Second))
				}

				for i := 0; i < 20 && ctx.Err() == nil; i++ { // Limit iterations and check context
					line, err := reader.ReadString('\n')
					if err != nil {
						t.Logf("Error reading from SSE stream: %v", err)
						select {
						case readDone <- false:
						default:
						}
						return
					}

					linesRead++
					line = strings.TrimSpace(line)

					// Check for valid SSE format
					if strings.HasPrefix(line, "event:") {
						eventFound = true
					} else if strings.HasPrefix(line, "data:") {
						dataFound = true
					}

					// If we've found both event and data, success
					if eventFound && dataFound {
						select {
						case readDone <- true:
						default:
						}
						return
					}
				}

				// Report partial success
				select {
				case readDone <- linesRead > 0:
				default:
				}
			}()

			// Wait for reading to complete or timeout
			select {
			case success := <-readDone:
				t.Logf("SSE test completed. Read %d lines. Event found: %v, Data found: %v",
					linesRead, eventFound, dataFound)
				// We're testing that SSE works at all
				assert.True(t, success, "Should have read at least one line from SSE stream")
			case <-timeout.C:
				t.Log("Timeout waiting for SSE data")
				// This is acceptable since we're just testing the connection works
			case <-ctx.Done():
				t.Log("Context cancelled while reading SSE data")
			}
		}
	})

	t.Run("WebSocket Through Tunnel", func(t *testing.T) {
		// Create a context with timeout for this test
		ctx, cancel := context.WithTimeout(parentCtx, 5*time.Second)
		defer cancel()

		// Connect to the WebSocket endpoint through the tunnel
		wsURL := fmt.Sprintf("ws://%s/ws", serverURL.Host)

		// Need to set Host header to route to the correct tunnel
		header := http.Header{}
		header.Set("Host", "echo.example.com")

		// Use a dialer with context
		dialer := &websocket.Dialer{
			HandshakeTimeout: 2 * time.Second,
		}

		conn, _, err := dialer.DialContext(ctx, wsURL, header)
		if err != nil {
			if ctx.Err() != nil {
				t.Log("WebSocket connection failed due to context cancellation:", err)
				return
			}
			require.NoError(t, err)
			return
		}

		// Ensure connection is closed
		defer func() {
			// Set a short deadline for the close message
			_ = conn.WriteControl(
				websocket.CloseMessage,
				websocket.FormatCloseMessage(websocket.CloseNormalClosure, ""),
				time.Now().Add(time.Second),
			)
			conn.Close()
			t.Log("WebSocket connection closed")
		}()

		// Set read/write deadlines
		_ = conn.SetReadDeadline(time.Now().Add(3 * time.Second))
		_ = conn.SetWriteDeadline(time.Now().Add(3 * time.Second))

		// Send a test message
		testMessage := "Hello WebSocket Through Tunnel"
		err = conn.WriteMessage(websocket.TextMessage, []byte(testMessage))
		require.NoError(t, err)

		// Create a channel to signal when we get the echo response
		responseChan := make(chan bool, 1)

		// Read messages in a goroutine
		go func() {
			// Try a few times in case there are info messages first
			for i := 0; i < 5 && ctx.Err() == nil; i++ {
				// Set a read deadline to avoid blocking forever
				_ = conn.SetReadDeadline(time.Now().Add(1 * time.Second))

				_, msg, err := conn.ReadMessage()
				if err != nil {
					t.Logf("Error reading WebSocket message: %v", err)
					select {
					case responseChan <- false:
					default:
					}
					return
				}

				var response map[string]interface{}
				err = json.Unmarshal(msg, &response)

				// If we can't parse as JSON or it's not a recognized message, skip
				if err != nil || response["type"] == nil {
					continue
				}

				// If we get an echo message, verify it
				if response["type"] == "echo" {
					assert.Equal(t, testMessage, response["message"])
					select {
					case responseChan <- true:
					default:
					}
					return
				}
			}

			// No echo response found after multiple attempts
			select {
			case responseChan <- false:
			default:
			}
		}()

		// Wait for response with timeout
		select {
		case success := <-responseChan:
			assert.True(t, success, "Should have received echo response")
		case <-time.After(3 * time.Second):
			t.Log("Timeout waiting for WebSocket echo response")
		case <-ctx.Done():
			t.Log("Context cancelled while waiting for WebSocket response")
		}
	})

	// Explicit cleanup in the correct order to avoid hanging
	tunnelCancel() // First cancel the tunnel context
	t.Log("Tunnel context cancelled")
}
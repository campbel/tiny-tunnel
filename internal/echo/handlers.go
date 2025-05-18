package echo

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/campbel/tiny-tunnel/internal/log"
	"github.com/campbel/tiny-tunnel/internal/safe"
	"github.com/gorilla/websocket"
)

var upgrader = websocket.Upgrader{
	ReadBufferSize:  1024,
	WriteBufferSize: 1024,
	CheckOrigin: func(r *http.Request) bool {
		return true // Allow all connections for testing
	},
}

// HandleHTTP handles standard HTTP requests
func HandleHTTP(l log.Logger) func(w http.ResponseWriter, r *http.Request) {
	return func(w http.ResponseWriter, r *http.Request) {
		l.Info("handling HTTP request", "method", r.Method, "path", r.URL.Path)
		body, _ := io.ReadAll(r.Body)
		response := map[string]any{
			"method":  r.Method,
			"url":     r.URL,
			"headers": r.Header,
			"body":    body,
		}
		data, _ := json.Marshal(response)
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, string(data))
	}
}

// HandleSSE handles Server-Sent Events requests
func HandleSSE(l log.Logger) func(w http.ResponseWriter, r *http.Request) {
	return func(w http.ResponseWriter, r *http.Request) {
		l.Info("handling SSE request", "method", r.Method, "path", r.URL.Path, "remote_addr", r.RemoteAddr, "user_agent", r.UserAgent())

		// Set headers for SSE
		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")
		w.Header().Set("Connection", "keep-alive")
		w.Header().Set("Access-Control-Allow-Origin", "*")
		l.Debug("SSE headers set", "content_type", w.Header().Get("Content-Type"))

		// Flush headers
		if f, ok := w.(http.Flusher); ok {
			f.Flush()
			l.Debug("SSE headers flushed")
		} else {
			l.Error("streaming not supported by ResponseWriter")
			http.Error(w, "Streaming not supported", http.StatusInternalServerError)
			return
		}

		// Create a channel to detect client disconnect
		notify := r.Context().Done()
		l.Debug("SSE disconnect detection setup")

		// Send initial connection message
		connectMsg := "event: connect\ndata: SSE Connection established\n\n"
		fmt.Fprint(w, connectMsg)
		l.Debug("SSE connectivity message sent", "message", strings.ReplaceAll(connectMsg, "\n", "\\n"))
		if f, ok := w.(http.Flusher); ok {
			f.Flush()
		}

		// Count to 5 and then close
		maxCount := 5
		for counter := 1; counter <= maxCount; counter++ {
			select {
			case <-notify:
				l.Info("SSE client disconnected", "total_messages_sent", counter-1)
				return
			default:
				// Send count as SSE event
				message := fmt.Sprintf("event: count\ndata: %d\n\n", counter)
				fmt.Fprint(w, message)
				l.Debug("SSE count sent", "counter", counter, "message", strings.ReplaceAll(message, "\n", "\\n"))

				if f, ok := w.(http.Flusher); ok {
					f.Flush()
				}
				time.Sleep(1 * time.Second)
			}
		}

		// Send final message indicating completion
		completeMsg := "event: complete\ndata: Count complete\n\n"
		fmt.Fprint(w, completeMsg)
		l.Debug("SSE completion message sent", "message", strings.ReplaceAll(completeMsg, "\n", "\\n"))
		if f, ok := w.(http.Flusher); ok {
			f.Flush()
		}

		l.Info("SSE count complete, connection closing", "total_messages_sent", maxCount+2)
	}
}

// HandleWebSocket handles WebSocket connections
func HandleWebSocket(l log.Logger) func(w http.ResponseWriter, r *http.Request) {
	return func(w http.ResponseWriter, r *http.Request) {
		l.Info("handling WebSocket request", "method", r.Method, "path", r.URL.Path)

		// Upgrade HTTP connection to WebSocket
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			l.Error("Failed to upgrade connection", "error", err)
			return
		}

		safeConn := safe.NewWSConn(conn)
		defer safeConn.Close()

		l.Info("WebSocket connection established", "client", r.RemoteAddr)

		// Send initial connection info
		info := map[string]any{
			"type":    "connection_info",
			"method":  r.Method,
			"url":     r.URL.String(),
			"path":    r.URL.Path,
			"headers": r.Header,
			"time":    time.Now().Format(time.RFC3339),
		}
		if err := safeConn.WriteJSON(info); err != nil {
			l.Error("Failed to send connection info", "error", err)
			return
		}

		// Create a channel to detect client disconnect
		done := make(chan struct{})

		// Start a goroutine to read messages from the client
		go func() {
			defer close(done)
			for {
				messageType, message, err := safeConn.ReadMessage()
				if err != nil {
					if !strings.Contains(err.Error(), "websocket: close") {
						l.Error("WebSocket read error", "error", err)
					}
					return
				}

				// Echo the message back to the client
				response := map[string]any{
					"type":    "echo",
					"message": string(message),
					"time":    time.Now().Format(time.RFC3339),
				}

				if messageType == websocket.TextMessage {
					if err := safeConn.WriteJSON(response); err != nil {
						l.Error("Failed to send echo response", "error", err)
						return
					}
				} else {
					if err := safeConn.WriteMessage(messageType, message); err != nil {
						l.Error("Failed to echo binary message", "error", err)
						return
					}
				}
			}
		}()

		// Send counter updates every second
		counter := 1
		ticker := time.NewTicker(1 * time.Second)
		defer ticker.Stop()

		for {
			select {
			case <-done:
				l.Info("WebSocket client disconnected")
				return
			case <-ticker.C:
				update := map[string]any{
					"type":  "counter",
					"count": counter,
					"time":  time.Now().Format(time.RFC3339),
				}
				if err := safeConn.WriteJSON(update); err != nil {
					l.Error("Failed to send counter update", "error", err)
					return
				}
				counter++
			}
		}
	}
}

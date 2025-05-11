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
func HandleHTTP(w http.ResponseWriter, r *http.Request) {
	log.Info("handling HTTP request", "method", r.Method, "path", r.URL.Path)
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

// HandleSSE handles Server-Sent Events requests
func HandleSSE(w http.ResponseWriter, r *http.Request) {
	log.Info("handling SSE request", "method", r.Method, "path", r.URL.Path, "remote_addr", r.RemoteAddr, "user_agent", r.UserAgent())

	// Set headers for SSE
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("Access-Control-Allow-Origin", "*")
	log.Debug("SSE headers set", "content_type", w.Header().Get("Content-Type"))

	// Flush headers
	if f, ok := w.(http.Flusher); ok {
		f.Flush()
		log.Debug("SSE headers flushed")
	} else {
		log.Error("streaming not supported by ResponseWriter")
		http.Error(w, "Streaming not supported", http.StatusInternalServerError)
		return
	}

	// Create a channel to detect client disconnect
	notify := r.Context().Done()
	log.Debug("SSE disconnect detection setup")

	// Send counter as SSE events
	counter := 1
	for {
		select {
		case <-notify:
			log.Info("SSE client disconnected", "total_messages_sent", counter-1)
			return
		default:
			// Simplify the SSE format to just send a simple count
			// This simple format is more likely to work consistently
			message := fmt.Sprintf("data: %d\n\n", counter)
			fmt.Fprint(w, message)
			log.Debug("SSE data sent", "counter", counter, "message", message)

			// For debugging, add a raw number every 5 counts (not valid SSE format)
			if counter%5 == 0 {
				// This is intentionally not valid SSE format to test tunnel
				checkMsg := fmt.Sprintf("CHECKMARK COUNT %d\n\n", counter)
				fmt.Fprint(w, checkMsg)
				log.Debug("Sending checkpoint message (invalid format)", "message", checkMsg)
			}

			if f, ok := w.(http.Flusher); ok {
				f.Flush()
			}
			counter++
			time.Sleep(1 * time.Second)
		}
	}
}

// HandleWebSocket handles WebSocket connections
func HandleWebSocket(w http.ResponseWriter, r *http.Request) {
	log.Info("handling WebSocket request", "method", r.Method, "path", r.URL.Path)

	// Upgrade HTTP connection to WebSocket
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Error("Failed to upgrade connection", "error", err)
		return
	}

	safeConn := safe.NewWSConn(conn)
	defer safeConn.Close()

	log.Info("WebSocket connection established", "client", r.RemoteAddr)

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
		log.Error("Failed to send connection info", "error", err)
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
					log.Error("WebSocket read error", "error", err)
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
					log.Error("Failed to send echo response", "error", err)
					return
				}
			} else {
				if err := safeConn.WriteMessage(messageType, message); err != nil {
					log.Error("Failed to echo binary message", "error", err)
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
			log.Info("WebSocket client disconnected")
			return
		case <-ticker.C:
			update := map[string]any{
				"type":  "counter",
				"count": counter,
				"time":  time.Now().Format(time.RFC3339),
			}
			if err := safeConn.WriteJSON(update); err != nil {
				log.Error("Failed to send counter update", "error", err)
				return
			}
			counter++
		}
	}
}

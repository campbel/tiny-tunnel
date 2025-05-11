package main

import (
	"fmt"
	"net/http"
	"os"
	"time"

	"github.com/campbel/tiny-tunnel/internal/log"
)

func handleSSE(w http.ResponseWriter, r *http.Request) {
	log.Info("handling SSE request", "path", r.URL.Path)

	// Set headers for SSE
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("Access-Control-Allow-Origin", "*")

	// Flush headers
	if f, ok := w.(http.Flusher); ok {
		f.Flush()
	} else {
		http.Error(w, "Streaming not supported", http.StatusInternalServerError)
		return
	}

	// Send a direct message (no event type)
	fmt.Fprintf(w, "data: Connection established\n\n")
	if f, ok := w.(http.Flusher); ok {
		f.Flush()
	}
	log.Info("sent direct message")

	// Create a channel to detect client disconnect
	notify := r.Context().Done()

	// Start counting
	counter := 1
	for {
		select {
		case <-notify:
			log.Info("Client disconnected")
			return
		default:
			if counter%3 == 0 {
				// Every third message: send with event type
				fmt.Fprintf(w, "event: count\ndata: %d\n\n", counter)
				log.Info("sent message with event type", "counter", counter)
			} else {
				// Other messages: send without event type
				fmt.Fprintf(w, "data: %d\n\n", counter)
				log.Info("sent message without event type", "counter", counter)
			}

			if f, ok := w.(http.Flusher); ok {
				f.Flush()
			}
			counter++
			time.Sleep(1 * time.Second)
		}
	}
}

func handleRoot(w http.ResponseWriter, r *http.Request) {
	// Simple HTML page that connects to the SSE endpoint
	html := `
<!DOCTYPE html>
<html>
<head>
    <title>SSE Test</title>
    <style>
        body { font-family: Arial, sans-serif; margin: 20px; }
        #log { border: 1px solid #ccc; padding: 10px; height: 300px; overflow-y: scroll; margin-top: 20px; }
        .entry { margin-bottom: 5px; }
        .time { color: #999; font-size: 0.8em; }
    </style>
</head>
<body>
    <h1>SSE Test Page</h1>
    <button id="connect">Connect</button>
    <button id="disconnect" disabled>Disconnect</button>
    <div id="status">Disconnected</div>
    <div id="count">Count: -</div>
    <h3>Event Log:</h3>
    <div id="log"></div>

    <script>
        let eventSource = null;
        const logEl = document.getElementById('log');
        const statusEl = document.getElementById('status');
        const countEl = document.getElementById('count');
        const connectBtn = document.getElementById('connect');
        const disconnectBtn = document.getElementById('disconnect');

        function log(message, type = 'info') {
            const entry = document.createElement('div');
            entry.className = 'entry ' + type;
            const time = new Date().toLocaleTimeString();
            entry.innerHTML = '<span class="time">[' + time + ']</span> ' + message;
            logEl.appendChild(entry);
            logEl.scrollTop = logEl.scrollHeight;
        }

        function connect() {
            if (eventSource) {
                eventSource.close();
            }
            
            log('Connecting to SSE endpoint...', 'info');
            statusEl.textContent = 'Connecting...';
            
            eventSource = new EventSource('/sse');
            
            // Connection opened
            eventSource.onopen = function(e) {
                log('Connection opened', 'success');
                statusEl.textContent = 'Connected';
                connectBtn.disabled = true;
                disconnectBtn.disabled = false;
            };
            
            // Listen for messages (default event handler)
            eventSource.onmessage = function(e) {
                log('Message received: ' + e.data, 'info');
                countEl.textContent = 'Count: ' + e.data;
            };
            
            // Listen for 'count' events
            eventSource.addEventListener('count', function(e) {
                log('Count event received: ' + e.data, 'event');
                countEl.textContent = 'Count: ' + e.data + ' (from event)';
            });
            
            // Connection error
            eventSource.onerror = function(e) {
                log('Error: Connection failed or closed', 'error');
                statusEl.textContent = 'Error: Connection failed';
                connectBtn.disabled = false;
                disconnectBtn.disabled = true;
                eventSource.close();
            };
        }
        
        function disconnect() {
            if (eventSource) {
                log('Disconnecting...', 'info');
                eventSource.close();
                eventSource = null;
                statusEl.textContent = 'Disconnected';
                countEl.textContent = 'Count: -';
                connectBtn.disabled = false;
                disconnectBtn.disabled = true;
            }
        }
        
        connectBtn.addEventListener('click', connect);
        disconnectBtn.addEventListener('click', disconnect);
        
        // Connect automatically when page loads
        window.onload = connect;
    </script>
</body>
</html>
`
	w.Header().Set("Content-Type", "text/html")
	fmt.Fprint(w, html)
}

func main() {
	port := "8085"
	if envPort := os.Getenv("PORT"); envPort != "" {
		port = envPort
	} else if len(os.Args) > 1 {
		port = os.Args[1]
	}

	// Set up routes
	http.HandleFunc("/", handleRoot)
	http.HandleFunc("/sse", handleSSE)

	// Start server
	addr := ":" + port
	log.Info("Server starting", "port", port, "url", "http://localhost:"+port)
	if err := http.ListenAndServe(addr, nil); err != nil {
		log.Error("Server failed", "error", err)
		os.Exit(1)
	}
}
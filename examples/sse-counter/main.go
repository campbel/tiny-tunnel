package main

import (
	"embed"
	"fmt"
	"io/fs"
	"net/http"
	"os"
	"time"

	"github.com/charmbracelet/log"
)

//go:embed static
var staticFiles embed.FS

func handleSSE(w http.ResponseWriter, r *http.Request) {
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
			// Send counter as SSE event
			fmt.Fprintf(w, "data: %d\n\n", counter)
			if f, ok := w.(http.Flusher); ok {
				f.Flush()
			}
			counter++
			time.Sleep(1 * time.Second)
		}
	}
}

func main() {
	port := "8023"
	if envPort := os.Getenv("PORT"); envPort != "" {
		port = envPort
	} else if len(os.Args) > 1 {
		port = os.Args[1]
	}

	// Serve static files from embedded filesystem
	staticFS, err := fs.Sub(staticFiles, "static")
	if err != nil {
		log.Fatal("Failed to create sub filesystem", "error", err)
	}
	http.Handle("/", http.FileServer(http.FS(staticFS)))
	
	// SSE endpoint
	http.HandleFunc("/events", handleSSE)

	log.Info("SSE counter server starting", "port", port, "address", "http://localhost:"+port)
	if err := http.ListenAndServe(":"+port, nil); err != nil {
		log.Fatal("Server failed", "error", err)
	}
}
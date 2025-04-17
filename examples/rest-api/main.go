package main

import (
	"embed"
	"encoding/json"
	"io/fs"
	"net/http"
	"os"

	"github.com/charmbracelet/log"
)

//go:embed static
var staticFiles embed.FS

type Message struct {
	Text string `json:"text"`
}

func main() {
	port := "8021"
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

	// GET endpoint
	http.HandleFunc("/api/message", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}
		
		msg := Message{Text: "Hello from GET endpoint"}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(msg)
	})

	// POST endpoint
	http.HandleFunc("/api/submit", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}

		var msg Message
		if err := json.NewDecoder(r.Body).Decode(&msg); err != nil {
			http.Error(w, "Bad request", http.StatusBadRequest)
			return
		}

		// Echo back the received message
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(Message{Text: "Received: " + msg.Text})
	})

	// PUT endpoint
	http.HandleFunc("/api/update", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPut {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}

		var msg Message
		if err := json.NewDecoder(r.Body).Decode(&msg); err != nil {
			http.Error(w, "Bad request", http.StatusBadRequest)
			return
		}

		// Echo back with PUT confirmation
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(Message{Text: "Updated: " + msg.Text})
	})

	// DELETE endpoint
	http.HandleFunc("/api/delete", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodDelete {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(Message{Text: "Resource deleted"})
	})

	log.Info("REST API server starting", "port", port, "address", "http://localhost:"+port)
	if err := http.ListenAndServe(":"+port, nil); err != nil {
		log.Fatal("Server failed", "error", err)
	}
}
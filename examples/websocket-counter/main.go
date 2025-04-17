package main

import (
	"embed"
	"io/fs"
	"net/http"
	"os"
	"strconv"
	"time"

	"github.com/charmbracelet/log"
	"github.com/gorilla/websocket"
)

//go:embed static
var staticFiles embed.FS

var upgrader = websocket.Upgrader{
	ReadBufferSize:  1024,
	WriteBufferSize: 1024,
	CheckOrigin: func(r *http.Request) bool {
		return true // Allow all connections for testing
	},
}

func handleWebsocket(w http.ResponseWriter, r *http.Request) {
	// Upgrade HTTP connection to WebSocket
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Error("Failed to upgrade connection", "error", err)
		return
	}
	defer conn.Close()

	log.Info("WebSocket connection established", "client", r.RemoteAddr)

	// Start counting
	counter := 1
	for {
		// Send counter value
		err := conn.WriteMessage(websocket.TextMessage, []byte(strconv.Itoa(counter)))
		if err != nil {
			log.Error("Write error", "error", err)
			break
		}
		
		counter++
		time.Sleep(1 * time.Second)
	}
}

func main() {
	port := "8022"
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
	
	// WebSocket endpoint
	http.HandleFunc("/ws", handleWebsocket)

	log.Info("WebSocket counter server starting", "port", port, "address", "http://localhost:"+port)
	if err := http.ListenAndServe(":"+port, nil); err != nil {
		log.Fatal("Server failed", "error", err)
	}
}
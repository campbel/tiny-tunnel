package echo

import (
	"context"
	"embed"
	"fmt"
	"io/fs"
	"net/http"
	"time"

	"github.com/campbel/tiny-tunnel/internal/log"
)

//go:embed static
var staticFiles embed.FS

// Options defines the configuration options for the echo server
type Options struct {
	Port string
}

// Server represents an echo server instance
type Server struct {
	options Options
	server  *http.Server
	mux     *http.ServeMux
	handler http.Handler
}

// NewServer creates a new echo server with the given options
func NewServer(options Options) (*Server, error) {
	if options.Port == "" {
		options.Port = "8000" // Default port
	}

	mux := http.NewServeMux()

	// Set up static file serving for the demo page
	staticFS, err := fs.Sub(staticFiles, "static")
	if err != nil {
		return nil, fmt.Errorf("failed to create sub filesystem: %w", err)
	}
	fileServer := http.FileServer(http.FS(staticFS))

	// Root handler serves the demo homepage
	mux.HandleFunc("/", fileServer.ServeHTTP)

	// HTTP endpoint
	mux.HandleFunc("/http", HandleHTTP)

	// Server-Sent Events endpoint
	mux.HandleFunc("/sse", HandleSSE)

	// WebSocket endpoint
	mux.HandleFunc("/ws", HandleWebSocket)

	server := &Server{
		options: options,
		mux:     mux,
		handler: mux,
		server: &http.Server{
			Addr:    ":" + options.Port,
			Handler: mux,
		},
	}

	return server, nil
}

// Start starts the echo server in the background
func (s *Server) Start() error {
	go func() {
		if err := s.server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Error("error starting server", "err", err)
		}
	}()

	log.Info("echo server running", 
		"demo", fmt.Sprintf("http://localhost:%s/", s.options.Port),
		"http", fmt.Sprintf("http://localhost:%s/http", s.options.Port),
		"sse", fmt.Sprintf("http://localhost:%s/sse", s.options.Port),
		"ws", fmt.Sprintf("ws://localhost:%s/ws", s.options.Port))

	return nil
}

// Shutdown gracefully shuts down the echo server
func (s *Server) Shutdown(ctx context.Context) error {
	log.Info("shutting down echo server")
	shutdownCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	err := s.server.Shutdown(shutdownCtx)
	if err != nil {
		if err == http.ErrServerClosed {
			log.Info("server closed")
		} else {
			log.Error("error shutting down server", "err", err)
			return err
		}
	}

	return nil
}

// Handler returns the HTTP handler for the echo server
// This is useful for testing or embedding the echo server in another application
func (s *Server) Handler() http.Handler {
	return s.handler
}
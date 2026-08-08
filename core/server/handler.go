package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/campbel/tiny-tunnel/core/server/ui"
	"github.com/campbel/tiny-tunnel/internal/guardian"
	"github.com/campbel/tiny-tunnel/internal/log"
	"github.com/campbel/tiny-tunnel/internal/safe"
	"github.com/gorilla/mux"
	"github.com/gorilla/websocket"
)

// contextKey is a custom type for context keys to avoid string collisions
type contextKey string

const (
	identityContextKey contextKey = "identity"
)

type Handler struct {
	options  Options
	upgrader websocket.Upgrader
	tunnels  *safe.Map[string, *Tunnel]
	verifier *guardian.Verifier
	l        log.Logger
}

func NewHandler(options Options, logger log.Logger) http.Handler {
	server := &Handler{
		options: options,
		upgrader: websocket.Upgrader{
			CheckOrigin: func(r *http.Request) bool {
				return true
			},
		},
		tunnels: safe.NewMap[string, *Tunnel](),
		l:       logger,
	}

	if options.EnableAuth {
		server.verifier = guardian.NewVerifier(guardian.Config{
			URL:      options.GuardianURL,
			Audience: options.GuardianAudience,
		})
	}

	router := mux.NewRouter()
	router.Host(fmt.Sprintf("{tunnel:[a-z0-9-]+}.%s", options.Hostname)).HandlerFunc(server.HandleTunnelRequest)

	if options.EnableAuth {
		// Wrap /register with Guardian auth middleware. Credentials are
		// Guardian user JWTs (verified locally via JWKS) or dch_ API keys
		// (resolved against Guardian). Token minting, login pages, and the
		// okta header dance all live in Guardian now — not here.
		router.HandleFunc("/register", server.authTokenMiddleware(server.HandleRegister))
		// Serve static files for the UI
		router.PathPrefix("/static/").Handler(http.StripPrefix("/static/", ui.GetHandler()))
		router.HandleFunc("/", server.HandleRoot)
		router.HandleFunc("/api/auth-test", server.authTokenMiddleware(server.HandleAuthTest))
	} else {
		router.HandleFunc("/register", server.HandleRegister)
		router.HandleFunc("/", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Header.Get("X-TT-Tunnel") != "" {
				server.HandleTunnelRequest(w, r)
				return
			}
			fmt.Fprintf(w, "Welcome to Tiny Tunnel. See github.com/campbel/tiny-tunnel for more info.")
		}))
	}

	return router
}

func (s *Handler) HandleRoot(w http.ResponseWriter, r *http.Request) {
	if r.Header.Get("X-TT-Tunnel") != "" {
		s.HandleTunnelRequest(w, r)
		return
	}

	// Serve our UI index.html
	indexData, err := ui.StaticFiles.ReadFile("static/index.html")
	if err != nil {
		s.l.Error("error reading index.html", "err", err)
		http.Error(w, "Error loading UI", http.StatusInternalServerError)
		return
	}

	// Convert to string to add the server host dynamically
	indexHTML := string(indexData)
	serverHost := r.Host

	// Replace the placeholder with the actual server host
	indexHTML = strings.Replace(indexHTML, `<span id="server-host">SERVER_HOST</span>`,
		fmt.Sprintf(`<span id="server-host">%s</span>`, serverHost), 1)

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Write([]byte(indexHTML))
}

func (s *Handler) HandleRegister(w http.ResponseWriter, r *http.Request) {
	name := r.FormValue("name")
	if name == "" {
		http.Error(w, "name is required", http.StatusBadRequest)
		return
	}

	// When auth is enabled the Guardian middleware has already verified the
	// credential; log who is registering.
	if s.options.EnableAuth {
		identity, ok := r.Context().Value(identityContextKey).(guardian.Identity)
		if !ok {
			http.Error(w, "unauthorized: missing identity", http.StatusUnauthorized)
			return
		}
		s.l.Info("tunnel registration attempt", "name", name, "user", identity.String(), "auth_method", identity.Method)
	}

	conn, err := s.upgrader.Upgrade(w, r, nil)
	if err != nil {
		s.l.Error("websocket upgrade failed", "err", err)
		http.Error(w, "", http.StatusInternalServerError)
		return
	}

	tunnel := NewTunnel(conn, TunnelOptions{
		HelloMessage: fmt.Sprintf("Welcome to Tiny Tunnel! Your tunnel is ready at %s", s.options.GetTunnelURL(name)),
	}, s.l)
	if !s.tunnels.SetNX(name, tunnel) {
		http.Error(w, "name is already used", http.StatusBadRequest)
		return
	}
	s.l.Info("registered tunnel", "name", name)

	tunnel.Listen(r.Context())

	s.tunnels.Delete(name)
	s.l.Info("unregistered tunnel", "name", name)
}

// getHeaderCaseInsensitive retrieves a header value using case-insensitive matching
func getHeaderCaseInsensitive(r *http.Request, header string) string {
	for key, values := range r.Header {
		if strings.EqualFold(key, header) && len(values) > 0 {
			return values[0]
		}
	}
	return ""
}

// authTokenMiddleware authenticates the X-Auth-Token (or Authorization)
// header against Guardian and stores the resolved identity in the request
// context. Accepts Guardian user JWTs and dch_ API keys.
func (s *Handler) authTokenMiddleware(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		credential := getHeaderCaseInsensitive(r, "X-Auth-Token")
		if credential == "" {
			credential = getHeaderCaseInsensitive(r, "Authorization")
		}
		if credential == "" {
			http.Error(w, "Unauthorized: Missing X-Auth-Token header", http.StatusUnauthorized)
			return
		}

		identity, err := s.verifier.Verify(r.Context(), credential)
		if err != nil {
			if errors.Is(err, guardian.ErrInvalidCredential) {
				s.l.Info("rejected credential", "err", err.Error())
				http.Error(w, "Unauthorized: Invalid token", http.StatusUnauthorized)
			} else {
				s.l.Error("guardian verification failed", "err", err.Error())
				http.Error(w, "Auth service unavailable", http.StatusBadGateway)
			}
			return
		}

		r = r.WithContext(context.WithValue(r.Context(), identityContextKey, identity))
		next(w, r)
	}
}

func (s *Handler) HandleAuthTest(w http.ResponseWriter, r *http.Request) {
	// Identity was resolved by authTokenMiddleware.
	identity, ok := r.Context().Value(identityContextKey).(guardian.Identity)
	if !ok {
		http.Error(w, "unauthorized: token validation failed", http.StatusUnauthorized)
		return
	}

	expires := ""
	if !identity.ExpiresAt.IsZero() {
		expires = identity.ExpiresAt.Format(time.RFC3339)
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{
		"valid":       true,
		"email":       identity.Email,
		"user":        identity.String(),
		"sub":         identity.Sub,
		"auth_method": identity.Method,
		"expires":     expires,
	})
}

func (s *Handler) HandleTunnelRequest(w http.ResponseWriter, r *http.Request) {
	tunnelID := mux.Vars(r)["tunnel"]
	if tunnelID == "" {
		tunnelID = r.Header.Get("X-TT-Tunnel")
	}

	if tunnelID == "" {
		http.Error(w, "tunnel name not provided", http.StatusBadRequest)
		return
	}

	tunnel, ok := s.tunnels.Get(tunnelID)
	if !ok {
		http.Error(w, "tunnel not found", http.StatusNotFound)
		return
	}
	tunnel.HandleHttpRequest(w, r)
}

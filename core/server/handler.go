package server

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/campbel/tiny-tunnel/core/server/ui"
	"github.com/campbel/tiny-tunnel/internal/log"
	"github.com/campbel/tiny-tunnel/internal/safe"
	"github.com/golang-jwt/jwt/v5"
	"github.com/gorilla/mux"
	"github.com/gorilla/websocket"
)

// contextKey is a custom type for context keys to avoid string collisions
type contextKey string

const (
	claimsContextKey contextKey = "claims"
)

type Claims struct {
	Email  string   `json:"email"`
	Scopes []string `json:"scopes"`
	jwt.RegisteredClaims
}

type Handler struct {
	options  Options
	upgrader websocket.Upgrader
	tunnels  *safe.Map[string, *Tunnel]
}

func NewHandler(options Options) http.Handler {
	server := &Handler{
		options: options,
		upgrader: websocket.Upgrader{
			CheckOrigin: func(r *http.Request) bool {
				return true
			},
		},
		tunnels: safe.NewMap[string, *Tunnel](),
	}

	router := mux.NewRouter()
	router.Host(fmt.Sprintf("{tunnel:[a-z0-9-]+}.%s", options.Hostname)).HandlerFunc(server.HandleTunnelRequest)

	if options.EnableAuth {
		// Wrap /register with token auth middleware
		router.HandleFunc("/register", server.authTokenMiddleware(server.HandleRegister))
		// Serve static files for the UI
		router.PathPrefix("/static/").Handler(http.StripPrefix("/static/", ui.GetHandler()))
		// API endpoint for token generation with email auth middleware
		router.HandleFunc("/login", server.authEmailMiddleware(server.HandleLogin))
		router.HandleFunc("/", server.HandleRoot)
		router.HandleFunc("/api/token", server.HandleGenerateToken)
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
		log.Error("error reading index.html", "err", err)
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

func (s *Handler) HandleLogin(w http.ResponseWriter, r *http.Request) {
	if r.Header.Get("X-TT-Tunnel") != "" {
		s.HandleTunnelRequest(w, r)
		return
	}

	// Serve our login.html
	loginData, err := ui.StaticFiles.ReadFile("static/login.html")
	if err != nil {
		log.Error("error reading login.html", "err", err)
		http.Error(w, "Error loading login page", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Write(loginData)
}

func (s *Handler) HandleRegister(w http.ResponseWriter, r *http.Request) {
	name := r.FormValue("name")
	if name == "" {
		http.Error(w, "name is required", http.StatusBadRequest)
		return
	}

	// Check if auth is enabled and validate claims
	if s.options.EnableAuth {
		claims, ok := r.Context().Value(claimsContextKey).(*Claims)
		if !ok {
			http.Error(w, "unauthorized: invalid token claims", http.StatusUnauthorized)
			return
		}

		// Verify the token has the tunnel:create scope
		hasScope := false
		for _, scope := range claims.Scopes {
			if scope == "tunnel:create" {
				hasScope = true
				break
			}
		}

		if !hasScope {
			http.Error(w, "forbidden: missing required scope", http.StatusForbidden)
			return
		}

		// Log the registration with user info
		log.Info("tunnel registration attempt", "name", name, "email", claims.Email)
	}

	conn, err := s.upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Error("websocket upgrade failed", "err", err)
		http.Error(w, "", http.StatusInternalServerError)
		return
	}

	tunnel := NewTunnel(conn, TunnelOptions{
		HelloMessage: fmt.Sprintf("Welcome to Tiny Tunnel! Your tunnel is ready at %s", s.options.GetTunnelURL(name)),
	})
	if !s.tunnels.SetNX(name, tunnel) {
		http.Error(w, "name is already used", http.StatusBadRequest)
		return
	}
	log.Info("registered tunnel", "name", name)

	tunnel.Listen(r.Context())

	s.tunnels.Delete(name)
	log.Info("unregistered tunnel", "name", name)
}

func (s *Handler) authEmailMiddleware(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		email := r.Header.Get("X-Auth-Request-Email")
		if email == "" {
			http.Error(w, "Unauthorized: Missing X-Auth-Request-Email header", http.StatusUnauthorized)
			return
		}
		// Email header exists, proceed to the next handler
		next(w, r)
	}
}

func (s *Handler) authTokenMiddleware(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		tokenHeader := r.Header.Get("X-Auth-Token")
		if tokenHeader == "" {
			http.Error(w, "Unauthorized: Missing X-Auth-Token header", http.StatusUnauthorized)
			return
		}

		// Validate JWT token
		token, err := jwt.ParseWithClaims(tokenHeader, &Claims{}, func(token *jwt.Token) (interface{}, error) {
			// Validate the signing method
			if _, ok := token.Method.(*jwt.SigningMethodHMAC); !ok {
				return nil, fmt.Errorf("unexpected signing method: %v", token.Header["alg"])
			}
			return []byte(s.options.GetJWTSecret()), nil
		})

		if err != nil {
			log.Error("token validation failed", "err", err)
			http.Error(w, "Unauthorized: Invalid token", http.StatusUnauthorized)
			return
		}

		if claims, ok := token.Claims.(*Claims); ok && token.Valid {
			// Add claims to request context
			r = r.WithContext(context.WithValue(r.Context(), claimsContextKey, claims))
			next(w, r)
		} else {
			http.Error(w, "Unauthorized: Invalid token claims", http.StatusUnauthorized)
		}
	}
}

func (s *Handler) HandleGenerateToken(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Get email from header
	email := r.Header.Get("X-Auth-Request-Email")
	if email == "" {
		http.Error(w, "Unauthorized: Missing X-Auth-Request-Email header", http.StatusUnauthorized)
		return
	}

	// Create JWT token with claims
	expirationTime := time.Now().Add(s.options.GetTokenExpiry())
	claims := &Claims{
		Email:  email,
		Scopes: []string{"tunnel:create"}, // Changed from tunnel:register, tunnel:access to just tunnel:create
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(expirationTime),
			IssuedAt:  jwt.NewNumericDate(time.Now()),
			Issuer:    "tiny-tunnel",
		},
	}

	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	tokenString, err := token.SignedString([]byte(s.options.GetJWTSecret()))
	if err != nil {
		log.Error("error signing token", "err", err)
		http.Error(w, "Error generating token", http.StatusInternalServerError)
		return
	}

	// Return the JWT token as JSON
	w.Header().Set("Content-Type", "application/json")
	w.Write([]byte(fmt.Sprintf(`{"token":"%s","expires":"%s"}`, tokenString, expirationTime.Format(time.RFC3339))))
}

func (s *Handler) HandleAuthTest(w http.ResponseWriter, r *http.Request) {
	// Get claims from context (already validated by authTokenMiddleware)
	claims, ok := r.Context().Value(claimsContextKey).(*Claims)
	if !ok {
		http.Error(w, "unauthorized: token validation failed", http.StatusUnauthorized)
		return
	}

	// Return the token information as JSON
	w.Header().Set("Content-Type", "application/json")
	w.Write([]byte(fmt.Sprintf(`{
		"valid": true,
		"email": "%s",
		"scopes": %q,
		"expires": "%s"
	}`, claims.Email, claims.Scopes, claims.ExpiresAt.Time.Format(time.RFC3339))))
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

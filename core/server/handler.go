package server

import (
	"fmt"
	"net/http"

	"github.com/campbel/tiny-tunnel/internal/log"
	"github.com/campbel/tiny-tunnel/internal/safe"
	"github.com/gorilla/mux"
	"github.com/gorilla/websocket"
)

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
	router.Host(fmt.Sprintf("{tunnel:[a-z]+}.%s", options.Hostname)).HandlerFunc(server.HandleTunnelRequest)
	router.HandleFunc("/register", server.HandleRegister)
	router.HandleFunc("/", server.HandleRoot)

	return router
}

func (s *Handler) HandleRoot(w http.ResponseWriter, r *http.Request) {
	if r.Header.Get("X-TT-Tunnel") != "" {
		s.HandleTunnelRequest(w, r)
		return
	}

	fmt.Fprint(w, "Welcome to Tiny Tunnel. See github.com/campbel/tiny-tunnel for more info.")
}

func (s *Handler) HandleRegister(w http.ResponseWriter, r *http.Request) {
	name := r.FormValue("name")
	if name == "" {
		http.Error(w, "name is required", http.StatusBadRequest)
		return
	}

	conn, err := s.upgrader.Upgrade(w, r, nil)
	if err != nil {
		http.Error(w, "", http.StatusInternalServerError)
		return
	}

	tunnel := NewTunnel(conn)
	if !s.tunnels.SetNX(name, tunnel) {
		http.Error(w, "name is already used", http.StatusBadRequest)
		return
	}
	log.Info("registered tunnel", "name", name)

	tunnel.Listen(r.Context())

	s.tunnels.Delete(name)
	log.Info("unregistered tunnel", "name", name)
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

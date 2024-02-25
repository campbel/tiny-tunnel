package server

import (
	"fmt"
	"net/http"
	"time"

	"github.com/campbel/tiny-tunnel/log"
	"github.com/campbel/tiny-tunnel/sync"
	"github.com/campbel/tiny-tunnel/types"
	"github.com/campbel/tiny-tunnel/util"
	"golang.org/x/crypto/acme/autocert"
	"golang.org/x/net/websocket"
)

var (
	AllowIPHeader = http.CanonicalHeaderKey("X-TT-Allow-IP")
)

func Serve(options ServeOptions) {
	log.Info("starting server", "options", options)
	websockerHandler := func(name string, c chan (types.Request)) http.Handler {
		responseDict := sync.NewMap[string, chan (types.Response)]()
		return websocket.Handler(func(ws *websocket.Conn) {
			done := make(chan bool)
			// Read responses
			go func() {
				defer func() {
					done <- true
					log.Info("closing reads", "name", name)
				}()
				for {
					buffer := make([]byte, 1024)
					if err := websocket.Message.Receive(ws, &buffer); err != nil {
						log.Info("error reading response", "err", err.Error(), "name", name)
						return
					}
					response := types.LoadResponse(buffer)
					if responseChan, ok := responseDict.Get(response.ID); !ok {
						log.Info("response undeliverable", "id", response.ID, "name", name)
					} else {
						responseChan <- response
						responseDict.Delete(response.ID)
					}
				}
			}()
			// Write requests
		LOOP:
			for {
				select {
				case <-done:
					break LOOP
				case msg := <-c:
					id := util.RandString(24)
					if !responseDict.SetNX(id, msg.ResponseChan) {
						break LOOP
					}
					msg.ID = id
					if err := websocket.Message.Send(ws, msg.JSON()); err != nil {
						log.Info("error writing request", "err", err, "name", name)
						break LOOP
					}
				}
			}
			log.Info("closing writes", "name", name)
		})
	}

	go func() {
		dict := sync.NewMap[string, types.Tunnel]()
		mux := http.NewServeMux()
		mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
			fmt.Fprint(w, "Welcome to Tiny Tunnel. See github.com/campbel/tiny-tunnel for more info.")
		})
		mux.HandleFunc("/register", func(w http.ResponseWriter, r *http.Request) {
			name := r.FormValue("name")
			if name == "" {
				http.Error(w, "name is required", http.StatusBadRequest)
				return
			}
			c := make(chan (types.Request))
			if !dict.SetNX(name, types.Tunnel{ID: name, C: c, AllowedIPs: r.Header[AllowIPHeader]}) {
				http.Error(w, "name is already used", http.StatusBadRequest)
				return
			}
			log.Info("registered tunnel", "name", name)
			websockerHandler(name, c).ServeHTTP(w, r)
			dict.Delete(name)
			log.Info("unregistered tunnel", "name", name)
		})

		root := func(w http.ResponseWriter, r *http.Request) {
			name, ok := util.GetSubdomain(r, options.Hostname)
			if !ok {
				mux.ServeHTTP(w, r)
				return
			}
			if tunnel, ok := dict.Get(name); ok {
				if !util.AllowedIP(r, tunnel.AllowedIPs) {
					http.Error(w, "gtfo", http.StatusForbidden)
					return
				}
				responseChan := make(chan (types.Response))
				tunnel.C <- types.Request{
					Method:       r.Method,
					Path:         r.URL.Path + "?" + r.URL.Query().Encode(),
					Headers:      r.Header,
					Body:         util.MustRead(r.Body),
					CreatedAt:    time.Now(),
					ResponseChan: responseChan,
				}
				resp := <-responseChan
				if resp.Error != "" {
					http.Error(w, "there was an error processing your request", http.StatusInternalServerError)
					return
				}
				for k, v := range resp.Headers {
					for _, v := range v {
						w.Header().Add(k, v)
					}
				}
				w.WriteHeader(resp.Status)
				w.Write(resp.Body)
				return
			}
			http.Error(w, "the specified service is unavailable", http.StatusServiceUnavailable)
		}

		server := &http.Server{
			Addr:    ":" + options.Port,
			Handler: http.HandlerFunc(root),
		}

		// automatic certificate creation for https
		if options.LetsEncrypt {
			m := &autocert.Manager{
				Cache:  autocert.DirCache("secret-dir"),
				Prompt: autocert.AcceptTOS,
				Email:  "campbel@hey.com",
			}
			server.TLSConfig = m.TLSConfig()
			util.Must(server.ListenAndServeTLS("", ""))
		} else {
			util.Must(server.ListenAndServe())
		}
	}()
	util.WaitSigInt()
}

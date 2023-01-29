package main

import (
	"fmt"
	"net/http"
	"os"
	"time"

	"golang.org/x/net/websocket"

	tthttp "github.com/campbel/tiny-tunnel/http"
	"github.com/campbel/tiny-tunnel/log"
	"github.com/campbel/tiny-tunnel/sync"
	"github.com/campbel/tiny-tunnel/types"
	"github.com/campbel/tiny-tunnel/util"
	"github.com/campbel/yoshi"
)

var (
	AllowIPHeader = http.CanonicalHeaderKey("X-TT-Allow-IP")
)

func main() {
	err := yoshi.New("tt").Run(types.YoshiApp{
		Server: server,
		Echo:   echo,
		Client: client,
	})
	if err != nil {
		fmt.Println(err)
		os.Exit(1)
	}
}

func echo(options types.EchoOptions) {
	log.Info("starting server", log.P("port", options.Port))
	util.Must(http.ListenAndServe(":"+options.Port, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write(types.Request{
			Method:  r.Method,
			Path:    r.URL.Path,
			Headers: r.Header,
			Body:    util.MustRead(r.Body),
		}.JSON())
	})))
}

func server(options types.ServerOptions) {
	log.Info("starting server", log.P("port", options.Port))
	websockerHandler := func(name string, c chan (types.Request)) http.Handler {
		responseDict := sync.NewMap[string, chan (types.Response)]()
		return websocket.Handler(func(ws *websocket.Conn) {
			done := make(chan bool)
			// Read responses
			go func() {
				defer func() {
					done <- true
					log.Info("closing reads", log.P("name", name))
				}()
				for {
					buffer := make([]byte, 1024)
					if err := websocket.Message.Receive(ws, &buffer); err != nil {
						log.Info("error reading response", log.P("err", err.Error()), log.P("name", name))
						return
					}
					response := types.LoadResponse(buffer)
					if responseChan, ok := responseDict.Get(response.ID); !ok {
						log.Info("response undeliverable", log.P("id", response.ID), log.P("name", name))
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
						log.Info("error writing request", log.P("err", err), log.P("name", name))
						break LOOP
					}
				}
			}
			log.Info("closing writes", log.P("name", name))
		})
	}

	go func() {
		dict := sync.NewMap[string, types.Tunnel]()
		mux := http.NewServeMux()
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
			log.Info("registered tunnel", log.P("name", name))
			websockerHandler(name, c).ServeHTTP(w, r)
			dict.Delete(name)
			log.Info("unregistered tunnel", log.P("name", name))
		})

		http.ListenAndServe(":"+options.Port, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
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
					Path:         r.URL.Path,
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
				w.Write(resp.Body)
				return
			}
			http.Error(w, "the specified service is unavailable", http.StatusServiceUnavailable)
		}))
	}()
	util.WaitSigInt()
}

func client(options types.ClientOptions) {
	util.Must(options.Valid())
	log.Info("starting client", log.P("options", options))
	go func() {
		attempts := 0
		for {
			time.Sleep(time.Duration(attempts) * time.Second)
			attempts++
			config, err := websocket.NewConfig(options.URL(), options.Origin())
			util.Must(err)
			config.Header[AllowIPHeader] = options.AllowedIPs
			ws, err := websocket.DialConfig(config)
			if err != nil {
				if attempts > options.ReconnectAttempts {
					log.Info("failed to connect to server, exiting", log.P("error", err.Error()))
					os.Exit(1)
				}
				log.Info("failed to connect to server", log.P("error", err.Error()))
				continue
			}
			attempts = 0
			log.Info("connected to server")
			for {
				buffer := make([]byte, 1024)
				if err := websocket.Message.Receive(ws, &buffer); err != nil {
					break
				}
				request := types.LoadRequest(buffer)
				for k, v := range options.Headers {
					request.Headers.Add(k, v)
				}
				go func() {
					response := tthttp.Do(options.Target, request)
					log.Info("finished",
						log.P("elapsed", fmt.Sprintf("%dms", time.Since(request.CreatedAt).Milliseconds())),
						log.P("request", log.Map{"method": request.Method, "path": request.Path, "headers": request.Headers}),
						log.P("response", log.Map{"status": response.Status, "error": response.Error, "headers": response.Headers}),
					)
					if err := websocket.Message.Send(ws, response.JSON()); err != nil {
						log.Info("failed to send response to server", log.P("error", err.Error()))
					}
				}()
			}
			log.Info("disconnected from server, reconnecting...")
		}
	}()
	util.WaitSigInt()
}

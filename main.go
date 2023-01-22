package main

import (
	"bytes"
	"crypto/tls"
	"fmt"
	"io"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"golang.org/x/net/websocket"

	"github.com/campbel/tiny-tunnel/log"
	"github.com/campbel/tiny-tunnel/sync"
	"github.com/campbel/tiny-tunnel/types"
	"github.com/campbel/tiny-tunnel/util"
)

func main() {

	var (
		defaultServerPort = "8000"
		defaultEchoPort   = "8001"
	)

	switch os.Args[1] {
	case "server":
		server(util.Env("TT_SERVER_PORT", defaultServerPort))
	case "echo":
		echo(util.Env("TT_ECHO_PORT", defaultEchoPort))
	case "client":
		var (
			target         string      = ""
			name           string      = util.RandString(5)
			serverHost     string      = "localhost"
			serverPort     string      = defaultServerPort
			serverInsecure bool        = false
			headers        http.Header = make(http.Header)
		)
		for i := 2; i < len(os.Args); {
			switch os.Args[i] {
			case "-n", "--name":
				name = os.Args[i+1]
				i += 2
			case "-p", "--server-port":
				serverPort = os.Args[i+1]
				i += 2
			case "-s", "--server-host":
				serverHost = os.Args[i+1]
				i += 2
			case "-k", "--insecure":
				serverInsecure = true
				i++
			case "-h", "--header":
				kv := strings.Split(os.Args[i+1], "=")
				headers.Add(kv[0], kv[1])
				i += 2
			case "--help":
				fmt.Println("Usage: tt client [options] TARGET")
				fmt.Println("Options:")
				fmt.Println("  -n, --name <name>          Name of client")
				fmt.Println("  -p, --server-port <port>   Port of server")
				fmt.Println("  -s, --server-host <host>   Host of server")
				fmt.Println("  -k, --insecure             Disable HTTPS")
				fmt.Println("  -h, --header <key=value>   Header to send")
				fmt.Println("  --help                     Show this help")
				os.Exit(0)
				return
			default:
				target = os.Args[i]
				i++
			}
		}
		client(name, target, serverHost, serverPort, serverInsecure, headers)
	default:
		fmt.Println("Usage: tt server|client|echo")
		os.Exit(1)
	}
}

func echo(port string) {
	log.Info("starting server", log.Pair{"port", port})
	util.Must(http.ListenAndServe(":"+port, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write(types.Request{
			Method:  r.Method,
			Path:    r.URL.Path,
			Headers: r.Header,
			Body:    util.MustRead(r.Body),
		}.JSON())
	})))
}

func server(port string) {
	log.Info("starting server", log.Pair{"port", port})
	websockerHandler := func(c chan (types.Request)) http.Handler {
		return websocket.Handler(func(ws *websocket.Conn) {
			for msg := range c {
				util.Must(websocket.Message.Send(ws, msg.JSON()))
				buffer := make([]byte, 1024)
				util.Must(websocket.Message.Receive(ws, &buffer))
				msg.ResponseChan <- types.LoadResponse(buffer)
			}
		})
	}

	go func() {
		dict := sync.NewMap[string, chan (types.Request)]()
		mux := http.NewServeMux()
		mux.HandleFunc("/register", func(w http.ResponseWriter, r *http.Request) {
			name := r.FormValue("name")
			if name == "" {
				http.Error(w, "name is required", http.StatusBadRequest)
				return
			}
			id := name + "." + r.Host
			c := make(chan (types.Request))
			if !dict.SetNX(id, c) {
				http.Error(w, "name is already used", http.StatusBadRequest)
				return
			}
			log.Info("registered tunnel", log.Pair{"name", name})
			websockerHandler(c).ServeHTTP(w, r)
			dict.Delete(id)
		})

		http.ListenAndServe(":"+port, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if c, ok := dict.Get(r.Host); ok {
				responseChan := make(chan (types.Response))
				c <- types.Request{
					Method:       r.Method,
					Path:         r.URL.Path,
					Headers:      r.Header,
					Body:         util.MustRead(r.Body),
					CreatedAt:    time.Now(),
					ResponseChan: responseChan,
				}
				resp := <-responseChan
				if resp.Error != "" {
					http.Error(w, resp.Error, http.StatusInternalServerError)
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
			mux.ServeHTTP(w, r)
		}))
	}()

	util.WaitSigInt()
}

func client(name, target, serverHost, serverPort string, insecure bool, headers http.Header) {
	log.Info("starting client",
		log.Pair{"name", fmt.Sprintf(`"%s"`, name)},
		log.Pair{"target", fmt.Sprintf(`"%s"`, target)},
		log.Pair{"server", fmt.Sprintf(`"%s:%s"`, serverHost, serverPort)},
		log.Pair{"insecure", strconv.FormatBool(insecure)},
	)
	schemeHttp := "https"
	schemeWs := "wss"
	if insecure {
		schemeHttp = "http"
		schemeWs = "ws"
	}
	origin := schemeHttp + "://" + serverHost
	url := schemeWs + "://" + serverHost + ":" + serverPort + "/register?name=" + name
	go func() {
		for {
			ws, err := websocket.Dial(url, "", origin)
			if err != nil {
				log.Info("failed to connect to server", log.Pair{"error", `"` + err.Error() + `"`})
				time.Sleep(time.Second)
				continue
			}
			for {
				buffer := make([]byte, 1024)
				if err := websocket.Message.Receive(ws, &buffer); err != nil {
					break
				}
				request := types.LoadRequest(buffer)
				for k, v := range headers {
					request.Headers[k] = v
				}
				response := do(target, request)
				logRequestResponse("finished", request, response)
				if err := websocket.Message.Send(ws, response.JSON()); err != nil {
					break
				}
			}
			time.Sleep(time.Second)
		}
	}()
	util.WaitSigInt()
}

var httpClient = func() http.Client {
	return http.Client{
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			return http.ErrUseLastResponse
		},
		Transport: &http.Transport{
			TLSClientConfig:     &tls.Config{InsecureSkipVerify: true},
			MaxConnsPerHost:     10,
			IdleConnTimeout:     10 * time.Second,
			TLSHandshakeTimeout: 3 * time.Second,
		},
	}
}()

func do(target string, req types.Request) types.Response {
	request, err := http.NewRequest(req.Method, target+req.Path, bytes.NewBuffer(req.Body))
	util.Must(err)
	request.Header = req.Headers
	response, err := httpClient.Do(request)
	if err != nil {
		return types.Response{Error: err.Error()}
	}
	defer response.Body.Close()
	body, err := io.ReadAll(response.Body)
	return types.Response{
		Status:  response.StatusCode,
		Headers: response.Header,
		Body:    body,
		Error:   util.ErrString(err),
	}
}

func logRequestResponse(message string, req types.Request, res types.Response) {
	log.Info(message,
		log.Pair{"elapsed", fmt.Sprintf(`"%dms"`, time.Since(req.CreatedAt).Milliseconds())},
		log.Pair{"request", fmt.Sprintf(`{"method":"%s","path":"%s","headers":%v}`, req.Method, req.Path, util.JSS(req.Headers))},
		log.Pair{"response", fmt.Sprintf(`{"status":%d,"error":"%s","headers":%v}`, res.Status, res.Error, util.JSS(res.Headers))},
	)
}

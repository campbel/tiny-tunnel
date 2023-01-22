package main

import (
	"bytes"
	"crypto/tls"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"

	"golang.org/x/net/websocket"

	"github.com/campbel/tiny-tunnel/log"
	"github.com/campbel/tiny-tunnel/sync"
	"github.com/campbel/tiny-tunnel/types"
	"github.com/campbel/tiny-tunnel/util"
)

const (
	serverPort = "8000"
)

func main() {
	switch os.Args[1] {
	case "server":
		server()
	case "echo":
		echo()
	case "client":
		var (
			serverHost string = "localhost:" + serverPort
			headers    http.Header
		)
		for i := 2; i < len(os.Args); {
			switch os.Args[i] {
			case "-s", "--server":
				serverHost = os.Args[i+1]
				i += 2
			case "-h", "--header":
				kv := strings.Split(os.Args[i+1], "=")
				headers.Add(kv[0], kv[1])
				i += 2
			case "--help":
				fmt.Println("Usage: tt client -s server [-h key=value]...")
				return
			}
		}
		client(serverHost, headers)
	default:
		fmt.Println("Usage: tt server|client|echo port")
	}
}

func echo() {
	util.Must(http.ListenAndServe(":"+os.Args[2], http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write(types.Request{
			Method:  r.Method,
			Path:    r.URL.Path,
			Headers: r.Header,
			Body:    util.MustRead(r.Body),
		}.JSON())
	})))
}

func server() {
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
			websockerHandler(c).ServeHTTP(w, r)
			dict.Delete(id)
		})

		http.ListenAndServe(":"+serverPort, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
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

func client(target string, headers http.Header) {
	origin := "http://localhost"
	url := "ws://localhost:" + serverPort + "/register?name=foobar"
	go func() {
		for {
			ws, err := websocket.Dial(url, "", origin)
			if err != nil {
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

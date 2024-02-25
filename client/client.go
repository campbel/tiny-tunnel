package client

import (
	"fmt"
	"time"

	tthttp "github.com/campbel/tiny-tunnel/http"
	"github.com/campbel/tiny-tunnel/log"
	"github.com/campbel/tiny-tunnel/types"
	"github.com/campbel/tiny-tunnel/util"
	"golang.org/x/net/websocket"
)

func ConnectAndHandle(options ConnectOptions) error {
	if err := options.Valid(); err != nil {
		return err
	}
	log.Info("starting client", "options", options)

	err := Connect(
		options.URL(), options.Origin(), options.ServerHeaders,
		func(ws *websocket.Conn, request types.Request) {
			for k, v := range options.TargetHeaders {
				request.Headers.Add(k, v)
			}
			response := tthttp.Do(options.Target, request)
			log.Info("finished",
				"elapsed", fmt.Sprintf("%dms", time.Since(request.CreatedAt).Milliseconds()),
				"req_method", request.Method, "req_path", request.Path, "req_headers", request.Headers,
				"res_status", response.Status, "res_error", response.Error, "res_headers", response.Headers,
			)
			if err := websocket.Message.Send(ws, response.JSON()); err != nil {
				log.Info("failed to send response to server", "error", err.Error())
			}
		})
	if err != nil {
		return err
	}
	util.WaitSigInt()
	return nil
}

func Connect(url, origin string, serverHeaders map[string]string, handler func(*websocket.Conn, types.Request)) error {

	// Establish a ws connection to the server
	config, err := websocket.NewConfig(url, origin)
	if err != nil {
		return err
	}
	for k, v := range serverHeaders {
		config.Header.Add(k, v)
	}
	ws, err := websocket.DialConfig(config)
	if err != nil {
		return err
	}

	log.Info("connected to server")

	// Read requests from the server
	go func() {
		for {
			buffer := make([]byte, 1024)
			if err := websocket.Message.Receive(ws, &buffer); err != nil {
				break
			}
			request := types.LoadRequest(buffer)
			go handler(ws, request)
		}
		log.Info("disconnected from server")
	}()

	return nil
}

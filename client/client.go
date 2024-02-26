package client

import (
	"context"
	"fmt"
	"time"

	tthttp "github.com/campbel/tiny-tunnel/http"
	"github.com/campbel/tiny-tunnel/log"
	"github.com/campbel/tiny-tunnel/types"
	"golang.org/x/net/websocket"
)

func ConnectAndHandle(ctx context.Context, options ConnectOptions) error {
	if err := options.Valid(); err != nil {
		return err
	}
	log.Info("starting client", "options", options)

	closed, err := Connect(
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

	select {
	case <-ctx.Done():
		log.Info("connection closed by user")
	case <-closed:
		log.Info("connection closed by remote")
	}

	return nil
}

func Connect(url, origin string, serverHeaders map[string]string, handler func(*websocket.Conn, types.Request)) (chan bool, error) {

	// Establish a ws connection to the server
	config, err := websocket.NewConfig(url, origin)
	if err != nil {
		return nil, err
	}
	for k, v := range serverHeaders {
		config.Header.Add(k, v)
	}
	ws, err := websocket.DialConfig(config)
	if err != nil {
		return nil, err
	}

	log.Info("connected to server")

	// Close channel when the function returns
	closed := make(chan bool)

	// Read requests from the server
	go func(c chan bool) {
		for {
			buffer := make([]byte, 1024)
			if err := websocket.Message.Receive(ws, &buffer); err != nil {
				break
			}
			request := types.LoadRequest(buffer)
			go handler(ws, request)
		}
		log.Info("disconnected from server")
		c <- true
	}(closed)

	return closed, nil
}

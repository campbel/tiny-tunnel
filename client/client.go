package client

import (
	"fmt"
	"os"
	"time"

	tthttp "github.com/campbel/tiny-tunnel/http"
	"github.com/campbel/tiny-tunnel/log"
	"github.com/campbel/tiny-tunnel/types"
	"github.com/campbel/tiny-tunnel/util"
	"golang.org/x/net/websocket"
)

func Connect(options ConnectOptions) error {
	if err := options.Valid(); err != nil {
		return err
	}

	log.Info("starting client", "options", options)
	go func() {
		attempts := 0
		for {
			time.Sleep(time.Duration(attempts) * time.Second)
			attempts++
			config, err := websocket.NewConfig(options.URL(), options.Origin())
			util.Must(err)
			for k, v := range options.ServerHeaders {
				config.Header.Add(k, v)
			}
			ws, err := websocket.DialConfig(config)
			if err != nil {
				if attempts > options.ReconnectAttempts {
					log.Info("failed to connect to server, exiting", "error", err.Error())
					os.Exit(1)
				}
				log.Info("failed to connect to server", "error", err.Error())
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
				for k, v := range options.TargetHeaders {
					request.Headers.Add(k, v)
				}
				go func() {
					response := tthttp.Do(options.Target, request)
					log.Info("finished",
						"elapsed", fmt.Sprintf("%dms", time.Since(request.CreatedAt).Milliseconds()),
						"req_method", request.Method, "req_path", request.Path, "req_headers", request.Headers,
						"res_status", response.Status, "res_error", response.Error, "res_headers", response.Headers,
					)
					if err := websocket.Message.Send(ws, response.JSON()); err != nil {
						log.Info("failed to send response to server", "error", err.Error())
					}
				}()
			}
			log.Info("disconnected from server, reconnecting...")
		}
	}()
	util.WaitSigInt()
	return nil
}

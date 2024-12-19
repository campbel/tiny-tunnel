package client

import (
	"context"
	"fmt"
	"net/url"
	"time"

	tthttp "github.com/campbel/tiny-tunnel/http"
	"github.com/campbel/tiny-tunnel/log"
	"github.com/campbel/tiny-tunnel/types"
	"github.com/campbel/tiny-tunnel/util"
	"golang.org/x/net/websocket"

	"github.com/campbel/tiny-tunnel/sync"
)

func ConnectAndHandle(ctx context.Context, options ConnectOptions) error {
	if err := options.Valid(); err != nil {
		return err
	}
	log.Info("starting client", "options", options)

	closed, err := Connect(ctx, options)
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

func Connect(ctx context.Context, options ConnectOptions) (chan bool, error) {
	return ConnectRaw(
		options.URL(), options.Origin(), options.ServerHeaders,
		func(request types.HTTPRequest) types.HTTPResponse {
			for k, v := range options.TargetHeaders {
				request.Headers.Add(k, v)
			}
			response := tthttp.Do(options.Target, request)
			log.Info("finished",
				"elapsed", fmt.Sprintf("%dms", time.Since(request.CreatedAt).Milliseconds()),
				"req_method", request.Method, "req_path", request.Path, "req_headers", request.Headers,
				"res_status", response.Status, "res_error", response.Error, "res_headers", response.Headers,
			)
			return response
		}, options)
}

func ConnectRaw(rawURL, origin string, serverHeaders map[string]string, handler func(types.HTTPRequest) types.HTTPResponse, options ConnectOptions) (chan bool, error) {

	// Defined a websocket connection map
	wsConnections := sync.NewMap[string, *websocket.Conn]()

	// Establish a ws connection to the server
	config, err := websocket.NewConfig(rawURL, origin)
	if err != nil {
		return nil, err
	}
	for k, v := range serverHeaders {
		config.Header.Add(k, v)
	}
	tunnelWSConn, err := websocket.DialConfig(config)
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
			if err := websocket.Message.Receive(tunnelWSConn, &buffer); err != nil {
				break
			}
			message := types.LoadMessage(buffer)
			switch message.Kind {
			case types.MessageKindWebsocketCreateRequest:
				sessionID := util.RandString(20)
				request := types.LoadWebsocketCreateRequest(message.Payload)

				// update the url
				url_, err := url.ParseRequestURI(options.Target)
				if err != nil {
					log.Info("failed to parse target url", "error", err.Error())
					return
				}
				switch url_.Scheme {
				case "http":
					url_.Scheme = "ws"
				case "https":
					url_.Scheme = "wss"
				default:
					log.Info("unsupported scheme", "scheme", url_.Scheme)
					return
				}
				url_.Path = request.Path

				appWSConn, err := websocket.Dial(url_.String(), "", origin)
				if err != nil {
					log.Info("failed to dial websocket", "error", err.Error())
					return
				}
				if !wsConnections.SetNX(sessionID, appWSConn) {
					log.Info("failed to set websocket connection")
					return
				}
				// Read messages
				go func(sessionID string) {
					for {
						var buffer []byte
						if err := websocket.Message.Receive(appWSConn, &buffer); err != nil {
							break
						}
						websocket.Message.Send(tunnelWSConn, types.NewMessage(
							types.MessageKindWebsocketMessage,
							types.WebsocketMessage{
								SessionID: sessionID,
								Data:      buffer,
							},
						).JSON())
					}
				}(sessionID)

				if err := websocket.Message.Send(tunnelWSConn, types.NewResponseMessage(
					message.ID,
					types.MessageKindWebsocketCreateResponse,
					types.WebsocketCreateResponse{
						SessionID: sessionID,
					},
				).JSON()); err != nil {
					log.Info("failed to send response to server", "error", err.Error())
				}
			case types.MessageKindWebsocketMessage:
				message := types.LoadWebsocketMessage(message.Payload)
				wsConn, ok := wsConnections.Get(message.SessionID)
				if !ok {
					log.Info("failed to get websocket connection", "session_id", message.SessionID)
					return
				}
				if err := websocket.Message.Send(wsConn, message.Data); err != nil {
					log.Info("failed to send message to websocket", "error", err.Error())
				}

			case types.MessageKindHttpRequest:
				request := types.LoadRequest(message.Payload)
				response := handler(request)
				if err := websocket.Message.Send(tunnelWSConn, types.NewResponseMessage(
					message.ID,
					types.MessageKindHttpResponse,
					response,
				).JSON()); err != nil {
					log.Info("failed to send response to server", "error", err.Error())
				}
			}
		}
		log.Info("disconnected from server")
		c <- true
	}(closed)

	return closed, nil
}

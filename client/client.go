package client

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"time"

	"github.com/campbel/tiny-tunnel/log"
	"github.com/campbel/tiny-tunnel/types"
	"github.com/campbel/tiny-tunnel/util"

	"github.com/campbel/tiny-tunnel/sync"

	"github.com/gorilla/websocket"

	tthttp "github.com/campbel/tiny-tunnel/http"
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

	// Websocket proxying between the tt-client and the tt-server are correlated by sessionID
	// This map is used to store the websocket connections for each sessionID
	// These websocket connections are multiplexed over the same connection to the tt-server
	wsConnections := sync.NewMap[string, *sync.WSConn]()

	// Establish a connection to the tt-server
	tunnelConn, err := connectToTTServer(ctx, options)
	if err != nil {
		return nil, err
	}

	log.Info("connected to server")

	// Close channel when the function returns
	closed := make(chan bool)

	// Send heartbeats to the server
	go func(c chan bool) {
		ticker := time.NewTicker(30 * time.Second)
		for {
			select {
			case <-ticker.C:
				if err := tunnelConn.WriteMessage(websocket.PingMessage, nil); err != nil {
					log.Info("failed to send ping", "error", err.Error())
					return
				}
			case <-c:
				ticker.Stop()
				return
			}
		}
	}(closed)

	// Read requests from the server
	go func(c chan bool) {
		for {
			mt, data, err := tunnelConn.ReadMessage()
			if err != nil {
				break
			}

			// Handle close messages
			if mt == websocket.CloseMessage {
				return
			}

			// At this point we only expect binary messages which enclose a message
			if mt != websocket.BinaryMessage {
				log.Info("unsupported message type received", "type", mt)
				continue
			}

			message := types.LoadMessage(data)
			switch message.Kind {
			case types.MessageKindWebsocketCreateRequest:
				sessionID := util.RandString(20)
				request := types.LoadWebsocketCreateRequest(message.Payload)
				log.Info("request for new websocket", "session_id", sessionID)

				// update the url
				url_, err := getWSURL(options.Target, request.Path)
				if err != nil {
					log.Info("failed to get websocket url", "error", err.Error())
					return
				}

				dialer := websocket.Dialer{}
				headers := http.Header{}
				headers.Add("Origin", request.Origin)

				rawAppConn, _, err := dialer.DialContext(ctx, url_.String(), headers)
				if err != nil {
					log.Info("failed to dial websocket", "error", err.Error())
					panic(err)
				}

				rawAppConn.SetPingHandler(func(appData string) error {
					return tunnelConn.WriteMessage(websocket.BinaryMessage, types.NewMessage(
						types.MessageKindWebsocketMessage,
						types.NewPingWebsocketMessage(sessionID, []byte(appData)),
					).JSON())
				})

				rawAppConn.SetPongHandler(func(appData string) error {
					return tunnelConn.WriteMessage(websocket.BinaryMessage, types.NewMessage(
						types.MessageKindWebsocketMessage,
						types.NewPongWebsocketMessage(sessionID, []byte(appData)),
					).JSON())
				})

				appCon := sync.NewWSConn(rawAppConn)

				if !wsConnections.SetNX(sessionID, appCon) {
					log.Info("failed to set websocket connection")
					return
				}
				// Read messages
				go func(sessionID string) {
					for {
						mt, data, err := appCon.ReadMessage()
						if err != nil {
							log.Info("error reading message, closing websocket", "err", err.Error(), "session_id", sessionID)
							wsConnections.Delete(sessionID)
							tunnelConn.WriteMessage(websocket.BinaryMessage, types.NewMessage(
								types.MessageKindWebsocketClose,
								types.WebsocketCloseMessage{
									SessionID: sessionID,
								},
							).JSON())
							break
						}
						var wsMsg types.WebsocketMessage
						switch mt {
						case websocket.PingMessage:
							wsMsg = types.NewPingWebsocketMessage(sessionID, data)
						case websocket.PongMessage:
							wsMsg = types.NewPongWebsocketMessage(sessionID, data)
						case websocket.BinaryMessage:
							wsMsg = types.NewBinaryWebsocketMessage(sessionID, data)
						case websocket.TextMessage:
							wsMsg = types.NewStringWebsocketMessage(sessionID, string(data))
						}

						tunnelConn.WriteMessage(websocket.BinaryMessage, types.NewMessage(
							types.MessageKindWebsocketMessage,
							wsMsg,
						).JSON())
					}
				}(sessionID)

				if err := tunnelConn.WriteMessage(websocket.BinaryMessage, types.NewResponseMessage(
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
				switch message.DataType {
				case websocket.PingMessage:
					if err := wsConn.WriteMessage(websocket.PingMessage, message.BinaryData); err != nil {
						log.Info("failed to send pong to websocket", "error", err.Error())
						continue
					}
				case websocket.PongMessage:
					if err := wsConn.WriteMessage(websocket.PongMessage, message.BinaryData); err != nil {
						log.Info("failed to send ping to websocket", "error", err.Error())
						continue
					}
				case websocket.BinaryMessage:
					if err := wsConn.WriteMessage(websocket.BinaryMessage, message.BinaryData); err != nil {
						log.Info("failed to send message to websocket", "error", err.Error())
						continue
					}
				case websocket.TextMessage:
					if err := wsConn.WriteMessage(websocket.TextMessage, []byte(message.StringData)); err != nil {
						log.Info("failed to send message to websocket", "error", err.Error())
						continue
					}
				}

			case types.MessageKindWebsocketClose:
				closeMessage := types.LoadWebsocketCloseMessage(message.Payload)
				appWSConn, ok := wsConnections.Get(closeMessage.SessionID)
				if !ok {
					log.Info("failed to get websocket connection", "session_id", closeMessage.SessionID)
					return
				}
				if err := appWSConn.Close(); err != nil {
					log.Info("failed to close websocket connection", "session_id", closeMessage.SessionID, "error", err.Error())
				}
				wsConnections.Delete(closeMessage.SessionID)

			case types.MessageKindHttpRequest:
				request := types.LoadRequest(message.Payload)

				for k, v := range options.TargetHeaders {
					request.Headers.Add(k, v)
				}
				response := tthttp.Do(options.Target, request)

				if err := tunnelConn.WriteMessage(websocket.BinaryMessage, types.NewResponseMessage(
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

func connectToTTServer(ctx context.Context, options ConnectOptions) (*sync.WSConn, error) {
	dialer := websocket.Dialer{}
	headers := http.Header{}
	headers.Add("Origin", options.Origin())
	for k, v := range options.ServerHeaders {
		headers.Add(k, v)
	}

	rawConn, _, err := dialer.DialContext(ctx, options.URL(), headers)
	if err != nil {
		return nil, err
	}
	return sync.NewWSConn(rawConn), nil
}

func getWSURL(target string, path string) (*url.URL, error) {
	url_, err := url.ParseRequestURI(target)
	if err != nil {
		return nil, err
	}
	url_.Path = path
	switch url_.Scheme {
	case "http":
		url_.Scheme = "ws"
	case "https":
		url_.Scheme = "wss"
	default:
		return nil, fmt.Errorf("unsupported scheme: %s", url_.Scheme)
	}
	url_.Path = path
	return url_, nil
}

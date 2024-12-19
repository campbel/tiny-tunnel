package client

import (
	"context"
	"net/http"
	"net/url"

	tthttp "github.com/campbel/tiny-tunnel/http"
	"github.com/campbel/tiny-tunnel/log"
	"github.com/campbel/tiny-tunnel/types"
	"github.com/campbel/tiny-tunnel/util"

	"github.com/campbel/tiny-tunnel/sync"

	"github.com/gorilla/websocket"
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
			return tthttp.Do(options.Target, request)
		}, options)
}

func ConnectRaw(rawURL, origin string, serverHeaders map[string]string, handler func(types.HTTPRequest) types.HTTPResponse, options ConnectOptions) (chan bool, error) {

	// Defined a websocket connection map
	wsConnections := sync.NewMap[string, *sync.WSConn]()

	// Establish a ws connection to the server
	dialer := websocket.Dialer{}
	headers := http.Header{}
	headers.Add("Origin", origin)
	for k, v := range options.ServerHeaders {
		headers.Add(k, v)
	}

	rawConn, _, err := dialer.Dial(rawURL, headers)
	if err != nil {
		return nil, err
	}

	conn := sync.NewWSConn(rawConn)

	log.Info("connected to server")

	// Close channel when the function returns
	closed := make(chan bool)

	// Read requests from the server
	go func(c chan bool) {
		for {
			_, data, err := conn.ReadMessage()
			if err != nil {
				break
			}

			message := types.LoadMessage(data)
			switch message.Kind {
			case types.MessageKindWebsocketCreateRequest:
				sessionID := util.RandString(20)
				request := types.LoadWebsocketCreateRequest(message.Payload)
				log.Info("request for new websocket", "session_id", sessionID)

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

				dialer := websocket.Dialer{}
				headers := http.Header{}
				headers.Add("Origin", request.Origin)

				rawAppConn, _, err := dialer.Dial(url_.String(), headers)
				if err != nil {
					log.Info("failed to dial websocket", "error", err.Error())
					panic(err)
				}
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
							log.Info("error reading message", "err", err.Error(), "session_id", sessionID)
							break
						}
						var wsMsg types.WebsocketMessage
						switch mt {
						case websocket.BinaryMessage:
							wsMsg = types.NewBinaryWebsocketMessage(sessionID, data)
						case websocket.TextMessage:
							wsMsg = types.NewStringWebsocketMessage(sessionID, string(data))
						}

						conn.WriteMessage(websocket.BinaryMessage, types.NewMessage(
							types.MessageKindWebsocketMessage,
							wsMsg,
						).JSON())
					}
				}(sessionID)

				if err := conn.WriteMessage(websocket.BinaryMessage, types.NewResponseMessage(
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
				if message.IsBinary() {
					if err := wsConn.WriteMessage(websocket.BinaryMessage, message.BinaryData); err != nil {
						log.Info("failed to send message to websocket", "error", err.Error())
						continue
					}
				} else {
					if err := wsConn.WriteMessage(websocket.TextMessage, []byte(message.StringData)); err != nil {
						log.Info("failed to send message to websocket", "error", err.Error())
						continue
					}
				}

			case types.MessageKindHttpRequest:
				request := types.LoadRequest(message.Payload)
				response := handler(request)
				if err := conn.WriteMessage(websocket.BinaryMessage, types.NewResponseMessage(
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

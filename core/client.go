package core

import (
	"bytes"
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"io"
	"net/http"
	gsync "sync"

	"github.com/campbel/tiny-tunnel/log"
	"github.com/campbel/tiny-tunnel/sync"
	"github.com/google/uuid"
	"github.com/gorilla/websocket"
)

type ClientTunnel struct {
	options ClientOptions

	tunnel     *Tunnel
	httpClient *http.Client
	wsSessions *sync.Map[string, *sync.WSConn]

	// manage the client tunnel done channel
	done   <-chan bool
	waitMu gsync.Mutex
	isDone bool
}

func NewClientTunnel(options ClientOptions) *ClientTunnel {
	return &ClientTunnel{
		options: options,
		httpClient: &http.Client{
			Transport: &http.Transport{
				TLSClientConfig: &tls.Config{InsecureSkipVerify: options.Insecure},
			},
		},
		wsSessions: sync.NewMap[string, *sync.WSConn](),
	}
}

// Wait blocks until the client tunnel is done
func (c *ClientTunnel) Wait() {
	c.waitMu.Lock()
	defer c.waitMu.Unlock()

	if c.isDone {
		return
	}

	<-c.done
	c.isDone = true
}

func (c *ClientTunnel) Connect(ctx context.Context) error {
	conn, _, err := websocket.DefaultDialer.DialContext(ctx, c.options.URL(), c.options.ServerHeaders)
	if err != nil {
		return err
	}

	c.tunnel = NewTunnel(conn)

	go func() {
		<-ctx.Done()
		c.tunnel.Close()
	}()

	c.tunnel.SetTextHandler(func(tunnel *Tunnel, id string, payload TextPayload) {
		fmt.Println("Received text:", payload.Text)
	})

	c.tunnel.SetHttpRequestHandler(func(tunnel *Tunnel, id string, payload HttpRequestPayload) {
		var body *bytes.Reader
		if payload.Body != nil {
			body = bytes.NewReader(payload.Body)
		}

		url_ := c.options.Target + payload.Path
		req, err := http.NewRequest(payload.Method, url_, body)
		if err != nil {
			log.Error("failed to create HTTP request", "error", err.Error())
			return
		}

		for k, v := range payload.Headers {
			for _, vv := range v {
				req.Header.Add(k, vv)
			}
		}

		resp, err := c.httpClient.Do(req)
		if err != nil {
			tunnel.SendResponse(MessageKindHttpResponse, id, &HttpResponsePayload{Error: err})
			return
		}
		defer resp.Body.Close()

		bodyBytes, err := io.ReadAll(resp.Body)
		if err != nil {
			tunnel.SendResponse(MessageKindHttpResponse, id, &HttpResponsePayload{Error: err})
			return
		}

		tunnel.SendResponse(MessageKindHttpResponse, id, &HttpResponsePayload{Response: HttpResponse{
			Status:  resp.StatusCode,
			Headers: resp.Header,
			Body:    bodyBytes,
		}})
	})

	c.tunnel.SetWebsocketCreateRequestHandler(func(tunnel *Tunnel, id string, payload WebsocketCreateRequestPayload) {
		log.Info("websocket create request", "payload", payload)
		wsUrl, err := getWebsocketURL(c.options.Target)
		if err != nil {
			tunnel.SendResponse(MessageKindWebsocketCreateResponse, id, &WebsocketCreateResponsePayload{Error: err})
			return
		}

		rawConn, resp, err := websocket.DefaultDialer.DialContext(ctx, wsUrl.String()+payload.Path, http.Header{"Origin": []string{payload.Origin}})
		if err != nil {
			tunnel.SendResponse(MessageKindWebsocketCreateResponse, id, &WebsocketCreateResponsePayload{Error: err})
			return
		}

		conn := sync.NewWSConn(rawConn)

		sessionID := uuid.New().String()
		if ok := c.wsSessions.SetNX(sessionID, conn); !ok {
			tunnel.SendResponse(MessageKindWebsocketCreateResponse, id, &WebsocketCreateResponsePayload{Error: errors.New("session already exists")})
			return
		}

		tunnel.SendResponse(MessageKindWebsocketCreateResponse, id, &WebsocketCreateResponsePayload{
			SessionID: sessionID,
			HttpResponse: &HttpResponsePayload{Response: HttpResponse{
				Status:  resp.StatusCode,
				Headers: resp.Header,
			}},
		})

		go func() {
			log.Info("starting websocket read loop", "session_id", sessionID)
			defer conn.Close()
			for {
				mt, data, err := conn.ReadMessage()
				if err != nil {
					log.Error("exiting websocket read loop", "error", err.Error(), "session_id", sessionID)
					break
				}
				log.Debug("read ws message", "session_id", sessionID, "kind", mt, "data", string(data))
				if err := tunnel.Send(MessageKindWebsocketMessage, &WebsocketMessagePayload{SessionID: sessionID, Kind: mt, Data: data}); err != nil {
					log.Error("failed to send websocket message", "error", err.Error())
				}
			}
		}()
	})

	c.tunnel.SetWebsocketMessageHandler(func(tunnel *Tunnel, id string, payload WebsocketMessagePayload) {
		conn, ok := c.wsSessions.Get(payload.SessionID)
		if !ok {
			log.Error("websocket session not found", "session_id", id)
			return
		}
		if err := conn.WriteMessage(payload.Kind, payload.Data); err != nil {
			log.Error("failed to write websocket message", "error", err.Error())
		}
	})

	doneChan := make(chan bool)
	go func() {
		c.tunnel.StartReadLoop(ctx)
		doneChan <- true
	}()

	c.done = doneChan

	return nil
}

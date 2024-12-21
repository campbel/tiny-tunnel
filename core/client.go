package core

import (
	"bytes"
	"context"
	"crypto/tls"
	"fmt"
	"io"
	"net/http"

	"github.com/campbel/tiny-tunnel/log"
	"github.com/gorilla/websocket"
)

type ClientTunnel struct {
	options ClientOptions

	tunnel     *Tunnel
	httpClient *http.Client
}

func NewClientTunnel(options ClientOptions) *ClientTunnel {
	return &ClientTunnel{
		options: options,
		httpClient: &http.Client{
			Transport: &http.Transport{
				TLSClientConfig: &tls.Config{InsecureSkipVerify: options.Insecure},
			},
		},
	}
}

func (c *ClientTunnel) Connect(ctx context.Context) error {
	conn, _, err := websocket.DefaultDialer.DialContext(ctx, c.options.URL(), c.options.ServerHeaders)
	if err != nil {
		return err
	}

	c.tunnel = NewTunnel(conn)

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

	c.tunnel.Run()

	return nil
}

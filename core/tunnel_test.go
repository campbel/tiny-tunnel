package core

import (
	"net/http"
	"net/http/httptest"
	"slices"
	"testing"

	"github.com/gorilla/websocket"
	"github.com/stretchr/testify/assert"
)

func TestTunnel(t *testing.T) {
	assert := assert.New(t)
	clientTunnel, serverTunnel := createConnectedTunnels(t)

	clientTunnel.SetTextHandler(func(tunnel *Tunnel, payload TextPayload) {
		assert.Equal(payload.Text, "Hello, World!")
		response := []byte(payload.Text)
		slices.Reverse(response)
		tunnel.Send(MessageKindText, &TextPayload{
			Text: string(response),
		})
	})

	responseChan := make(chan string)
	serverTunnel.SetTextHandler(func(tunnel *Tunnel, payload TextPayload) {
		responseChan <- string(payload.Text)
	})

	serverTunnel.Send(MessageKindText, &TextPayload{
		Text: "Hello, World!",
	})

	assert.Equal("!dlroW ,olleH", <-responseChan)

	serverTunnel.Close()
	clientTunnel.Close()
}

func createConnectedTunnels(t *testing.T) (*Tunnel, *Tunnel) {
	assert := assert.New(t)

	serverTunnelChan := make(chan *Tunnel)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		upgrader := websocket.Upgrader{
			CheckOrigin: func(r *http.Request) bool {
				return true
			},
		}
		conn, err := upgrader.Upgrade(w, r, nil)
		assert.NoError(err)
		defer conn.Close()
		serverTunnel := NewTunnel(conn)
		serverTunnelChan <- serverTunnel
		serverTunnel.Run()
	}))
	defer server.Close()

	// Setup the client
	serverWSURL, err := getWebsocketURL(server.URL)
	if !assert.NoError(err) {
		t.FailNow()
	}
	conn, _, err := websocket.DefaultDialer.Dial(serverWSURL.String(), nil)
	if !assert.NoError(err) {
		t.FailNow()
	}
	clientTunnel := NewTunnel(conn)
	go clientTunnel.Run()

	return clientTunnel, <-serverTunnelChan
}

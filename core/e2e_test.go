package core_test

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"slices"
	"testing"

	"github.com/campbel/tiny-tunnel/core/client"
	"github.com/campbel/tiny-tunnel/core/server"
	"github.com/campbel/tiny-tunnel/core/stats"
	"github.com/campbel/tiny-tunnel/internal/log"
	"github.com/gorilla/websocket"
	"github.com/stretchr/testify/assert"
)

func TestE2E(t *testing.T) {
	assert := assert.New(t)

	appServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Upgrade") == "websocket" {
			upgrader := websocket.Upgrader{
				CheckOrigin: func(r *http.Request) bool {
					return true
				},
			}
			conn, err := upgrader.Upgrade(w, r, nil)
			if err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}

			for {
				mt, message, err := conn.ReadMessage()
				if err != nil {
					break
				}
				slices.Reverse(message)
				if err := conn.WriteMessage(mt, message); err != nil {
					break
				}
			}
			conn.Close()
		} else {
			w.Write([]byte("Message from app server"))
		}
	}))
	defer appServer.Close()

	server := httptest.NewServer(server.NewHandler(server.Options{
		Hostname: "example.com",
	}, log.NewTestLogger()))
	defer server.Close()

	serverURL, _ := url.Parse(server.URL)

	ctx, cancel := context.WithCancel(context.Background())
	client, err := client.NewTunnel(ctx, client.Options{
		Name:       "test",
		ServerHost: serverURL.Hostname(),
		ServerPort: serverURL.Port(),
		Insecure:   true,
		Target:     appServer.URL,
	}, stats.NewTestStateProvider(), stats.NewTestStatsProvider(), log.NewTestLogger())

	assert.NoError(err)

	go client.Listen(ctx)

	t.Run("HTTP Request #1", func(t *testing.T) {
		for range 5 {
			req, _ := http.NewRequest(http.MethodGet, server.URL, nil)
			req.Host = "test.example.com"

			resp, err := http.DefaultClient.Do(req)
			assert.NoError(err)
			assert.Equal(http.StatusOK, resp.StatusCode)

			body, _ := io.ReadAll(resp.Body)
			assert.Equal("Message from app server", string(body))
		}
	})

	// WebSocket Request
	t.Run("WebSocket Request", func(t *testing.T) {
		dialer := websocket.Dialer{}
		conn, resp, err := dialer.Dial(fmt.Sprintf("ws://%s", serverURL.Host), http.Header{
			"X-TT-Tunnel": []string{"test"},
		})
		assert.NoError(err)
		defer conn.Close()
		assert.Equal(http.StatusSwitchingProtocols, resp.StatusCode)

		for i := range 5 {
			text := fmt.Sprintf("A simple ws text message %d", i)
			expected := []byte(text)
			slices.Reverse(expected)
			if !assert.NoError(conn.WriteMessage(websocket.TextMessage, []byte(text))) {
				return
			}

			mt, message, err := conn.ReadMessage()
			assert.NoError(err)
			assert.Equal(websocket.TextMessage, mt)
			assert.Equal(expected, message)
		}
	})

	cancel()
}

package core

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/gorilla/websocket"
	"github.com/stretchr/testify/assert"
)

func TestServer(t *testing.T) {
	assert := assert.New(t)

	serverTunnelChan := make(chan *ServerTunnel)
	tunnelServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		upgrader := websocket.Upgrader{
			CheckOrigin: func(r *http.Request) bool {
				return true
			},
		}

		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			t.Fatal(err)
		}

		serverTunnel := NewServerTunnel(conn)
		serverTunnelChan <- serverTunnel
		serverTunnel.Connect()
	}))
	defer tunnelServer.Close()

	appServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("Hello, World!"))
	}))
	defer appServer.Close()

	serverURL, err := url.Parse(tunnelServer.URL)
	if !assert.NoError(err) {
		return
	}

	clientTunnel := NewClientTunnel(ClientOptions{
		Name:       "test",
		ServerHost: strings.Split(serverURL.Host, ":")[0],
		ServerPort: serverURL.Port(),
		Insecure:   true,
		Target:     appServer.URL,
	})

	go clientTunnel.Connect(context.Background())

	serverTunnel := <-serverTunnelChan

	recorder := httptest.NewRecorder()
	serverTunnel.HandleHttpRequest(recorder, httptest.NewRequest("GET", "/", nil))

	response := recorder.Result()
	assert.Equal(http.StatusOK, response.StatusCode)
}

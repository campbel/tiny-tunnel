package server_test

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/campbel/tiny-tunnel/core/client"
	"github.com/campbel/tiny-tunnel/core/server"
	"github.com/campbel/tiny-tunnel/internal/util"
	"github.com/google/uuid"
	"github.com/gorilla/websocket"
	"github.com/stretchr/testify/assert"
)

func TestServerRoot(t *testing.T) {
	assert := assert.New(t)

	server := httptest.NewServer(server.NewHandler(server.Options{
		Hostname: "example.com",
	}))
	defer server.Close()

	response, err := http.Get(server.URL)
	if !assert.NoError(err) {
		return
	}

	body, err := io.ReadAll(response.Body)
	if !assert.NoError(err) {
		return
	}

	assert.Equal(http.StatusOK, response.StatusCode)
	assert.Equal("Welcome to Tiny Tunnel. See github.com/campbel/tiny-tunnel for more info.", string(body))
}

func TestServerRegister(t *testing.T) {
	assert := assert.New(t)

	server := httptest.NewServer(server.NewHandler(server.Options{
		Hostname: "example.com",
	}))
	defer server.Close()

	wsURL, err := util.GetWebsocketURL(server.URL)
	if !assert.NoError(err) {
		return
	}

	conn, _, err := websocket.DefaultDialer.Dial(wsURL.String()+"/register?name=test", nil)
	if !assert.NoError(err) {
		return
	}

	conn.Close()
}

func TestServerConnectWithClient(t *testing.T) {
	assert := assert.New(t)
	randomString := uuid.New().String()

	appServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, randomString)
	}))
	defer appServer.Close()

	server := httptest.NewServer(server.NewHandler(server.Options{
		Hostname: "example.com",
	}))
	defer server.Close()

	serverURL, err := url.Parse(server.URL)
	if !assert.NoError(err) {
		return
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	client, err := client.NewTunnel(ctx, client.Options{
		Name:       "test",
		ServerHost: serverURL.Hostname(),
		ServerPort: serverURL.Port(),
		Insecure:   true,
		Target:     appServer.URL,
	})

	if !assert.NoError(err) {
		return
	}

	go client.Listen(ctx)

	request, err := http.NewRequest("GET", server.URL, nil)
	if !assert.NoError(err) {
		return
	}
	request.Host = "test.example.com"

	response, err := http.DefaultClient.Do(request)
	if !assert.NoError(err) {
		return
	}

	body, err := io.ReadAll(response.Body)
	if !assert.NoError(err) {
		return
	}

	assert.Equal(http.StatusOK, response.StatusCode)
	assert.Equal(randomString, string(body))

}

func TestServerTunnel(t *testing.T) {
	assert := assert.New(t)

	serverCtx, cancel := context.WithCancel(context.Background())
	defer cancel()
	serverTunnelChan := make(chan *server.Tunnel)
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

		serverTunnel := server.NewTunnel(conn, server.TunnelOptions{
			HelloMessage: "hello, world!",
		})
		serverTunnelChan <- serverTunnel
		serverTunnel.Listen(serverCtx)
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

	clientCtx, cancel := context.WithCancel(context.Background())
	defer cancel()
	client, err := client.NewTunnel(clientCtx, client.Options{
		Name:       "test",
		ServerHost: strings.Split(serverURL.Host, ":")[0],
		ServerPort: serverURL.Port(),
		Insecure:   true,
		Target:     appServer.URL,
	})

	assert.NoError(err)

	go client.Listen(clientCtx)

	serverTunnel := <-serverTunnelChan

	recorder := httptest.NewRecorder()
	serverTunnel.HandleHttpRequest(recorder, httptest.NewRequest("GET", "/", nil))

	response := recorder.Result()
	assert.Equal(http.StatusOK, response.StatusCode)
}

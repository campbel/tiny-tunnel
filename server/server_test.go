package server

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	"github.com/campbel/tiny-tunnel/client"
	"github.com/gorilla/websocket"
	"github.com/stretchr/testify/assert"
)

func TestServer(t *testing.T) {
	assert := assert.New(t)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Setup the tt-server
	serverDomain := "example.com"
	ttServer := httptest.NewServer(NewHandler(serverDomain))

	// Setup the app server
	appServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("Hello, World!"))
	}))

	// Connect as a tt-client to the tt-server
	serverHost, serverPort := getServerAndPortFromURL(t, ttServer.URL)
	tunnelName := "foo"
	_, err := client.Connect(ctx, client.ConnectOptions{
		Name:          tunnelName,
		ServerHost:    serverHost,
		ServerPort:    serverPort,
		ServerHeaders: map[string]string{},
		Target:        appServer.URL,
		TargetHeaders: map[string]string{},
		Insecure:      true,
	})
	if !assert.NoError(err) {
		return
	}

	// Make a request to the tt-server
	request, err := http.NewRequest("GET", ttServer.URL, nil)
	if !assert.NoError(err) {
		return
	}
	request.Host = fmt.Sprintf("%s.%s", tunnelName, serverDomain)
	response, err := http.DefaultClient.Do(request)
	if !assert.NoError(err) {
		return
	}
	assert.Equal(http.StatusOK, response.StatusCode)
	body, err := io.ReadAll(response.Body)
	if !assert.NoError(err) {
		return
	}
	assert.Equal("Hello, World!", string(body))
}

func TestServerWebSocket(t *testing.T) {
	assert := assert.New(t)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Setup the tt-server
	serverDomain := "example.com"
	ttServer := httptest.NewServer(NewHandler(serverDomain))

	appServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		handleWebsocketConn(t, w, r)
	}))

	// Connect as a tt-client to the tt-server
	serverHost, serverPort := getServerAndPortFromURL(t, ttServer.URL)
	tunnelName := "foo"
	_, err := client.Connect(ctx, client.ConnectOptions{
		Name:          tunnelName,
		ServerHost:    serverHost,
		ServerPort:    serverPort,
		ServerHeaders: map[string]string{},
		Target:        appServer.URL,
		TargetHeaders: map[string]string{},
		Insecure:      true,
	})
	if !assert.NoError(err) {
		return
	}

	// Make a websocket request to the app-server (verify that it works without tt)
	appHost, appPort := getServerAndPortFromURL(t, appServer.URL)

	dialer := websocket.Dialer{}
	headers := http.Header{}
	headers.Add("Origin", fmt.Sprintf("http://%s.%s", tunnelName, serverDomain))
	conn, _, err := dialer.Dial(fmt.Sprintf("ws://%s:%s", appHost, appPort), headers)
	if !assert.NoError(err) {
		return
	}
	defer conn.Close()
	if err := conn.WriteMessage(websocket.TextMessage, []byte("Hello, World!")); !assert.NoError(err) {
		return
	}
	wt, buffer, err := conn.ReadMessage()
	if !assert.NoError(err) {
		return
	}
	assert.Equal(websocket.TextMessage, wt)
	assert.Equal("Hello, World!", string(buffer))

	// Make a websocket request to the tt-server
	dialer = websocket.Dialer{}
	headers = http.Header{}
	headers.Add("Origin", fmt.Sprintf("http://%s.%s", tunnelName, serverDomain))
	headers.Add("X-TT-Host", fmt.Sprintf("%s.%s", tunnelName, serverDomain))
	conn, _, err = dialer.Dial(fmt.Sprintf("ws://%s:%s", serverHost, serverPort), headers)
	if !assert.NoError(err) {
		return
	}
	defer conn.Close()

	// strings
	for i := range 5 {
		if err := conn.WriteMessage(websocket.TextMessage, []byte(fmt.Sprintf("Hello, World! %d", i))); !assert.NoError(err) {
			return
		}
		wt, buffer, err := conn.ReadMessage()
		if !assert.NoError(err) {
			return
		}
		assert.Equal(websocket.TextMessage, wt)
		assert.Equal(fmt.Sprintf("Hello, World! %d", i), string(buffer))
	}

	// binary
	var data = []byte{0xFF, 0xFE, 0xFD}
	for range 5 {
		if err := conn.WriteMessage(websocket.BinaryMessage, data); !assert.NoError(err) {
			return
		}
		wt, buffer, err := conn.ReadMessage()
		if !assert.NoError(err) {
			return
		}
		assert.Equal(websocket.BinaryMessage, wt)
		assert.Equal(data, buffer)
	}
}

func getServerAndPortFromURL(t *testing.T, rawURL string) (string, string) {
	t.Helper()
	url, err := url.Parse(rawURL)
	if err != nil {
		t.Fatal(err)
	}
	return url.Hostname(), url.Port()
}

func handleWebsocketConn(t *testing.T, w http.ResponseWriter, r *http.Request) {
	t.Helper()

	upgrader := websocket.Upgrader{
		CheckOrigin: func(r *http.Request) bool {
			return true
		},
	}

	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()

	for {
		mt, data, err := conn.ReadMessage()
		if err != nil {
			t.Fatal(err)
		}

		switch mt {
		case websocket.TextMessage:
			if err := conn.WriteMessage(websocket.TextMessage, data); err != nil {
				t.Fatal(err)
			}
		case websocket.BinaryMessage:
			if err := conn.WriteMessage(websocket.BinaryMessage, data); err != nil {
				t.Fatal(err)
			}
		}
	}
}

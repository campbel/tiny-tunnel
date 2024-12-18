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
	"github.com/stretchr/testify/assert"
	"golang.org/x/net/websocket"
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
		createEchoWebsocketHandler(t).ServeHTTP(w, r)
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
	ws, err := websocket.Dial(fmt.Sprintf("ws://%s:%s/ws/%s", appHost, appPort, tunnelName), "", fmt.Sprintf("http://%s.%s", tunnelName, serverDomain))
	if !assert.NoError(err) {
		return
	}
	defer ws.Close()
	err = websocket.Message.Send(ws, []byte("Hello, World!"))
	if !assert.NoError(err) {
		return
	}
	var message []byte
	bufferSize := 1024
	for {
		buffer := make([]byte, bufferSize)
		n, err := ws.Read(buffer)
		if !assert.NoError(err) {
			return
		}
		message = append(message, buffer[:n]...)
		if n < bufferSize {
			break
		}
	}
	assert.Equal("!dlroW ,olleH", string(message))

	// // Make a websocket request to the tt-server
	// ws, err = websocket.Dial(fmt.Sprintf("ws://%s:%s/ws/%s", serverHost, serverPort, tunnelName), "", fmt.Sprintf("http://%s.%s", tunnelName, serverDomain))
	// if !assert.NoError(err) {
	// 	return
	// }
	// defer ws.Close()
	// err = websocket.Message.Send(ws, []byte("Hello, World!"))
	// if !assert.NoError(err) {
	// 	return
	// }
	// message = make([]byte, 1024)
	// _, err = ws.Read(message)
	// if !assert.NoError(err) {
	// 	return
	// }
	// assert.Equal("!dlroW ,olleH", string(message))
}

func getServerAndPortFromURL(t *testing.T, rawURL string) (string, string) {
	t.Helper()
	url, err := url.Parse(rawURL)
	if err != nil {
		t.Fatal(err)
	}
	return url.Hostname(), url.Port()
}

func createEchoWebsocketHandler(t *testing.T) websocket.Handler {
	t.Helper()
	return websocket.Handler(func(ws *websocket.Conn) {
		defer ws.Close()
		var message []byte
		bufferSize := 1024
		for {
			buffer := make([]byte, bufferSize)
			n, err := ws.Read(buffer)
			if !assert.NoError(t, err) {
				t.FailNow()
			}
			message = append(message, buffer[:n]...)
			if n < bufferSize {
				break
			}
		}
		websocket.Message.Send(ws, message)
	})
}

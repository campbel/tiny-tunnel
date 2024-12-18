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

func getServerAndPortFromURL(t *testing.T, rawURL string) (string, string) {
	t.Helper()
	url, err := url.Parse(rawURL)
	if err != nil {
		t.Fatal(err)
	}
	return url.Hostname(), url.Port()
}

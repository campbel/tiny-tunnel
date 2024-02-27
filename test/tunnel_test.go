package test

import (
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/campbel/tiny-tunnel/client"
	"github.com/campbel/tiny-tunnel/server"
	"github.com/campbel/tiny-tunnel/types"
	"github.com/stretchr/testify/assert"
)

func TestBasicTunnelSetup(t *testing.T) {
	assert := assert.New(t)

	uniqueMessage := "missiong accomplished"

	server, err := setupClientServer(func(request types.Request) types.Response {
		return types.Response{
			ID:     request.ID,
			Status: http.StatusOK,
			Body:   []byte(uniqueMessage),
		}
	})
	assert.Nil(err)

	// Make a request to the server
	request, err := http.NewRequest("GET", server.URL+"/foo", nil)
	assert.Nil(err)
	request.Host = "foo.localhost"
	resp, err := http.DefaultClient.Do(request)
	assert.Nil(err)
	assert.Equal(http.StatusOK, resp.StatusCode)
	data, err := io.ReadAll(resp.Body)
	assert.Nil(err)
	assert.Equal(uniqueMessage, string(data))
}

func setupClientServer(handler func(types.Request) types.Response) (*httptest.Server, error) {
	tunnelServerHandler := server.NewHandler("localhost")
	server := httptest.NewServer(tunnelServerHandler)
	connectURL := "ws://" + server.Listener.Addr().String() + "/register?name=foo"
	_, err := client.Connect(connectURL, "http://localhost", nil, handler)
	return server, err
}

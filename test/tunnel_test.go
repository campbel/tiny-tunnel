package test

import (
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/campbel/tiny-tunnel/client"
	"github.com/campbel/tiny-tunnel/log"
	"github.com/campbel/tiny-tunnel/server"
	"github.com/campbel/tiny-tunnel/types"
	"github.com/stretchr/testify/assert"
	"golang.org/x/net/websocket"
)

func TestBasicTunnelSetup(t *testing.T) {
	assert := assert.New(t)

	uniqueMessage := "missiong accomplished"

	tunnelServerHandler := server.NewHandler("localhost")
	server := httptest.NewServer(tunnelServerHandler)
	defer server.Close()

	resp, err := http.Get(server.URL)
	assert.Nil(err)
	assert.Equal(http.StatusOK, resp.StatusCode)

	connectURL := "ws://" + server.Listener.Addr().String() + "/register?name=foo"
	_, err = client.Connect(connectURL, "http://localhost", nil, func(ws *websocket.Conn, request types.Request) {
		response := types.Response{
			ID:     request.ID,
			Status: 200,
			Body:   []byte(uniqueMessage),
		}
		if err := websocket.Message.Send(ws, response.JSON()); err != nil {
			log.Info("failed to send response to server", "error", err.Error())
		}
	})
	assert.Nil(err)

	// Make a request to the server
	request, err := http.NewRequest("GET", server.URL+"/foo", nil)
	assert.Nil(err)
	request.Host = "foo.localhost"
	resp, err = http.DefaultClient.Do(request)
	assert.Nil(err)
	assert.Equal(http.StatusOK, resp.StatusCode)
	data, err := io.ReadAll(resp.Body)
	assert.Nil(err)
	assert.Equal(uniqueMessage, string(data))
}

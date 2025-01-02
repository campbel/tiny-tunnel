package client_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	"github.com/campbel/tiny-tunnel/core/client"
	"github.com/campbel/tiny-tunnel/core/protocol"
	"github.com/google/uuid"
	"github.com/gorilla/websocket"
	"github.com/stretchr/testify/assert"
)

func TestClient(t *testing.T) {
	assert := assert.New(t)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Mock tunnel Server
	done := make(chan struct{})
	responseChan := make(chan protocol.Message)
	defer close(done)
	var conn *websocket.Conn
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		upgrader := websocket.Upgrader{}

		var err error
		conn, err = upgrader.Upgrade(w, r, nil)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		for {
			select {
			case <-done:
				return
			default:
				var msg protocol.Message
				err := conn.ReadJSON(&msg)
				if err != nil {
					t.Fatal(err)
				}
				responseChan <- msg
			}
		}
	}))
	defer server.Close()

	// App Server
	requestChan := make(chan *http.Request)
	appServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestChan <- r
	}))
	defer appServer.Close()

	// Create client
	url, err := url.Parse(server.URL)
	if err != nil {
		t.Fatal(err)
	}
	client, err := client.NewTunnel(ctx, client.Options{
		ServerHost: url.Hostname(),
		ServerPort: url.Port(),
		Insecure:   true,
		Target:     appServer.URL,
	})
	if err != nil {
		t.Fatal(err)
	}
	go client.Listen(ctx)

	// Execute test steps
	conn.WriteJSON(protocol.Message{
		ID:   uuid.New().String(),
		Kind: protocol.MessageKindHttpRequest,
		Payload: JSON(protocol.HttpRequestPayload{
			Method: "GET",
			Path:   "/foobar?foo=bar",
			Headers: map[string][]string{
				"X-Foo": {"bar"},
			},
		}),
	})

	request := <-requestChan
	assert.Equal(request.Method, "GET")
	assert.Equal(request.URL.Path, "/foobar")
	assert.Equal(request.URL.Query().Get("foo"), "bar")
	assert.Equal(request.Header.Get("X-Foo"), "bar")

	response := <-responseChan
	assert.Equal(response.Kind, protocol.MessageKindHttpResponse)
	var resp protocol.HttpResponsePayload
	err = json.Unmarshal(response.Payload, &resp)
	if err != nil {
		t.Fatal(err)
	}
	assert.Equal(resp.Response.Status, 200)
}

func JSON(v any) []byte {
	b, err := json.Marshal(v)
	if err != nil {
		panic(err)
	}
	return b
}

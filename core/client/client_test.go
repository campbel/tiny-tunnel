package client_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"

	"github.com/campbel/tiny-tunnel/core/client"
	"github.com/campbel/tiny-tunnel/core/protocol"
	"github.com/campbel/tiny-tunnel/core/shared"
	"github.com/google/uuid"
	"github.com/gorilla/websocket"
	"github.com/stretchr/testify/assert"
)

func TestClientHttpRequest(t *testing.T) {
	assert := assert.New(t)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	requestChan := make(chan *http.Request)
	_, conn, responseChan := setupTestScenario(t, ctx, func(w http.ResponseWriter, r *http.Request) {
		requestChan <- r
	})

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
	err := json.Unmarshal(response.Payload, &resp)
	assert.NoError(err)
	assert.Equal(resp.Response.Status, 200)
}

type Event struct {
	ID   string
	Type string
	Data string
}

func writeEvent(w http.ResponseWriter, event Event) {
	// Write event fields according to SSE specification
	if event.ID != "" {
		fmt.Fprintf(w, "id: %s\n", event.ID)
	}
	if event.Type != "" {
		fmt.Fprintf(w, "event: %s\n", event.Type)
	}
	fmt.Fprintf(w, "data: %s\n\n", event.Data)

	// Flush the response writer
	if f, ok := w.(http.Flusher); ok {
		f.Flush()
	}
}

func TestClientServerSentEvents(t *testing.T) {
	assert := assert.New(t)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	_, conn, responseChan := setupTestScenario(t, ctx, func(w http.ResponseWriter, r *http.Request) {
		// prepare the header
		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")
		w.Header().Set("Connection", "keep-alive")
		w.Header().Set("Access-Control-Allow-Origin", "*")

		for i := 0; i < 3; i++ {
			select {
			case <-r.Context().Done():
				return
			default:
				writeEvent(w, Event{
					ID:   fmt.Sprintf("%d", i),
					Type: "message",
					Data: fmt.Sprintf("foo %d", i),
				})
			}
		}
	})

	conn.WriteJSON(protocol.Message{
		ID:   uuid.New().String(),
		Kind: protocol.MessageKindSSERequest,
		Payload: JSON(protocol.SSERequestPayload{
			Path: "/",
		}),
	})

	messages := []string{}
LOOP:
	for {
		select {
		case msg := <-responseChan:
			if msg.Kind == protocol.MessageKindSSEClose {
				break LOOP
			}
			assert.Equal(msg.Kind, protocol.MessageKindSSEMessage)
			var payload protocol.SSEMessagePayload
			err := json.Unmarshal(msg.Payload, &payload)
			assert.NoError(err)
			messages = append(messages, payload.Data)
		case <-time.After(1 * time.Second):
			t.Fatal("timeout")
		}
	}

	assert.Equal([]string{"id: 0", "event: message", "data: foo 0", "", "id: 1", "event: message", "data: foo 1", "", "id: 2", "event: message", "data: foo 2", ""}, messages)
}

func setupTestScenario(t *testing.T, ctx context.Context, handler func(w http.ResponseWriter, r *http.Request)) (*shared.Tunnel, *websocket.Conn, chan protocol.Message) {
	t.Helper()

	// Mock tunnel Server
	responseChan := make(chan protocol.Message)
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
			case <-ctx.Done():
				return
			default:
				var msg protocol.Message
				err := conn.ReadJSON(&msg)
				if err != nil {
					break
				}
				responseChan <- msg
			}
		}
	}))
	go func() {
		<-ctx.Done()
		server.Close()
	}()

	// App Server
	appServer := httptest.NewServer(http.HandlerFunc(handler))
	go func() {
		<-ctx.Done()
		appServer.Close()
	}()

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

	return client, conn, responseChan
}

func JSON(v any) []byte {
	b, err := json.Marshal(v)
	if err != nil {
		panic(err)
	}
	return b
}

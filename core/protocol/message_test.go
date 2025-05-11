package protocol

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestMessageSerialization(t *testing.T) {
	tests := []Message{
		{
			ID:      "123",
			Kind:    MessageKindText,
			Payload: []byte(`{"text":"hello"}`),
		},
		{
			ID:   "456",
			Kind: MessageKindHttpRequest,
			RE:   "789",
			Payload: mustMarshal(HttpRequestPayload{
				Method: "GET",
				Path:   "/test",
			}),
		},
	}

	for _, msg := range tests {
		t.Run(msg.ID, func(t *testing.T) {
			data, err := json.Marshal(msg)
			assert.NoError(t, err)

			var decoded Message
			err = json.Unmarshal(data, &decoded)
			assert.NoError(t, err)
			assert.Equal(t, msg, decoded)
		})
	}
}

func TestHttpPayloads(t *testing.T) {
	tests := []struct {
		name    string
		payload HttpRequestPayload
	}{
		{
			name: "simple GET request",
			payload: HttpRequestPayload{
				Method:  "GET",
				Path:    "/api/v1/test",
				Headers: http.Header{"Accept": []string{"application/json"}},
				Body:    nil,
			},
		},
		{
			name: "POST request with body",
			payload: HttpRequestPayload{
				Method:  "POST",
				Path:    "/api/v1/users",
				Headers: http.Header{"Content-Type": []string{"application/json"}},
				Body:    []byte(`{"name":"test user"}`),
			},
		},
		{
			name: "request with multiple headers",
			payload: HttpRequestPayload{
				Method: "PUT",
				Path:   "/api/v1/update",
				Headers: http.Header{
					"Content-Type":  []string{"application/json"},
					"Authorization": []string{"Bearer token"},
					"X-Request-ID": []string{"123"},
				},
				Body: []byte(`{"status":"updated"}`),
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			data, err := json.Marshal(tt.payload)
			assert.NoError(t, err)

			var decoded HttpRequestPayload
			err = json.Unmarshal(data, &decoded)
			assert.NoError(t, err)
			
			assert.Equal(t, tt.payload.Method, decoded.Method)
			assert.Equal(t, tt.payload.Path, decoded.Path)
			assert.Equal(t, tt.payload.Headers, decoded.Headers)
			assert.Equal(t, tt.payload.Body, decoded.Body)
		})
	}
}

func TestSSEPayloads(t *testing.T) {
	tests := []struct {
		name    string
		payload interface{}
	}{
		{
			name: "sse request",
			payload: SSERequestPayload{
				Path:   "/events",
				Headers: http.Header{"Accept": []string{"text/event-stream"}},
			},
		},
		{
			name: "sse message without sequence",
			payload: SSEMessagePayload{
				Data: "event: update\ndata: {\"count\":1}",
			},
		},
		{
			name: "sse message with sequence",
			payload: SSEMessagePayload{
				Data:     "event: update\ndata: {\"count\":2}",
				Sequence: 5,
			},
		},
		{
			name: "sse close",
			payload: SSEClosePayload{
				Error: "connection closed",
				Timestamp: "2023-05-11T10:30:00Z",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			data, err := json.Marshal(tt.payload)
			assert.NoError(t, err)

			switch p := tt.payload.(type) {
			case SSERequestPayload:
				var decoded SSERequestPayload
				err = json.Unmarshal(data, &decoded)
				assert.NoError(t, err)
				assert.Equal(t, p, decoded)
			case SSEMessagePayload:
				var decoded SSEMessagePayload
				err = json.Unmarshal(data, &decoded)
				assert.NoError(t, err)
				assert.Equal(t, p, decoded)
			case SSEClosePayload:
				var decoded SSEClosePayload
				err = json.Unmarshal(data, &decoded)
				assert.NoError(t, err)
				assert.Equal(t, p, decoded)
			}
		})
	}
}

func TestWebsocketPayloads(t *testing.T) {
	tests := []struct {
		name    string
		payload interface{}
	}{
		{
			name: "websocket create request",
			payload: WebsocketCreateRequestPayload{
				Path:   "/ws",
				Origin: "http://localhost:8080",
			},
		},
		{
			name: "websocket message",
			payload: WebsocketMessagePayload{
				SessionID: "session123",
				Kind:      1, // binary
				Data:     []byte("hello websocket"),
			},
		},
		{
			name: "websocket close",
			payload: WebsocketClosePayload{
				SessionID: "session123",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			data, err := json.Marshal(tt.payload)
			assert.NoError(t, err)

			switch p := tt.payload.(type) {
			case WebsocketCreateRequestPayload:
				var decoded WebsocketCreateRequestPayload
				err = json.Unmarshal(data, &decoded)
				assert.NoError(t, err)
				assert.Equal(t, p, decoded)
			case WebsocketMessagePayload:
				var decoded WebsocketMessagePayload
				err = json.Unmarshal(data, &decoded)
				assert.NoError(t, err)
				assert.Equal(t, p, decoded)
			case WebsocketClosePayload:
				var decoded WebsocketClosePayload
				err = json.Unmarshal(data, &decoded)
				assert.NoError(t, err)
				assert.Equal(t, p, decoded)
			}
		})
	}
}

// Helper function to marshal payload or panic
func mustMarshal(v interface{}) []byte {
	data, err := json.Marshal(v)
	if err != nil {
		panic(err)
	}
	return data
}

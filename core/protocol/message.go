package protocol

import (
	"net/http"
	"time"
)

const (
	MessageKindText = iota
	MessageKindHttpRequest
	MessageKindHttpResponse
	MessageKindWebsocketCreateRequest
	MessageKindWebsocketCreateResponse
	MessageKindWebsocketMessage
	MessageKindWebsocketClose
	MessageKindSSERequest
	MessageKindSSEMessage
	MessageKindSSEClose
)

type Message struct {
	ID   string `json:"id"`
	Kind int    `json:"kind"`
	// RE is the request ID of the message that this message is responding to
	RE      string `json:"re"`
	Payload []byte `json:"payload"`
}

type TextPayload struct {
	Text string `json:"text"`
}

type HttpRequestPayload struct {
	Method  string      `json:"method"`
	Path    string      `json:"path"`
	Headers http.Header `json:"headers"`
	Body    []byte      `json:"body"`
}

type HttpResponsePayload struct {
	Error    error        `json:"error"`
	Response HttpResponse `json:"response"`
}

type HttpResponse struct {
	Status  int         `json:"status"`
	Headers http.Header `json:"headers"`
	Body    []byte      `json:"body"`
}

type WebsocketCreateRequestPayload struct {
	Origin string `json:"origin"`
	Path   string `json:"path"`
}

type WebsocketCreateResponsePayload struct {
	SessionID    string               `json:"session_id"`
	Error        error                `json:"error"`
	HttpResponse *HttpResponsePayload `json:"http_response"`
}

type WebsocketMessagePayload struct {
	SessionID string `json:"session_id"`
	Kind      int    `json:"kind"`
	Data      []byte `json:"data"`
}

type WebsocketClosePayload struct {
	SessionID string `json:"session_id"`
}

type HTTPRequest struct {
	Method    string      `json:"method,omitempty"`
	Path      string      `json:"path,omitempty"`
	Headers   http.Header `json:"headers,omitempty"`
	Body      []byte      `json:"body,omitempty"`
	CreatedAt time.Time   `json:"created_at,omitempty"`
}

type HTTPResponse struct {
	Status  int         `json:"status,omitempty"`
	Headers http.Header `json:"headers,omitempty"`
	Body    []byte      `json:"body,omitempty"`
	Error   string      `json:"error,omitempty"`
}

type WebsocketCreateRequest struct {
	Path    string      `json:"path,omitempty"`
	Headers http.Header `json:"headers,omitempty"`
	Origin  string      `json:"origin,omitempty"`
}

type WebsocketCreateResponse struct {
	SessionID string `json:"session_id,omitempty"`
}

type WebsocketMessage struct {
	SessionID string `json:"session_id,omitempty"`

	DataType   byte   `json:"is_binary,omitempty"`
	BinaryData []byte `json:"payload,omitempty"`
	StringData string `json:"string_payload,omitempty"`
}

type WebsocketCloseMessage struct {
	SessionID string `json:"session_id,omitempty"`
}

type SSERequestPayload struct {
	Path    string      `json:"path,omitempty"`
	Headers http.Header `json:"headers,omitempty"`
}

type SSEMessagePayload struct {
	Data     string `json:"data,omitempty"`
	Sequence int    `json:"sequence,omitempty"` // Sequence number to ensure correct ordering
}

type SSEClosePayload struct {
	Error string `json:"error,omitempty"`
}

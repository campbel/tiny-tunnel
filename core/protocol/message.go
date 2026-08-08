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
	// Streaming HTTP responses. A response to an HttpRequest may either be a
	// single buffered HttpResponse, or a stream: one HttpResponseStart,
	// followed by zero or more HttpResponseChunk, terminated by HttpResponseEnd.
	// All stream messages carry the originating request ID in RE.
	MessageKindHttpResponseStart
	MessageKindHttpResponseChunk
	MessageKindHttpResponseEnd
	// HttpStreamCancel is sent by the server to the client when the downstream
	// consumer disconnects, so the client can cancel the upstream request.
	MessageKindHttpStreamCancel
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

// HttpResponseStartPayload begins a streamed HTTP response.
type HttpResponseStartPayload struct {
	Status  int         `json:"status"`
	Headers http.Header `json:"headers,omitempty"`
}

// HttpResponseChunkPayload carries raw response body bytes, relayed verbatim
// and in order.
type HttpResponseChunkPayload struct {
	Data []byte `json:"data"`
}

// HttpResponseEndPayload terminates a streamed HTTP response.
type HttpResponseEndPayload struct {
	Error string `json:"error,omitempty"`
}

// HttpStreamCancelPayload asks the client to cancel an in-flight streamed
// response identified by the originating request ID.
type HttpStreamCancelPayload struct {
	RequestID string `json:"request_id"`
}

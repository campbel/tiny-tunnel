package core

import (
	"encoding/json"
	"net/http"
)

const (
	MessageKindText = iota
	MessageKindHttpRequest
	MessageKindHttpResponse
	MessageKindWebsocketCreateRequest
	MessageKindWebsocketCreateResponse
	MessageKindWebsocketMessage
	MessageKindWebsocketClose
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

func (p *TextPayload) Bytes() []byte {
	json, _ := json.Marshal(p)
	return json
}

type Payload interface {
	Bytes() []byte
}

type HttpRequestPayload struct {
	Method  string      `json:"method"`
	Path    string      `json:"path"`
	Headers http.Header `json:"headers"`
	Body    []byte      `json:"body"`
}

func (p *HttpRequestPayload) Bytes() []byte {
	json, _ := json.Marshal(p)
	return json
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

func (p *HttpResponsePayload) Bytes() []byte {
	json, _ := json.Marshal(p)
	return json
}

type WebsocketCreateRequestPayload struct {
	Origin string `json:"origin"`
	Path   string `json:"path"`
}

func (p *WebsocketCreateRequestPayload) Bytes() []byte {
	json, _ := json.Marshal(p)
	return json
}

type WebsocketCreateResponsePayload struct {
	SessionID    string               `json:"session_id"`
	Error        error                `json:"error"`
	HttpResponse *HttpResponsePayload `json:"http_response"`
}

func (p *WebsocketCreateResponsePayload) Bytes() []byte {
	json, _ := json.Marshal(p)
	return json
}

type WebsocketMessagePayload struct {
	SessionID string `json:"session_id"`
	Kind      int    `json:"kind"`
	Data      []byte `json:"data"`
}

func (p *WebsocketMessagePayload) Bytes() []byte {
	json, _ := json.Marshal(p)
	return json
}

type WebsocketClosePayload struct {
	SessionID string `json:"session_id"`
}

func (p *WebsocketClosePayload) Bytes() []byte {
	json, _ := json.Marshal(p)
	return json
}

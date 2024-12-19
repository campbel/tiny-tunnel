package types

import (
	"encoding/json"
	"time"

	"github.com/campbel/tiny-tunnel/util"
)

const (
	MessageKindHttpRequest             = "http_request"
	MessageKindHttpResponse            = "http_response"
	MessageKindWebsocketCreateRequest  = "ws_create_request"
	MessageKindWebsocketCreateResponse = "ws_create_response"
	MessageKindWebsocketMessage        = "ws_message"
)

type Payload interface {
	JSON() []byte
}

type Message struct {
	ID      string `json:"id,omitempty"`
	Kind    string `json:"kind,omitempty"`
	Payload []byte `json:"payload,omitempty"`

	// CreatedAt is the time the message was created
	CreatedAt time.Time `json:"created_at,omitempty"`

	// ResponseTo defines the ID of the message that this message is a response to
	ResponseTo string `json:"response_to,omitempty"`
}

func NewMessage(kind string, payload Payload) Message {
	return Message{
		ID:        util.RandString(24),
		Kind:      kind,
		Payload:   payload.JSON(),
		CreatedAt: time.Now(),
	}
}

func NewResponseMessage(responseTo string, kind string, payload Payload) Message {
	return Message{
		ID:         util.RandString(24),
		Kind:       kind,
		Payload:    payload.JSON(),
		ResponseTo: responseTo,
		CreatedAt:  time.Now(),
	}
}

func (m Message) JSON() []byte {
	return util.JS(m)
}

func (m Message) HTTPResponsePayload() HTTPResponse {
	if m.Kind != MessageKindHttpResponse {
		panic("not a response message")
	}
	return LoadResponse(m.Payload)
}

func LoadMessage(data []byte) Message {
	var m Message
	util.Must(json.Unmarshal(data, &m))
	return m
}

package types

import (
	"encoding/json"

	"github.com/campbel/tiny-tunnel/util"
)

const (
	MessageKindRequest                 = "http_request"
	MessageKindResponse                = "http_response"
	MessageKindWebsocketCreateRequest  = "ws_create_request"
	MessageKindWebsocketCreateResponse = "ws_create_response"
	MessageKindWebsocketMessage        = "ws_message"
)

type Message struct {
	ID           string         `json:"id,omitempty"`
	Kind         string         `json:"kind,omitempty"`
	Payload      []byte         `json:"payload,omitempty"`
	ResponseChan chan (Message) `json:"-"`
}

func (m Message) JSON() []byte {
	return util.JS(m)
}

func (m Message) HTTPResponsePayload() HTTPResponse {
	if m.Kind != MessageKindResponse {
		panic("not a response message")
	}
	return LoadResponse(m.Payload)
}

func LoadMessage(data []byte) Message {
	var m Message
	util.Must(json.Unmarshal(data, &m))
	return m
}

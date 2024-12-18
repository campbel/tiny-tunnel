package types

import (
	"encoding/json"

	"github.com/campbel/tiny-tunnel/util"
)

const (
	MessageKindRequest   = "http_request"
	MessageKindResponse  = "http_response"
	MessageKindWebsocket = "ws"
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

func LoadMessage(data []byte) Message {
	var m Message
	util.Must(json.Unmarshal(data, &m))
	return m
}

type WebsocketMessage struct {
	Data []byte `json:"data,omitempty"`
}

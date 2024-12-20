package types

import (
	"encoding/json"
	"net/http"

	"github.com/campbel/tiny-tunnel/util"
	"github.com/gorilla/websocket"
)

type WebsocketCreateRequest struct {
	Path    string      `json:"path,omitempty"`
	Headers http.Header `json:"headers,omitempty"`
	Origin  string      `json:"origin,omitempty"`
}

func (r WebsocketCreateRequest) JSON() []byte {
	return util.JS(r)
}

func LoadWebsocketCreateRequest(data []byte) WebsocketCreateRequest {
	var req WebsocketCreateRequest
	util.Must(json.Unmarshal(data, &req))
	return req
}

type WebsocketCreateResponse struct {
	SessionID string `json:"session_id,omitempty"`
}

func (r WebsocketCreateResponse) JSON() []byte {
	return util.JS(r)
}

func LoadWebsocketCreateResponse(data []byte) WebsocketCreateResponse {
	var resp WebsocketCreateResponse
	util.Must(json.Unmarshal(data, &resp))
	return resp
}

type WebsocketMessage struct {
	SessionID string `json:"session_id,omitempty"`

	DataType   byte   `json:"is_binary,omitempty"`
	BinaryData []byte `json:"payload,omitempty"`
	StringData string `json:"string_payload,omitempty"`
}

func NewBinaryWebsocketMessage(sessionID string, data []byte) WebsocketMessage {
	return WebsocketMessage{
		SessionID:  sessionID,
		DataType:   websocket.BinaryMessage,
		BinaryData: data,
	}
}

func NewStringWebsocketMessage(sessionID, data string) WebsocketMessage {
	return WebsocketMessage{
		SessionID:  sessionID,
		DataType:   websocket.TextMessage,
		StringData: data,
	}
}

func NewPingWebsocketMessage(sessionID string, data []byte) WebsocketMessage {
	return WebsocketMessage{
		SessionID:  sessionID,
		DataType:   websocket.PingMessage,
		BinaryData: data,
	}
}

func NewPongWebsocketMessage(sessionID string, data []byte) WebsocketMessage {
	return WebsocketMessage{
		SessionID:  sessionID,
		DataType:   websocket.PongMessage,
		BinaryData: data,
	}
}
func (m WebsocketMessage) JSON() []byte {
	return util.JS(m)
}

func LoadWebsocketMessage(data []byte) WebsocketMessage {
	var msg WebsocketMessage
	util.Must(json.Unmarshal(data, &msg))
	return msg
}

type WebsocketCloseMessage struct {
	SessionID string `json:"session_id,omitempty"`
}

func (m WebsocketCloseMessage) JSON() []byte {
	return util.JS(m)
}

func LoadWebsocketCloseMessage(data []byte) WebsocketCloseMessage {
	var msg WebsocketCloseMessage
	util.Must(json.Unmarshal(data, &msg))
	return msg
}

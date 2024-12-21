package core

import (
	"encoding/json"
	gsync "sync"

	"github.com/campbel/tiny-tunnel/log"
	"github.com/campbel/tiny-tunnel/sync"
	"github.com/google/uuid"
	"github.com/gorilla/websocket"
)

type Tunnel struct {
	conn *sync.WSConn

	// responseChannels is a map of message IDs to channels that want to receive the response
	responseChannels map[string][]chan Message

	// Handlers
	textHandler                   func(tunnel *Tunnel, id string, payload TextPayload)
	httpRequestHandler            func(tunnel *Tunnel, id string, payload HttpRequestPayload)
	websocketCreateRequestHandler func(tunnel *Tunnel, id string, payload WebsocketCreateRequestPayload)
	websocketMessageHandler       func(tunnel *Tunnel, id string, payload WebsocketMessagePayload)
}

func NewTunnel(conn *websocket.Conn) *Tunnel {
	return &Tunnel{conn: sync.NewWSConn(conn), responseChannels: make(map[string][]chan Message)}
}

func (t *Tunnel) Close() error {
	return t.conn.Close()
}

func (t *Tunnel) Send(kind int, message Payload, reChan ...chan Message) error {
	msg := Message{
		ID:      uuid.New().String(),
		Kind:    kind,
		Payload: message.Bytes(),
	}
	if len(reChan) > 0 {
		t.responseChannels[msg.ID] = reChan
	}
	return t.conn.WriteJSON(msg)
}

func (t *Tunnel) SendResponse(kind int, id string, message Payload) error {
	msg := Message{
		ID:      uuid.New().String(),
		RE:      id,
		Kind:    kind,
		Payload: message.Bytes(),
	}
	return t.conn.WriteJSON(msg)
}

func (t *Tunnel) Run() {
	defer t.Close()
	for {
		var msg Message
		err := t.conn.ReadJSON(&msg)
		if err != nil {
			log.Error("failed to read message", "error", err.Error())
			return
		}

		// If a message contains a RE, it is a response to a previous message
		// We need to send it to the channel(s) waiting for the response
		if msg.RE != "" {
			if reChans, ok := t.responseChannels[msg.RE]; ok {
				var wg gsync.WaitGroup
				for _, reChan := range reChans {
					wg.Add(1)
					go func(reChan chan Message) {
						defer wg.Done()
						reChan <- msg
					}(reChan)
				}
				wg.Wait()
				delete(t.responseChannels, msg.RE)
			}
			continue
		}

		switch msg.Kind {
		case MessageKindText:
			var payload TextPayload
			if err := json.Unmarshal(msg.Payload, &payload); err != nil {
				log.Error("failed to unmarshal text payload", "error", err.Error())
				continue
			}
			t.textHandler(t, msg.ID, payload)
		case MessageKindHttpRequest:
			var payload HttpRequestPayload
			if err := json.Unmarshal(msg.Payload, &payload); err != nil {
				log.Error("failed to unmarshal HTTP request payload", "error", err.Error())
				continue
			}
			t.httpRequestHandler(t, msg.ID, payload)
		case MessageKindWebsocketCreateRequest:
			var payload WebsocketCreateRequestPayload
			if err := json.Unmarshal(msg.Payload, &payload); err != nil {
				log.Error("failed to unmarshal websocket create request payload", "error", err.Error())
				continue
			}
			t.websocketCreateRequestHandler(t, msg.ID, payload)
		case MessageKindWebsocketMessage:
			var payload WebsocketMessagePayload
			if err := json.Unmarshal(msg.Payload, &payload); err != nil {
				log.Error("failed to unmarshal websocket message payload", "error", err.Error())
				continue
			}
			t.websocketMessageHandler(t, msg.ID, payload)
		}
	}
}

func (t *Tunnel) SetTextHandler(handler func(tunnel *Tunnel, id string, payload TextPayload)) {
	t.textHandler = handler
}

func (t *Tunnel) SetHttpRequestHandler(handler func(tunnel *Tunnel, id string, payload HttpRequestPayload)) {
	t.httpRequestHandler = handler
}

func (t *Tunnel) SetWebsocketCreateRequestHandler(handler func(tunnel *Tunnel, id string, payload WebsocketCreateRequestPayload)) {
	t.websocketCreateRequestHandler = handler
}

func (t *Tunnel) SetWebsocketMessageHandler(handler func(tunnel *Tunnel, id string, payload WebsocketMessagePayload)) {
	t.websocketMessageHandler = handler
}

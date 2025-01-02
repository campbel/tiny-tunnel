package shared

import (
	"context"
	"encoding/json"
	"sync"
	"time"

	"github.com/campbel/tiny-tunnel/core/protocol"
	"github.com/campbel/tiny-tunnel/internal/log"
	"github.com/campbel/tiny-tunnel/internal/safe"
	"github.com/google/uuid"
	"github.com/gorilla/websocket"
)

type Tunnel struct {
	conn *safe.WSConn

	// closure
	isClosed     bool
	closeHandler func()
	closeChan    chan struct{}
	closeMu      sync.Mutex

	// responseChannels is a map of message IDs to channels that want to receive the response
	responseChannels *safe.Map[string, []chan protocol.Message]

	// Handlers
	handlerRegistry map[int]func(tunnel *Tunnel, id string, payload []byte)
}

func NewTunnel(conn *websocket.Conn) *Tunnel {
	return &Tunnel{
		conn:             safe.NewWSConn(conn),
		responseChannels: safe.NewMap[string, []chan protocol.Message](),
		closeChan:        make(chan struct{}),
		handlerRegistry:  make(map[int]func(tunnel *Tunnel, id string, payload []byte)),
	}
}

func (t *Tunnel) Close() {
	t.close(false)
}

func (t *Tunnel) SetCloseHandler(handler func()) {
	t.closeHandler = handler
}

func (t *Tunnel) close(peerSent bool) {
	t.closeMu.Lock()
	defer t.closeMu.Unlock()

	if t.isClosed {
		return
	}

	if !peerSent {
		t.conn.Conn().WriteControl(websocket.CloseMessage, websocket.FormatCloseMessage(websocket.CloseNormalClosure, ""), time.Now().Add(time.Second))
	}
	t.isClosed = true
	close(t.closeChan)
	t.conn.Close()
	if t.closeHandler != nil {
		t.closeHandler()
	}
}

func (t *Tunnel) Send(kind int, message any, reChan ...chan protocol.Message) error {
	data, err := json.Marshal(message)
	if err != nil {
		return err
	}

	msg := protocol.Message{
		ID:      uuid.New().String(),
		Kind:    kind,
		Payload: data,
	}
	if len(reChan) > 0 {
		t.responseChannels.SetNX(msg.ID, reChan)
	}
	return t.conn.WriteJSON(msg)
}

func (t *Tunnel) SendResponse(kind int, id string, message any) error {
	data, err := json.Marshal(message)
	if err != nil {
		return err
	}

	msg := protocol.Message{
		ID:      uuid.New().String(),
		RE:      id,
		Kind:    kind,
		Payload: data,
	}
	return t.conn.WriteJSON(msg)
}

func (t *Tunnel) Listen(ctx context.Context) {
	go func() {
		select {
		case <-ctx.Done():
			t.close(false)
		case <-t.closeChan:
			return
		}
	}()

	for {
		var msg protocol.Message
		err := t.conn.ReadJSON(&msg)
		if err != nil {
			// if err is websocket.CloseError, we need to close the tunnel
			switch v := err.(type) {
			case *websocket.CloseError:
				if v.Code != websocket.CloseNormalClosure {
					log.Error("receive non-normal closure", "code", v.Code, "text", v.Text)
				}
				t.close(true)
				return
			default:
				t.close(false)
				return
			}
		}

		// Handle the message
		go func(msg protocol.Message) {

			// If a message contains a RE, it is a response to a previous message
			// We need to send it to the channel(s) waiting for the response
			if msg.RE != "" {
				if reChans, ok := t.responseChannels.Get(msg.RE); ok {
					var wg sync.WaitGroup
					for _, reChan := range reChans {
						wg.Add(1)
						go func(reChan chan protocol.Message) {
							defer wg.Done()
							reChan <- msg
						}(reChan)
					}
					wg.Wait()
					t.responseChannels.Delete(msg.RE)
				}
				return
			}

			if handler, ok := t.handlerRegistry[msg.Kind]; ok {
				handler(t, msg.ID, msg.Payload)
			} else {
				log.Error("no handler registered for message kind", "kind", msg.Kind)
			}
		}(msg)
	}
}

func (t *Tunnel) registerHandler(kind int, handler func(tunnel *Tunnel, id string, payload []byte)) {
	t.handlerRegistry[kind] = handler
}

func HandlerFunc[T any](handler func(tunnel *Tunnel, id string, payload T)) func(tunnel *Tunnel, id string, payload []byte) {
	return func(tunnel *Tunnel, id string, payload []byte) {
		var tPayload T
		if err := json.Unmarshal(payload, &tPayload); err != nil {
			log.Error("failed to unmarshal payload", "error", err.Error())
			return
		}
		handler(tunnel, id, tPayload)
	}
}

func (t *Tunnel) RegisterTextHandler(handler func(tunnel *Tunnel, id string, payload protocol.TextPayload)) {
	t.registerHandler(protocol.MessageKindText, HandlerFunc(handler))
}

func (t *Tunnel) RegisterHttpRequestHandler(handler func(tunnel *Tunnel, id string, payload protocol.HttpRequestPayload)) {
	t.registerHandler(protocol.MessageKindHttpRequest, HandlerFunc(handler))
}

func (t *Tunnel) RegisterWebsocketCreateRequestHandler(handler func(tunnel *Tunnel, id string, payload protocol.WebsocketCreateRequestPayload)) {
	t.registerHandler(protocol.MessageKindWebsocketCreateRequest, HandlerFunc(handler))
}

func (t *Tunnel) RegisterWebsocketMessageHandler(handler func(tunnel *Tunnel, id string, payload protocol.WebsocketMessagePayload)) {
	t.registerHandler(protocol.MessageKindWebsocketMessage, HandlerFunc(handler))
}

func (t *Tunnel) RegisterWebsocketCloseHandler(handler func(tunnel *Tunnel, id string, payload protocol.WebsocketClosePayload)) {
	t.registerHandler(protocol.MessageKindWebsocketClose, HandlerFunc(handler))
}
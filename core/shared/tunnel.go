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

	// Context for storing arbitrary data
	context   map[string]interface{}
	contextMu sync.RWMutex

	// Track last time a message was received
	lastReceiveTime time.Time
	lastReceiveMu   sync.RWMutex
}

func NewTunnel(conn *websocket.Conn) *Tunnel {
	return &Tunnel{
		conn:             safe.NewWSConn(conn),
		responseChannels: safe.NewMap[string, []chan protocol.Message](),
		closeChan:        make(chan struct{}),
		handlerRegistry:  make(map[int]func(tunnel *Tunnel, id string, payload []byte)),
		context:          make(map[string]interface{}),
		lastReceiveTime:  time.Now(),
	}
}

func (t *Tunnel) Close() {
	t.close(false)
}

func (t *Tunnel) IsClosed() bool {
	t.closeMu.Lock()
	defer t.closeMu.Unlock()
	return t.isClosed
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

func (t *Tunnel) SendWithResponseChannel(kind int, message any, reChan chan protocol.Message) (func(), error) {
	data, err := json.Marshal(message)
	if err != nil {
		return func() {}, err
	}
	msg := protocol.Message{
		ID:      uuid.New().String(),
		Kind:    kind,
		Payload: data,
	}
	t.responseChannels.SetNX(msg.ID, []chan protocol.Message{reChan})
	clean := func() {
		t.responseChannels.Delete(msg.ID)
	}
	return clean, t.conn.WriteJSON(msg)
}

func (t *Tunnel) Send(kind int, message any) error {
	data, err := json.Marshal(message)
	if err != nil {
		return err
	}

	msg := protocol.Message{
		ID:      uuid.New().String(),
		Kind:    kind,
		Payload: data,
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

		// Update last receive time
		t.lastReceiveMu.Lock()
		t.lastReceiveTime = time.Now()
		t.lastReceiveMu.Unlock()

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

// SetContext stores a value in the tunnel's context with the given key.
func (t *Tunnel) SetContext(key string, value interface{}) {
	t.contextMu.Lock()
	defer t.contextMu.Unlock()
	t.context[key] = value
}

// GetContext retrieves a value from the tunnel's context by key.
func (t *Tunnel) GetContext(key string) interface{} {
	t.contextMu.RLock()
	defer t.contextMu.RUnlock()
	return t.context[key]
}

// LastReceiveTime returns the time when the last message was received.
func (t *Tunnel) LastReceiveTime() time.Time {
	t.lastReceiveMu.RLock()
	defer t.lastReceiveMu.RUnlock()
	return t.lastReceiveTime
}

func (t *Tunnel) registerHandler(kind int, handler func(tunnel *Tunnel, id string, payload []byte)) {
	t.handlerRegistry[kind] = handler
}

func handlerFunc[T any](handler func(tunnel *Tunnel, id string, payload T)) func(tunnel *Tunnel, id string, payload []byte) {
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
	t.registerHandler(protocol.MessageKindText, handlerFunc(handler))
}

func (t *Tunnel) RegisterHttpRequestHandler(handler func(tunnel *Tunnel, id string, payload protocol.HttpRequestPayload)) {
	t.registerHandler(protocol.MessageKindHttpRequest, handlerFunc(handler))
}

func (t *Tunnel) RegisterWebsocketCreateRequestHandler(handler func(tunnel *Tunnel, id string, payload protocol.WebsocketCreateRequestPayload)) {
	t.registerHandler(protocol.MessageKindWebsocketCreateRequest, handlerFunc(handler))
}

func (t *Tunnel) RegisterWebsocketMessageHandler(handler func(tunnel *Tunnel, id string, payload protocol.WebsocketMessagePayload)) {
	t.registerHandler(protocol.MessageKindWebsocketMessage, handlerFunc(handler))
}

func (t *Tunnel) RegisterWebsocketCloseHandler(handler func(tunnel *Tunnel, id string, payload protocol.WebsocketClosePayload)) {
	t.registerHandler(protocol.MessageKindWebsocketClose, handlerFunc(handler))
}

func (t *Tunnel) RegisterSSERequestHandler(handler func(tunnel *Tunnel, id string, payload protocol.SSERequestPayload)) {
	t.registerHandler(protocol.MessageKindSSERequest, handlerFunc(handler))
}

func (t *Tunnel) RegisterSSEMessageHandler(handler func(tunnel *Tunnel, id string, payload protocol.SSEMessagePayload)) {
	t.registerHandler(protocol.MessageKindSSEMessage, handlerFunc(handler))
}

func (t *Tunnel) RegisterSSECloseHandler(handler func(tunnel *Tunnel, id string, payload protocol.SSEClosePayload)) {
	t.registerHandler(protocol.MessageKindSSEClose, handlerFunc(handler))
}

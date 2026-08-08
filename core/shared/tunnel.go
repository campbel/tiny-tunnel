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
	closeChan    chan struct{} // Channel to signal tunnel closure
	closeMu      sync.Mutex

	// For context management and cleanup
	ctx        context.Context
	cancelFunc context.CancelFunc

	// responseChannels is a map of message IDs to the channel that wants to receive the response
	responseChannels *safe.Map[string, chan protocol.Message]

	// Handlers
	handlerRegistry map[int]func(tunnel *Tunnel, id string, payload []byte)

	// Context for storing arbitrary data
	context   map[string]interface{}
	contextMu sync.RWMutex

	// Track last time a message was received
	lastReceiveTime time.Time
	lastReceiveMu   sync.RWMutex

	l log.Logger
}

func NewTunnel(conn *websocket.Conn, l log.Logger) *Tunnel {
	ctx, cancel := context.WithCancel(context.Background())
	return &Tunnel{
		conn:             safe.NewWSConn(conn),
		responseChannels: safe.NewMap[string, chan protocol.Message](),
		closeChan:        make(chan struct{}),
		handlerRegistry:  make(map[int]func(tunnel *Tunnel, id string, payload []byte)),
		context:          make(map[string]interface{}),
		lastReceiveTime:  time.Now(),
		ctx:              ctx,
		cancelFunc:       cancel,
		l:                l,
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

	// Mark as closed first to prevent new messages from being sent
	t.isClosed = true

	// Cancel the context to notify dependent goroutines
	if t.cancelFunc != nil {
		t.cancelFunc()
	}

	// Close the channel to notify any goroutines waiting.
	// Consumers of response channels must select on Done() to unblock;
	// we intentionally do not close the response channels themselves to
	// avoid send-on-closed-channel races with the read loop.
	close(t.closeChan)

	// Close the connection with timeout
	if !peerSent {
		t.conn.CloseWithTimeout(time.Second)
	} else {
		t.conn.Close()
	}

	// Call the close handler if set
	if t.closeHandler != nil {
		t.closeHandler()
	}
}

// SendWithResponseChannel sends a message and registers reChan to receive
// responses addressed to it (RE == returned id). The returned id can be used
// to reference the request in follow-up messages (e.g. stream cancellation).
func (t *Tunnel) SendWithResponseChannel(kind int, message any, reChan chan protocol.Message) (string, func(), error) {
	data, err := json.Marshal(message)
	if err != nil {
		return "", func() {}, err
	}
	msg := protocol.Message{
		ID:      uuid.New().String(),
		Kind:    kind,
		Payload: data,
	}
	t.responseChannels.SetNX(msg.ID, reChan)
	clean := func() {
		t.responseChannels.Delete(msg.ID)
	}
	return msg.ID, clean, t.conn.WriteJSON(msg)
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
					t.l.Error("receive non-normal closure", "code", v.Code, "text", v.Text)
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

		// Handle the message synchronously so that messages are processed in
		// the exact order they arrive on the websocket. Handlers that perform
		// long-running work (e.g. proxying an HTTP request) are responsible
		// for spawning their own goroutines so they don't block this loop.

		// If a message contains a RE, it is a response to a previous message.
		// Deliver it in order to the channel waiting for the response. If the
		// channel's buffer is full this applies backpressure to the tunnel;
		// consumers must keep draining until the stream ends. Done() unblocks
		// delivery when the tunnel closes.
		if msg.RE != "" {
			if reChan, ok := t.responseChannels.Get(msg.RE); ok {
				select {
				case reChan <- msg:
				case <-t.closeChan:
					return
				}
			}
			continue
		}

		if handler, ok := t.handlerRegistry[msg.Kind]; ok {
			handler(t, msg.ID, msg.Payload)
		} else {
			t.l.Error("no handler registered for message kind", "kind", msg.Kind)
		}
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

// Context returns the tunnel's context, which is cancelled when the tunnel is closed
func (t *Tunnel) Context() context.Context {
	return t.ctx
}

// Done returns a channel that's closed when the tunnel is closed
func (t *Tunnel) Done() <-chan struct{} {
	return t.closeChan
}

func (t *Tunnel) registerHandler(kind int, handler func(tunnel *Tunnel, id string, payload []byte)) {
	t.handlerRegistry[kind] = handler
}

func handlerFunc[T any](handler func(tunnel *Tunnel, id string, payload T)) func(tunnel *Tunnel, id string, payload []byte) {
	return func(tunnel *Tunnel, id string, payload []byte) {
		var tPayload T
		if err := json.Unmarshal(payload, &tPayload); err != nil {
			tunnel.l.Error("failed to unmarshal payload", "error", err.Error())
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

func (t *Tunnel) RegisterHttpStreamCancelHandler(handler func(tunnel *Tunnel, id string, payload protocol.HttpStreamCancelPayload)) {
	t.registerHandler(protocol.MessageKindHttpStreamCancel, handlerFunc(handler))
}

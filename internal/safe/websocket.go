package safe

import (
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

type WSConn struct {
	mu     sync.Mutex
	closed bool
	conn   *websocket.Conn
}

func NewWSConn(conn *websocket.Conn) *WSConn {
	return &WSConn{
		conn: conn,
	}
}

func (w *WSConn) WriteMessage(messageType int, data []byte) error {
	w.mu.Lock()
	defer w.mu.Unlock()

	if w.closed {
		return websocket.ErrCloseSent
	}

	return w.conn.WriteMessage(messageType, data)
}

func (w *WSConn) WriteJSON(v any) error {
	w.mu.Lock()
	defer w.mu.Unlock()

	if w.closed {
		return websocket.ErrCloseSent
	}

	return w.conn.WriteJSON(v)
}

func (w *WSConn) Conn() *websocket.Conn {
	return w.conn
}

func (w *WSConn) ReadMessage() (int, []byte, error) {
	// ReadMessage doesn't need a lock as it's a blocking operation
	// that will return an error if the connection is closed
	return w.conn.ReadMessage()
}

func (w *WSConn) ReadJSON(v any) error {
	// ReadJSON doesn't need a lock as it's a blocking operation
	// that will return an error if the connection is closed
	return w.conn.ReadJSON(v)
}

// SetReadDeadline sets the read deadline on the underlying connection.
func (w *WSConn) SetReadDeadline(t time.Time) error {
	return w.conn.SetReadDeadline(t)
}

// SetWriteDeadline sets the write deadline on the underlying connection.
func (w *WSConn) SetWriteDeadline(t time.Time) error {
	return w.conn.SetWriteDeadline(t)
}

// CloseWithTimeout forcefully closes the connection with a timeout
func (w *WSConn) CloseWithTimeout(timeout time.Duration) error {
	w.mu.Lock()
	defer w.mu.Unlock()

	if w.closed {
		return nil
	}

	// Set a short deadline for the close message
	_ = w.conn.SetWriteDeadline(time.Now().Add(timeout))

	// Send a close message, but don't wait indefinitely
	_ = w.conn.WriteControl(
		websocket.CloseMessage,
		websocket.FormatCloseMessage(websocket.CloseNormalClosure, ""),
		time.Now().Add(timeout),
	)

	// Mark as closed
	w.closed = true

	// Set a read deadline to unblock any reads
	_ = w.conn.SetReadDeadline(time.Now())

	// Close the underlying connection
	return w.conn.Close()
}

func (w *WSConn) Close() error {
	// Use CloseWithTimeout with a short timeout
	return w.CloseWithTimeout(1 * time.Second)
}

// IsClosed returns whether the connection has been closed
func (w *WSConn) IsClosed() bool {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.closed
}

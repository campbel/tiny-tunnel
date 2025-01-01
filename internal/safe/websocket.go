package safe

import (
	"sync"

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
	return w.conn.WriteMessage(messageType, data)
}

func (w *WSConn) WriteJSON(v any) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.conn.WriteJSON(v)
}

func (w *WSConn) Conn() *websocket.Conn {
	return w.conn
}

func (w *WSConn) ReadMessage() (int, []byte, error) {
	return w.conn.ReadMessage()
}

func (w *WSConn) ReadJSON(v any) error {
	return w.conn.ReadJSON(v)
}

func (w *WSConn) Close() error {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.closed {
		return nil
	}
	w.closed = true
	return w.conn.Close()
}

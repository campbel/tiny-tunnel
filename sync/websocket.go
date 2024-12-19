package sync

import (
	"sync"

	"github.com/gorilla/websocket"
)

type WSConn struct {
	mu   sync.Mutex
	conn *websocket.Conn
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

func (w *WSConn) Conn() *websocket.Conn {
	return w.conn
}

func (w *WSConn) ReadMessage() (int, []byte, error) {
	return w.conn.ReadMessage()
}

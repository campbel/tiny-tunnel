package stats

import (
	"sync"
)

type StatsProvider interface {
	GetStats() map[string]any
	GetHttpStats() HttpStats
	GetWebsocketStats() WebsocketStats
	GetSseStats() ServerSentEventsStats
	GetWebsocketConnections() int
	IncrementWebsocketConnection()
	DecrementWebsocketConnection()
	IncrementWebsocketMessageSent()
	IncrementWebsocketMessageRecv()
	IncrementHttpRequest()
	IncrementHttpResponse()
	IncrementSseConnection()
	DecrementSseConnection()
	IncrementSseMessageRecv()
}

func NewTunnelStats() *Stats {
	return &Stats{}
}

type Stats struct {
	sync.Mutex
	websocket WebsocketStats
	http      HttpStats
	sse       ServerSentEventsStats
}

type WebsocketStats struct {
	TotalConnections  int
	ActiveConnections int
	TotalMessagesSent int
	TotalMessagesRecv int
}

type HttpStats struct {
	TotalRequests  int
	TotalResponses int
}

type ServerSentEventsStats struct {
	TotalConnections  int
	ActiveConnections int
	TotalMessagesRecv int
}

func (t *Stats) GetStats() map[string]any {
	return map[string]any{
		"websocket": t.websocket,
		"http":      t.http,
		"sse":       t.sse,
	}
}

func (t *Stats) GetHttpStats() HttpStats {
	t.Lock()
	defer t.Unlock()
	return t.http
}
func (t *Stats) GetWebsocketStats() WebsocketStats {
	t.Lock()
	defer t.Unlock()
	return t.websocket
}

func (t *Stats) GetSseStats() ServerSentEventsStats {
	t.Lock()
	defer t.Unlock()
	return t.sse
}

func (t *Stats) GetWebsocketConnections() int {
	t.Lock()
	defer t.Unlock()
	return t.websocket.ActiveConnections
}

func (t *Stats) IncrementWebsocketConnection() {
	t.Lock()
	defer t.Unlock()
	t.websocket.TotalConnections++
	t.websocket.ActiveConnections++
}

func (t *Stats) DecrementWebsocketConnection() {
	t.Lock()
	defer t.Unlock()
	t.websocket.ActiveConnections--
}

func (t *Stats) IncrementWebsocketMessageSent() {
	t.Lock()
	defer t.Unlock()
	t.websocket.TotalMessagesSent++
}

func (t *Stats) IncrementWebsocketMessageRecv() {
	t.Lock()
	defer t.Unlock()
	t.websocket.TotalMessagesRecv++
}

func (t *Stats) IncrementHttpRequest() {
	t.Lock()
	defer t.Unlock()
	t.http.TotalRequests++
}

func (t *Stats) IncrementHttpResponse() {
	t.Lock()
	defer t.Unlock()
	t.http.TotalResponses++
}

func (t *Stats) IncrementSseConnection() {
	t.Lock()
	defer t.Unlock()
	t.sse.TotalConnections++
	t.sse.ActiveConnections++
}

func (t *Stats) DecrementSseConnection() {
	t.Lock()
	defer t.Unlock()
	t.sse.ActiveConnections--
}

func (t *Stats) IncrementSseMessageRecv() {
	t.Lock()
	defer t.Unlock()
	t.sse.TotalMessagesRecv++
}

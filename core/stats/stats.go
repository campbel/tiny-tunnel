package stats

import (
	"sync"
)

type Provider interface {
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

type Tracker struct {
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

func (t *Tracker) GetStats() map[string]any {
	return map[string]any{
		"websocket": t.websocket,
		"http":      t.http,
		"sse":       t.sse,
	}
}

func (t *Tracker) GetHttpStats() HttpStats {
	t.Lock()
	defer t.Unlock()
	return t.http
}
func (t *Tracker) GetWebsocketStats() WebsocketStats {
	t.Lock()
	defer t.Unlock()
	return t.websocket
}

func (t *Tracker) GetSseStats() ServerSentEventsStats {
	t.Lock()
	defer t.Unlock()
	return t.sse
}

func (t *Tracker) IncrementWebsocketConnection() {
	t.Lock()
	defer t.Unlock()
	t.websocket.TotalConnections++
	t.websocket.ActiveConnections++
}

func (t *Tracker) DecrementWebsocketConnection() {
	t.Lock()
	defer t.Unlock()
	t.websocket.ActiveConnections--
}

func (t *Tracker) IncrementWebsocketMessageSent() {
	t.Lock()
	defer t.Unlock()
	t.websocket.TotalMessagesSent++
}

func (t *Tracker) IncrementWebsocketMessageRecv() {
	t.Lock()
	defer t.Unlock()
	t.websocket.TotalMessagesRecv++
}

func (t *Tracker) IncrementHttpRequest() {
	t.Lock()
	defer t.Unlock()
	t.http.TotalRequests++
}

func (t *Tracker) IncrementHttpResponse() {
	t.Lock()
	defer t.Unlock()
	t.http.TotalResponses++
}

func (t *Tracker) IncrementSseConnection() {
	t.Lock()
	defer t.Unlock()
	t.sse.TotalConnections++
	t.sse.ActiveConnections++
}

func (t *Tracker) DecrementSseConnection() {
	t.Lock()
	defer t.Unlock()
	t.sse.ActiveConnections--
}

func (t *Tracker) IncrementSseMessageRecv() {
	t.Lock()
	defer t.Unlock()
	t.sse.TotalMessagesRecv++
}

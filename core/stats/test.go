package stats

import "time"

var _ StatsProvider = &TestStatsProvider{}

type TestStatsProvider struct {
	websocketConnections  int
	websocketMessagesSent int
	websocketMessagesRecv int
	httpRequests          int
	httpResponses         int
	sseConnections        int
	sseMessagesRecv       int
}

func NewTestStatsProvider() *TestStatsProvider {
	return &TestStatsProvider{}
}

func (p *TestStatsProvider) IncrementWebsocketConnection() {
	p.websocketConnections++
}

func (p *TestStatsProvider) DecrementWebsocketConnection() {
	p.websocketConnections--
}

func (p *TestStatsProvider) IncrementWebsocketMessageSent() {
	p.websocketMessagesSent++
}

func (p *TestStatsProvider) IncrementWebsocketMessageRecv() {
	p.websocketMessagesRecv++
}

func (p *TestStatsProvider) IncrementHttpRequest() {
	p.httpRequests++
}

func (p *TestStatsProvider) IncrementHttpResponse() {
	p.httpResponses++
}

func (p *TestStatsProvider) IncrementSseConnection() {
	p.sseConnections++
}

func (p *TestStatsProvider) DecrementSseConnection() {
	p.sseConnections--
}

func (p *TestStatsProvider) IncrementSseMessageRecv() {
	p.sseMessagesRecv++
}

func (p *TestStatsProvider) GetWebsocketConnections() int {
	return p.websocketConnections
}

func (p *TestStatsProvider) GetStats() map[string]any {
	return map[string]any{
		"websocket": p.websocketConnections,
		"http":      p.httpRequests,
		"sse":       p.sseConnections,
	}
}

func (p *TestStatsProvider) GetHttpStats() HttpStats {
	return HttpStats{
		TotalRequests:  p.httpRequests,
		TotalResponses: p.httpResponses,
	}
}

func (p *TestStatsProvider) GetWebsocketStats() WebsocketStats {
	return WebsocketStats{
		TotalConnections:  p.websocketConnections,
		ActiveConnections: p.websocketConnections,
		TotalMessagesSent: p.websocketMessagesSent,
		TotalMessagesRecv: p.websocketMessagesRecv,
	}
}

func (p *TestStatsProvider) GetSseStats() ServerSentEventsStats {
	return ServerSentEventsStats{
		TotalConnections:  p.sseConnections,
		ActiveConnections: p.sseConnections,
		TotalMessagesRecv: p.sseMessagesRecv,
	}
}

func (p *TestStatsProvider) Reset() {
	p.websocketConnections = 0
	p.websocketMessagesSent = 0
	p.websocketMessagesRecv = 0
	p.httpRequests = 0
	p.httpResponses = 0
	p.sseConnections = 0
	p.sseMessagesRecv = 0
}

var _ StateProvider = &TestStateProvider{}

type TestStateProvider struct {
	status             Status
	statusMessage      string
	url                string
	target             string
	connectionDuration time.Duration
}

func NewTestStateProvider() *TestStateProvider {
	return &TestStateProvider{}
}

func (p *TestStateProvider) SetStatus(status Status) {
	p.status = status
}

func (p *TestStateProvider) SetStatusMessage(message string) {
	p.statusMessage = message
}

func (p *TestStateProvider) SetURL(url string) {
	p.url = url
}

func (p *TestStateProvider) GetStatus() Status {
	return p.status
}

func (p *TestStateProvider) GetConnectionDuration() time.Duration {
	return p.connectionDuration
}

func (p *TestStateProvider) GetURL() string {
	return p.url
}

func (p *TestStateProvider) GetTarget() string {
	return p.target
}

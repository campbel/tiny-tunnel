package stats

import (
	"sync"
	"time"
)

// TunnelState represents the current state of a tunnel connection.
type TunnelState struct {
	mu               sync.RWMutex
	status           Status
	connectionTime   time.Time
	disconnectedTime time.Time
	url              string
	target           string
	name             string
	logs             []LogEntry
	logViewMode      bool
	statusMessage    string
	tracker          *Tracker
	subscribers      []StateSubscriber
}

// Status represents the connection status of the tunnel.
type Status string

// Status constants
const (
	StatusDisconnected Status = "disconnected"
	StatusConnecting   Status = "connecting"
	StatusConnected    Status = "connected"
	StatusError        Status = "error"
)

// LogEntry represents a log message with metadata.
type LogEntry struct {
	Timestamp time.Time
	Level     string
	Message   string
	Fields    map[string]interface{}
}

// StateSubscriber is the interface that must be implemented by state observers.
type StateSubscriber interface {
	OnStateUpdate(state *TunnelState)
}

// NewTunnelState creates a new tunnel state with the given target and name.
func NewTunnelState(target string, name string) *TunnelState {
	return &TunnelState{
		status:        StatusDisconnected,
		target:        target,
		name:          name,
		logs:          make([]LogEntry, 0),
		tracker:       new(Tracker),
		subscribers:   make([]StateSubscriber, 0),
		statusMessage: "Initializing...",
	}
}

// Subscribe registers a new subscriber to receive state updates.
func (s *TunnelState) Subscribe(subscriber StateSubscriber) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.subscribers = append(s.subscribers, subscriber)
	// Immediately notify the subscriber of the current state
	go subscriber.OnStateUpdate(s)
}

// Unsubscribe removes a subscriber from receiving state updates.
func (s *TunnelState) Unsubscribe(subscriber StateSubscriber) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i, sub := range s.subscribers {
		if sub == subscriber {
			s.subscribers = append(s.subscribers[:i], s.subscribers[i+1:]...)
			break
		}
	}
}

// notifySubscribers notifies all subscribers of a state change.
func (s *TunnelState) notifySubscribers() {
	s.mu.RLock()
	subscribers := make([]StateSubscriber, len(s.subscribers))
	copy(subscribers, s.subscribers)
	s.mu.RUnlock()

	for _, subscriber := range subscribers {
		go subscriber.OnStateUpdate(s)
	}
}

// GetTracker returns the stats tracker.
func (s *TunnelState) GetTracker() *Tracker {
	return s.tracker
}

// SetStatus sets the tunnel connection status.
func (s *TunnelState) SetStatus(status Status) {
	s.mu.Lock()
	defer s.mu.Unlock()

	// Only update if the status has changed
	if s.status != status {
		s.status = status

		// Update timestamps based on status
		switch status {
		case StatusConnected:
			s.connectionTime = time.Now()
		case StatusDisconnected:
			s.disconnectedTime = time.Now()
		}

		// Notify subscribers of the state change
		go s.notifySubscribers()
	}
}

// SetStatusMessage sets a status message for display.
func (s *TunnelState) SetStatusMessage(message string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.statusMessage = message
	go s.notifySubscribers()
}

// SetURL sets the public URL for the tunnel.
func (s *TunnelState) SetURL(url string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.url = url
	go s.notifySubscribers()
}

// AddLogEntry adds a log entry to the state.
func (s *TunnelState) AddLogEntry(entry LogEntry) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.logs = append(s.logs, entry)

	// Parse URL from welcome message
	if entry.Message != "" && s.url == "" {
		// Look for a URL pattern in the message that might contain the public URL
		// This is a simple example; adjust based on actual log patterns
		for key, value := range entry.Fields {
			if key == "url" || key == "public_url" {
				if url, ok := value.(string); ok {
					s.url = url
				}
			}
		}
	}

	go s.notifySubscribers()
}

// ToggleLogViewMode toggles between log view and stats view.
func (s *TunnelState) ToggleLogViewMode() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.logViewMode = !s.logViewMode
	go s.notifySubscribers()
}

// GetStatus returns the current connection status.
func (s *TunnelState) GetStatus() Status {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.status
}

// GetConnectionDuration returns the duration since the connection was established.
func (s *TunnelState) GetConnectionDuration() time.Duration {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.status != StatusConnected {
		return 0
	}
	return time.Since(s.connectionTime)
}

// GetURL returns the public URL of the tunnel.
func (s *TunnelState) GetURL() string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.url
}

// GetTarget returns the target URL the tunnel is forwarding to.
func (s *TunnelState) GetTarget() string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.target
}

// GetName returns the name of the tunnel.
func (s *TunnelState) GetName() string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.name
}

// GetLogs returns all log entries.
func (s *TunnelState) GetLogs() []LogEntry {
	s.mu.RLock()
	defer s.mu.RUnlock()
	logs := make([]LogEntry, len(s.logs))
	copy(logs, s.logs)
	return logs
}

// GetStatusMessage returns the current status message.
func (s *TunnelState) GetStatusMessage() string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.statusMessage
}

// IsLogViewMode returns whether the state is in log view mode.
func (s *TunnelState) IsLogViewMode() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.logViewMode
}

// TunnelStateProvider implements the Provider interface to track stats
// and forward them to the TunnelState.
type TunnelStateProvider struct {
	state *TunnelState
}

// NewTunnelStateProvider creates a new provider that updates the given state.
func NewTunnelStateProvider(state *TunnelState) *TunnelStateProvider {
	return &TunnelStateProvider{
		state: state,
	}
}

// Forward Provider interface methods to the Tracker in TunnelState

func (p *TunnelStateProvider) IncrementWebsocketConnection() {
	p.state.tracker.IncrementWebsocketConnection()
	p.state.notifySubscribers()
}

func (p *TunnelStateProvider) DecrementWebsocketConnection() {
	p.state.tracker.DecrementWebsocketConnection()
	p.state.notifySubscribers()
}

func (p *TunnelStateProvider) IncrementWebsocketMessageSent() {
	p.state.tracker.IncrementWebsocketMessageSent()
	p.state.notifySubscribers()
}

func (p *TunnelStateProvider) IncrementWebsocketMessageRecv() {
	p.state.tracker.IncrementWebsocketMessageRecv()
	p.state.notifySubscribers()
}

func (p *TunnelStateProvider) IncrementHttpRequest() {
	p.state.tracker.IncrementHttpRequest()
	p.state.notifySubscribers()
}

func (p *TunnelStateProvider) IncrementHttpResponse() {
	p.state.tracker.IncrementHttpResponse()
	p.state.notifySubscribers()
}

func (p *TunnelStateProvider) IncrementSseConnection() {
	p.state.tracker.IncrementSseConnection()
	p.state.notifySubscribers()
}

func (p *TunnelStateProvider) DecrementSseConnection() {
	p.state.tracker.DecrementSseConnection()
	p.state.notifySubscribers()
}

func (p *TunnelStateProvider) IncrementSseMessageRecv() {
	p.state.tracker.IncrementSseMessageRecv()
	p.state.notifySubscribers()
}
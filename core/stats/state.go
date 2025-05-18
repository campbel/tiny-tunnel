package stats

import (
	"sync"
	"time"
)

type StateProvider interface {
	SetStatus(status Status)
	SetStatusMessage(message string)
	SetURL(url string)
	GetStatus() Status
	GetConnectionDuration() time.Duration
	GetURL() string
	GetTarget() string
}

// TunnelState represents the current state of a tunnel connection.
type TunnelState struct {
	mu               sync.RWMutex
	status           Status
	connectionTime   time.Time
	disconnectedTime time.Time
	url              string
	target           string
	name             string
	statusMessage    string
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

// NewTunnelState creates a new tunnel state with the given target and name.
func NewTunnelState(target string, name string) *TunnelState {
	return &TunnelState{
		status:        StatusDisconnected,
		target:        target,
		name:          name,
		statusMessage: "Initializing...",
	}
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
	}
}

// SetStatusMessage sets a status message for display.
func (s *TunnelState) SetStatusMessage(message string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.statusMessage = message
}

// SetURL sets the public URL for the tunnel.
func (s *TunnelState) SetURL(url string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.url = url
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

// GetStatusMessage returns the current status message.
func (s *TunnelState) GetStatusMessage() string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.statusMessage
}

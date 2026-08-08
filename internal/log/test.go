package log

import (
	"fmt"
	"slices"
	"sync"
)

// TestLogger is a logger that records all messages to a slice
type TestLogger struct {
	mu       sync.Mutex
	messages []string
}

func (l *TestLogger) log(message string, args ...any) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.messages = append(l.messages, fmt.Sprintf(message, args...))
}

func NewTestLogger() *TestLogger {
	return &TestLogger{
		messages: []string{},
	}
}

func (l *TestLogger) Debug(message string, args ...any) {
	l.log(message, args...)
}

func (l *TestLogger) Info(message string, args ...any) {
	l.log(message, args...)
}

func (l *TestLogger) Warn(message string, args ...any) {
	l.log(message, args...)
}

func (l *TestLogger) Error(message string, args ...any) {
	l.log(message, args...)
}

func (l *TestLogger) Reset() {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.messages = []string{}
}

// Messages returns a copy of the messages
func (l *TestLogger) Messages() []string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return slices.Clone(l.messages)
}

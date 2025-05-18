package log

import (
	"fmt"
	"slices"
)

// TestLogger is a logger that records all messages to a slice
type TestLogger struct {
	messages []string
}

func NewTestLogger() *TestLogger {
	return &TestLogger{
		messages: []string{},
	}
}

func (l *TestLogger) Debug(message string, args ...any) {
	l.messages = append(l.messages, fmt.Sprintf(message, args...))
}

func (l *TestLogger) Info(message string, args ...any) {
	l.messages = append(l.messages, fmt.Sprintf(message, args...))
}

func (l *TestLogger) Warn(message string, args ...any) {
	l.messages = append(l.messages, fmt.Sprintf(message, args...))
}

func (l *TestLogger) Error(message string, args ...any) {
	l.messages = append(l.messages, fmt.Sprintf(message, args...))
}

func (l *TestLogger) Reset() {
	l.messages = []string{}
}

// Messages returns a copy of the messages
func (l *TestLogger) Messages() []string {
	return slices.Clone(l.messages)
}

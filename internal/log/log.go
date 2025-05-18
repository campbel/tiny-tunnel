package log

import (
	"os"

	"github.com/charmbracelet/log"
)

type BasicLogger struct {
	logger *log.Logger
}

func NewBasicLogger(debug bool) *BasicLogger {

	var (
		reportCaller = false
		level        = log.InfoLevel
		formatter    = log.TextFormatter
	)

	if debug {
		reportCaller = true
		level = log.DebugLevel
	}

	return &BasicLogger{
		logger: log.NewWithOptions(os.Stderr, log.Options{
			ReportTimestamp: true,
			TimeFormat:      "15:04:05",
			Level:           level,
			ReportCaller:    reportCaller,
			Formatter:       formatter,
		}),
	}
}

func (l *BasicLogger) Debug(message string, args ...any) {
	l.logger.Helper()
	l.logger.Debug(message, args...)
}

func (l *BasicLogger) Info(message string, args ...any) {
	l.logger.Helper()
	l.logger.Info(message, args...)
}

func (l *BasicLogger) Warn(message string, args ...any) {
	l.logger.Helper()
	l.logger.Warn(message, args...)
}

func (l *BasicLogger) Error(message string, args ...any) {
	l.logger.Helper()
	l.logger.Error(message, args...)
}

package log

import (
	"os"

	"github.com/charmbracelet/log"
)

var logger = log.NewWithOptions(os.Stderr, log.Options{
	ReportTimestamp: true,
	TimeFormat:      "15:04:05",
	Level:           log.InfoLevel,
	ReportCaller:    false,
	Formatter:       log.TextFormatter,
})

func init() {
	if os.Getenv("DEBUG") != "" {
		logger.SetReportCaller(true)
		logger.SetLevel(log.DebugLevel)
	}
}

func Debug(message string, args ...any) {
	logger.Helper()
	logger.Debug(message, args...)
}

func Info(message string, args ...any) {
	logger.Helper()
	logger.Info(message, args...)
}

func Warn(message string, args ...any) {
	logger.Helper()
	logger.Warn(message, args...)
}

func Error(message string, args ...any) {
	logger.Helper()
	logger.Error(message, args...)
}

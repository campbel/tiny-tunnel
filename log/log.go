package log

import (
	"os"

	"github.com/charmbracelet/log"
)

var logger = log.New(os.Stderr)

func init() {
	logger.SetTimeFormat("15:04:05")
	if os.Getenv("DEBUG") != "" {
		logger.SetLevel(log.DebugLevel)
		logger.SetFormatter(log.TextFormatter)
	}
}

func Debug(message string, args ...any) {
	logger.Debug(message, args...)
}

func Info(message string, args ...any) {
	logger.Info(message, args...)
}

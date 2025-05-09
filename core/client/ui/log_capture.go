package ui

import (
	"io"
	"os"
	"regexp"
	"strings"
	"time"

	"github.com/campbel/tiny-tunnel/core/stats"
	"github.com/charmbracelet/log"
)

// LogCapture captures log output and forwards it to the tunnel state.
type LogCapture struct {
	writer  io.Writer
	state   *stats.TunnelState
	urlRe   *regexp.Regexp
}

// NewLogCapture creates a new log capture instance that updates the given state.
func NewLogCapture(state *stats.TunnelState) *LogCapture {
	// Regex to match URLs in log messages
	urlRe := regexp.MustCompile(`https?://[^\s'"]+`)
	
	return &LogCapture{
		state: state,
		urlRe: urlRe,
	}
}

// Start begins capturing log output, redirecting it to the state.
func (lc *LogCapture) Start() {
	// Create a pipe and set up a writer that will be used for log output
	r, w := io.Pipe()
	lc.writer = w
	
	// Create multi-writer to maintain normal log output while also capturing it
	mw := io.MultiWriter(os.Stdout, w)
	
	// Set the logger output to our multi-writer
	log.SetOutput(mw)
	
	// Start a goroutine to read from the pipe and process logs
	go lc.processLogs(r)
}

// Stop stops log capturing and restores the original log output.
func (lc *LogCapture) Stop() {
	if lc.writer != nil {
		// Restore original output
		log.SetOutput(os.Stdout)
		
		// Close our writer
		if closer, ok := lc.writer.(io.Closer); ok {
			closer.Close()
		}
	}
}

// processLogs reads logs from the reader and adds them to the state.
func (lc *LogCapture) processLogs(r io.Reader) {
	buf := make([]byte, 4096)
	
	for {
		n, err := r.Read(buf)
		if err != nil {
			if err != io.EOF {
				// Log the error to the original output
				log.Error("error reading log pipe", "err", err)
			}
			return
		}
		
		// Process the log message
		if n > 0 {
			logMsg := string(buf[:n])
			lc.processLogMessage(logMsg)
		}
	}
}

// processLogMessage parses a log message and adds it to the state.
func (lc *LogCapture) processLogMessage(msg string) {
	// Remove trailing newlines
	msg = strings.TrimSpace(msg)
	if msg == "" {
		return
	}
	
	// Parse the log message (simplified, adjust based on your logging format)
	parts := strings.SplitN(msg, " ", 3)
	
	entry := stats.LogEntry{
		Timestamp: time.Now(),
		Fields:    make(map[string]interface{}),
	}
	
	// Extract log level if available
	if len(parts) >= 2 {
		levelStr := strings.ToLower(parts[0])
		if strings.Contains(levelStr, "info") {
			entry.Level = "info"
		} else if strings.Contains(levelStr, "error") {
			entry.Level = "error"
			// When an error occurs, update the state
			lc.state.SetStatusMessage("Error: " + msg)
			if lc.state.GetStatus() == stats.StatusConnected {
				lc.state.SetStatus(stats.StatusError)
			}
		} else if strings.Contains(levelStr, "debug") {
			entry.Level = "debug"
		} else if strings.Contains(levelStr, "warn") {
			entry.Level = "warn"
		} else {
			entry.Level = "info" // Default level
		}
		
		// Set the rest as the message
		if len(parts) >= 3 {
			entry.Message = strings.Join(parts[1:], " ")
		} else {
			entry.Message = msg
		}
	} else {
		// If we can't parse the level, use the entire message
		entry.Level = "info"
		entry.Message = msg
	}
	
	// Extract any URLs from the message
	urls := lc.urlRe.FindAllString(entry.Message, -1)
	if len(urls) > 0 {
		entry.Fields["url"] = urls[0]
		
		// If the state doesn't have a URL set, and this looks like a welcome message
		// with the tunnel URL, update the state URL
		if lc.state.GetURL() == "" && strings.Contains(strings.ToLower(entry.Message), "tunnel") {
			lc.state.SetURL(urls[0])
		}
	}
	
	// Update connection status based on log content
	if strings.Contains(strings.ToLower(entry.Message), "connected") {
		lc.state.SetStatus(stats.StatusConnected)
		lc.state.SetStatusMessage("Connected")
	} else if strings.Contains(strings.ToLower(entry.Message), "connecting") {
		lc.state.SetStatus(stats.StatusConnecting)
		lc.state.SetStatusMessage("Connecting...")
	} else if strings.Contains(strings.ToLower(entry.Message), "disconnect") {
		lc.state.SetStatus(stats.StatusDisconnected)
		lc.state.SetStatusMessage("Disconnected")
	}
	
	// Add the log entry to the state
	lc.state.AddLogEntry(entry)
}

// Write implements io.Writer to allow LogCapture to be used as a log output.
func (lc *LogCapture) Write(p []byte) (n int, err error) {
	if lc.writer != nil {
		return lc.writer.Write(p)
	}
	return 0, io.ErrClosedPipe
}
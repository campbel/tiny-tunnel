package testing

import (
	"bufio"
	"errors"
	"fmt"
	"strings"
	"time"
)

// findEventInStream reads the SSE stream to find a specific event and validates its data
// using the provided checkFn. It returns true if the event is found and passes validation.
func findEventInStream(reader *bufio.Reader, eventName string, checkFn func(string) bool, timeout time.Duration) (bool, error) {
	eventCh := make(chan string, 1)
	errCh := make(chan error, 1)
	
	go func() {
		var currentEvent string
		var data string
		
		for {
			line, err := reader.ReadString('\n')
			if err != nil {
				errCh <- fmt.Errorf("error reading SSE stream: %w", err)
				return
			}
			
			line = strings.TrimSpace(line)
			if line == "" {
				// Empty line signals the end of an event
				if currentEvent == eventName && data != "" {
					eventCh <- data
					return
				}
				currentEvent = ""
				data = ""
				continue
			}
			
			if strings.HasPrefix(line, "event:") {
				currentEvent = strings.TrimSpace(strings.TrimPrefix(line, "event:"))
			} else if strings.HasPrefix(line, "data:") {
				data = strings.TrimSpace(strings.TrimPrefix(line, "data:"))
			}
		}
	}()
	
	select {
	case data := <-eventCh:
		return checkFn(data), nil
	case err := <-errCh:
		return false, err
	case <-time.After(timeout):
		return false, errors.New("timeout waiting for SSE event")
	}
}
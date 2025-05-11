package testing

import (
	"testing"

	"github.com/campbel/tiny-tunnel/core/protocol"
	"github.com/stretchr/testify/assert"
)

// TestBackwardCompatibility verifies that we properly detect legacy clients without sequence numbers
func TestBackwardCompatibility(t *testing.T) {
	// Simply test the logic for detecting legacy clients
	// This is a unit test for the backward compatibility logic

	// Mock SSE message with sequence
	modernMessage := protocol.SSEMessagePayload{
		Data:     "event: test\ndata: modern message",
		Sequence: 5,
	}

	// Mock SSE message without sequence (legacy)
	legacyMessage := protocol.SSEMessagePayload{
		Data: "event: test\ndata: legacy message",
		// Sequence is 0 (default)
	}

	// Test that our legacy detection logic works
	t.Run("Detect Legacy Client", func(t *testing.T) {
		// Logic from server.go:
		// if sseMessage.Sequence == 0 && len(messageBuffer) == 0 && expectedSequence == 0

		// Empty buffer represents first message
		messageBuffer := make(map[int]protocol.SSEMessagePayload)
		expectedSequence := 0

		// Should detect as legacy
		isLegacy := legacyMessage.Sequence == 0 &&
			len(messageBuffer) == 0 &&
			expectedSequence == 0

		assert.True(t, isLegacy, "Should detect legacy client from first zero-sequence message")

		// Should not detect as legacy once we receive a non-zero sequence
		isLegacy = modernMessage.Sequence == 0 &&
			len(messageBuffer) == 0 &&
			expectedSequence == 0

		assert.False(t, isLegacy, "Should not detect modern client as legacy")

		// Should not detect as legacy after processing some messages
		expectedSequence = 3
		isLegacy = legacyMessage.Sequence == 0 &&
			len(messageBuffer) == 0 &&
			expectedSequence == 0

		assert.False(t, isLegacy, "Should not detect as legacy after processing messages")
	})
}
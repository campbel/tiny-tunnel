package testing

import (
	"bufio"
	"bytes"
	"context"
	"crypto/rand"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/campbel/tiny-tunnel/core/client"
	"github.com/campbel/tiny-tunnel/core/server"
	"github.com/campbel/tiny-tunnel/core/stats"
	"github.com/campbel/tiny-tunnel/internal/log"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// setupStreamingTunnel wires up a tunnel server and a client tunnel pointed at
// the given origin, and returns a function that issues requests through the
// tunnel plus a cleanup function.
func setupStreamingTunnel(t *testing.T, originURL string) (do func(req *http.Request) (*http.Response, error), tunnelURL string) {
	t.Helper()

	tunnelServer := httptest.NewServer(server.NewHandler(server.Options{
		Hostname: "example.com",
	}, log.NewTestLogger()))
	t.Cleanup(tunnelServer.Close)

	serverURL, err := url.Parse(tunnelServer.URL)
	require.NoError(t, err)

	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)

	clientTunnel, err := client.NewTunnel(ctx, client.Options{
		Name:       "streamtest",
		ServerHost: serverURL.Hostname(),
		ServerPort: serverURL.Port(),
		Insecure:   true,
		Target:     originURL,
	}, stats.NewTestStateProvider(), stats.NewTestStatsProvider(), log.NewTestLogger())
	require.NoError(t, err)

	go clientTunnel.Listen(ctx)
	t.Cleanup(clientTunnel.Close)

	// Wait for the tunnel registration to complete
	time.Sleep(200 * time.Millisecond)

	httpClient := &http.Client{}
	do = func(req *http.Request) (*http.Response, error) {
		req.Host = "streamtest.example.com"
		return httpClient.Do(req)
	}
	return do, tunnelServer.URL
}

// TestChunkedJSONStreamThroughTunnel simulates a k8s watch/informer stream: a
// long-lived HTTPS origin emitting newline-delimited JSON with chunked
// transfer encoding (Content-Type: application/json, NOT text/event-stream).
// Every chunk must arrive promptly, in order, and the stream must survive
// well past 10 seconds (regression: SSE streams used to die at 10s).
func TestChunkedJSONStreamThroughTunnel(t *testing.T) {
	if testing.Short() {
		t.Skip("long-running stream test")
	}

	const (
		numEvents = 24
		interval  = 500 * time.Millisecond // 24 * 0.5s = 12s > 10s regression boundary
	)

	// HTTPS origin, like a real k8s apiserver.
	origin := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		flusher := w.(http.Flusher)
		for i := 0; i < numEvents; i++ {
			line, _ := json.Marshal(map[string]any{
				"type":   "MODIFIED",
				"seq":    i,
				"sentAt": time.Now().UnixNano(),
			})
			fmt.Fprintf(w, "%s\n", line)
			flusher.Flush()
			select {
			case <-r.Context().Done():
				return
			case <-time.After(interval):
			}
		}
	}))
	defer origin.Close()

	do, tunnelURL := setupStreamingTunnel(t, origin.URL)

	req, err := http.NewRequest("GET", tunnelURL+"/apis/v1/watch", nil)
	require.NoError(t, err)

	resp, err := do(req)
	require.NoError(t, err)
	defer resp.Body.Close()

	require.Equal(t, http.StatusOK, resp.StatusCode)
	require.Equal(t, "application/json", resp.Header.Get("Content-Type"))

	scanner := bufio.NewScanner(resp.Body)
	received := 0
	for scanner.Scan() {
		var event struct {
			Type   string `json:"type"`
			Seq    int    `json:"seq"`
			SentAt int64  `json:"sentAt"`
		}
		require.NoError(t, json.Unmarshal(scanner.Bytes(), &event))

		// Strict ordering
		require.Equal(t, received, event.Seq, "events must arrive in order")

		// Prompt delivery: each event must arrive shortly after it was sent
		latency := time.Since(time.Unix(0, event.SentAt))
		assert.Less(t, latency, 2*time.Second, "event %d took %s to arrive", event.Seq, latency)

		received++
	}
	require.NoError(t, scanner.Err())
	require.Equal(t, numEvents, received, "all events must be delivered")
}

// TestSSEThroughTunnelStrict verifies exact SSE delivery: every event, in
// order, byte-faithful framing.
func TestSSEThroughTunnelStrict(t *testing.T) {
	const numEvents = 100

	origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")
		flusher := w.(http.Flusher)
		for i := 0; i < numEvents; i++ {
			fmt.Fprintf(w, "event: count\ndata: %d\n\n", i)
			flusher.Flush()
		}
	}))
	defer origin.Close()

	do, tunnelURL := setupStreamingTunnel(t, origin.URL)

	req, err := http.NewRequest("GET", tunnelURL+"/events", nil)
	require.NoError(t, err)
	req.Header.Set("Accept", "text/event-stream")

	resp, err := do(req)
	require.NoError(t, err)
	defer resp.Body.Close()

	require.Equal(t, http.StatusOK, resp.StatusCode)
	require.Equal(t, "text/event-stream", resp.Header.Get("Content-Type"))

	body, err := io.ReadAll(resp.Body)
	require.NoError(t, err)

	var expected strings.Builder
	for i := 0; i < numEvents; i++ {
		fmt.Fprintf(&expected, "event: count\ndata: %d\n\n", i)
	}
	require.Equal(t, expected.String(), string(body), "stream must be byte-identical and complete")
}

// TestLargeChunkThroughTunnel verifies that a large (>64KB) payload streams
// through intact (regression: the old SSE path used a line scanner with a
// 64KB limit).
func TestLargeChunkThroughTunnel(t *testing.T) {
	payload := make([]byte, 256*1024)
	_, err := rand.Read(payload)
	require.NoError(t, err)

	origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/octet-stream")
		flusher := w.(http.Flusher)
		// Flush a header byte first to force chunked (streaming) mode
		w.Write(payload[:1])
		flusher.Flush()
		w.Write(payload[1:])
		flusher.Flush()
	}))
	defer origin.Close()

	do, tunnelURL := setupStreamingTunnel(t, origin.URL)

	req, err := http.NewRequest("GET", tunnelURL+"/blob", nil)
	require.NoError(t, err)

	resp, err := do(req)
	require.NoError(t, err)
	defer resp.Body.Close()

	require.Equal(t, http.StatusOK, resp.StatusCode)
	body, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	require.True(t, bytes.Equal(payload, body), "payload must round-trip byte-identical")
}

// TestStreamCancelPropagation verifies that when the downstream consumer
// disconnects mid-stream, the cancellation propagates through the tunnel and
// the upstream request to the origin is cancelled (no leaked streams).
func TestStreamCancelPropagation(t *testing.T) {
	originCancelled := make(chan struct{})

	origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		flusher := w.(http.Flusher)
		ticker := time.NewTicker(50 * time.Millisecond)
		defer ticker.Stop()
		for i := 0; ; i++ {
			select {
			case <-r.Context().Done():
				close(originCancelled)
				return
			case <-ticker.C:
				fmt.Fprintf(w, "data: %d\n\n", i)
				flusher.Flush()
			}
		}
	}))
	defer origin.Close()

	do, tunnelURL := setupStreamingTunnel(t, origin.URL)

	ctx, cancel := context.WithCancel(context.Background())
	req, err := http.NewRequestWithContext(ctx, "GET", tunnelURL+"/events", nil)
	require.NoError(t, err)

	resp, err := do(req)
	require.NoError(t, err)
	defer resp.Body.Close()

	// Read a few events to prove the stream is live, then disconnect.
	reader := bufio.NewReader(resp.Body)
	for i := 0; i < 3; {
		line, err := reader.ReadString('\n')
		require.NoError(t, err)
		if strings.HasPrefix(line, "data:") {
			i++
		}
	}
	cancel()

	select {
	case <-originCancelled:
		// upstream request was cancelled — no leak
	case <-time.After(5 * time.Second):
		t.Fatal("upstream request was not cancelled after downstream disconnect")
	}
}

// TestBufferedRequestsDuringActiveStream verifies that regular buffered HTTP
// requests keep working while a long-lived stream is active on the same
// tunnel.
func TestBufferedRequestsDuringActiveStream(t *testing.T) {
	origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/stream":
			w.Header().Set("Content-Type", "text/event-stream")
			flusher := w.(http.Flusher)
			ticker := time.NewTicker(20 * time.Millisecond)
			defer ticker.Stop()
			for i := 0; ; i++ {
				select {
				case <-r.Context().Done():
					return
				case <-ticker.C:
					fmt.Fprintf(w, "data: %d\n\n", i)
					flusher.Flush()
				}
			}
		case "/echo":
			body, _ := io.ReadAll(r.Body)
			w.Header().Set("Content-Type", "text/plain")
			w.WriteHeader(http.StatusOK)
			w.Write(body)
		case "/missing":
			http.Error(w, "not found", http.StatusNotFound)
		}
	}))
	defer origin.Close()

	do, tunnelURL := setupStreamingTunnel(t, origin.URL)

	// Open a long-lived stream
	streamCtx, cancelStream := context.WithCancel(context.Background())
	defer cancelStream()
	streamReq, err := http.NewRequestWithContext(streamCtx, "GET", tunnelURL+"/stream", nil)
	require.NoError(t, err)
	streamResp, err := do(streamReq)
	require.NoError(t, err)
	defer streamResp.Body.Close()

	// Confirm the stream is flowing
	reader := bufio.NewReader(streamResp.Body)
	line, err := reader.ReadString('\n')
	require.NoError(t, err)
	require.True(t, strings.HasPrefix(line, "data:"))

	// Fire buffered requests while the stream is active
	for i := 0; i < 20; i++ {
		msg := fmt.Sprintf("hello %d", i)
		req, err := http.NewRequest("POST", tunnelURL+"/echo", strings.NewReader(msg))
		require.NoError(t, err)
		resp, err := do(req)
		require.NoError(t, err)
		body, err := io.ReadAll(resp.Body)
		resp.Body.Close()
		require.NoError(t, err)
		assert.Equal(t, http.StatusOK, resp.StatusCode)
		assert.Equal(t, msg, string(body))
	}

	// Status passthrough for non-200s still works
	req, err := http.NewRequest("GET", tunnelURL+"/missing", nil)
	require.NoError(t, err)
	resp, err := do(req)
	require.NoError(t, err)
	resp.Body.Close()
	assert.Equal(t, http.StatusNotFound, resp.StatusCode)

	// The stream is still alive afterwards
	line, err = reader.ReadString('\n')
	require.NoError(t, err)
	require.NotEmpty(t, line)
}

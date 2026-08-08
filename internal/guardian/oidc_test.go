package guardian

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// newFakeIdP implements Guardian's PAR + /auth/login flow: PAR stores the
// pushed parameters, /auth/login consumes the single-use request_uri and
// redirects back to the stored redirect_uri with ?token=...&state=...
func newFakeIdP(t *testing.T) *httptest.Server {
	t.Helper()

	type parEntry struct {
		redirectURI string
		state       string
	}
	parStore := map[string]parEntry{}

	mux := http.NewServeMux()
	var server *httptest.Server

	mux.HandleFunc("/api/v1/oauth/par", func(w http.ResponseWriter, r *http.Request) {
		require.NoError(t, r.ParseForm())
		assert.Equal(t, "svc_test", r.Form.Get("client_id"))
		redirectURI := r.Form.Get("redirect_uri")
		require.NotEmpty(t, redirectURI)

		requestURI := "urn:guardian:par:test-" + fmt.Sprint(len(parStore))
		parStore[requestURI] = parEntry{redirectURI: redirectURI, state: r.Form.Get("state")}

		w.WriteHeader(http.StatusCreated)
		json.NewEncoder(w).Encode(map[string]any{"request_uri": requestURI, "expires_in": 90})
	})

	mux.HandleFunc("/auth/login", func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		assert.Equal(t, "svc_test", q.Get("client_id"))
		entry, ok := parStore[q.Get("request_uri")]
		require.True(t, ok, "unknown or reused request_uri")
		delete(parStore, q.Get("request_uri")) // single-use

		// Simulate a successful SSO: redirect back with token + state.
		redirect, err := url.Parse(entry.redirectURI)
		require.NoError(t, err)
		rq := redirect.Query()
		rq.Set("token", "test-access-token")
		if entry.state != "" {
			rq.Set("state", entry.state)
		}
		redirect.RawQuery = rq.Encode()

		resp, err := http.Get(redirect.String())
		require.NoError(t, err)
		resp.Body.Close()
		assert.Equal(t, http.StatusOK, resp.StatusCode)
	})

	server = httptest.NewServer(mux)
	t.Cleanup(server.Close)
	return server
}

// browserFetch simulates opening the login URL in a browser.
func browserFetch(u string) error {
	go func() {
		resp, err := http.Get(u)
		if err == nil {
			resp.Body.Close()
		}
	}()
	return nil
}

func TestLoginPARFlow(t *testing.T) {
	idp := newFakeIdP(t)

	result, err := Login(context.Background(), LoginConfig{
		GuardianURL:  idp.URL,
		ClientID:     "svc_test",
		CallbackPort: 18085,
		Timeout:      10 * time.Second,
		OpenBrowser:  browserFetch,
	})
	require.NoError(t, err)
	assert.Equal(t, "test-access-token", result.AccessToken)
}

func TestLoginTimeout(t *testing.T) {
	idp := newFakeIdP(t)

	_, err := Login(context.Background(), LoginConfig{
		GuardianURL:  idp.URL,
		ClientID:     "svc_test",
		CallbackPort: 18086,
		Timeout:      200 * time.Millisecond,
		OpenBrowser:  func(string) error { return nil }, // never completes
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "timed out")
}

func TestLoginRequiresConfig(t *testing.T) {
	_, err := Login(context.Background(), LoginConfig{})
	assert.Error(t, err)
}

func TestLoginPARRejection(t *testing.T) {
	// IdP that rejects the PAR request (e.g. unregistered redirect_uri).
	mux := http.NewServeMux()
	mux.HandleFunc("/api/v1/oauth/par", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]string{
			"error":             "invalid_redirect_uri",
			"error_description": "redirect_uri does not match any registered pattern for this client",
		})
	})
	server := httptest.NewServer(mux)
	defer server.Close()

	_, err := Login(context.Background(), LoginConfig{
		GuardianURL:  server.URL,
		ClientID:     "svc_test",
		CallbackPort: 18087,
		Timeout:      5 * time.Second,
		OpenBrowser:  func(string) error { return nil },
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid_redirect_uri")
}

func TestLoginStateMismatchRejected(t *testing.T) {
	// IdP that redirects back with the wrong state.
	mux := http.NewServeMux()
	var server *httptest.Server
	mux.HandleFunc("/api/v1/oauth/par", func(w http.ResponseWriter, r *http.Request) {
		r.ParseForm()
		w.WriteHeader(http.StatusCreated)
		json.NewEncoder(w).Encode(map[string]any{"request_uri": "urn:guardian:par:x", "expires_in": 90})
	})
	mux.HandleFunc("/auth/login", func(w http.ResponseWriter, r *http.Request) {
		go func() {
			resp, err := http.Get("http://localhost:18088/auth/callback?token=x&state=wrong")
			if err == nil {
				resp.Body.Close()
			}
		}()
	})
	server = httptest.NewServer(mux)
	defer server.Close()

	_, err := Login(context.Background(), LoginConfig{
		GuardianURL:  server.URL,
		ClientID:     "svc_test",
		CallbackPort: 18088,
		Timeout:      5 * time.Second,
		OpenBrowser:  browserFetch,
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "state mismatch")
}

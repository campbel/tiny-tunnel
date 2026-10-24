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

// newFakeIdP implements Guardian's PAR + /oauth/authorize + /oauth/token flow:
// PAR stores the pushed parameters, /oauth/authorize consumes the single-use
// request_uri and PKCE challenge and redirects back to the stored redirect_uri
// with ?code=...&state=..., and /oauth/token verifies PKCE and issues the
// access_token.
func newFakeIdP(t *testing.T) *httptest.Server {
	t.Helper()

	type parEntry struct {
		clientID    string
		redirectURI string
		state       string
	}
	type codeEntry struct {
		clientID      string
		redirectURI   string
		codeChallenge string
	}
	parStore := map[string]parEntry{}
	codeStore := map[string]codeEntry{}

	mux := http.NewServeMux()
	var server *httptest.Server

	mux.HandleFunc("/api/v1/oauth/par", func(w http.ResponseWriter, r *http.Request) {
		require.NoError(t, r.ParseForm())
		assert.Equal(t, "svc_test", r.Form.Get("client_id"))
		redirectURI := r.Form.Get("redirect_uri")
		require.NotEmpty(t, redirectURI)

		requestURI := "urn:guardian:par:test-" + fmt.Sprint(len(parStore))
		parStore[requestURI] = parEntry{
			clientID:    r.Form.Get("client_id"),
			redirectURI: redirectURI,
			state:       r.Form.Get("state"),
		}

		w.WriteHeader(http.StatusCreated)
		json.NewEncoder(w).Encode(map[string]any{"request_uri": requestURI, "expires_in": 90})
	})

	mux.HandleFunc("/oauth/authorize", func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		assert.Equal(t, "svc_test", q.Get("client_id"))
		assert.Equal(t, "code", q.Get("response_type"))
		assert.Contains(t, q.Get("scope"), "openid")
		assert.Equal(t, "S256", q.Get("code_challenge_method"))
		require.NotEmpty(t, q.Get("code_challenge"))

		entry, ok := parStore[q.Get("request_uri")]
		require.True(t, ok, "unknown or reused request_uri")
		delete(parStore, q.Get("request_uri")) // single-use

		authCode := "code-" + fmt.Sprint(len(codeStore)+1)
		codeStore[authCode] = codeEntry{
			clientID:      entry.clientID,
			redirectURI:   entry.redirectURI,
			codeChallenge: q.Get("code_challenge"),
		}

		// Simulate a successful SSO: redirect back with code + state.
		redirect, err := url.Parse(entry.redirectURI)
		require.NoError(t, err)
		rq := redirect.Query()
		rq.Set("code", authCode)
		if entry.state != "" {
			rq.Set("state", entry.state)
		}
		redirect.RawQuery = rq.Encode()

		resp, err := http.Get(redirect.String())
		require.NoError(t, err)
		resp.Body.Close()
		assert.Equal(t, http.StatusOK, resp.StatusCode)
	})

	mux.HandleFunc("/oauth/token", func(w http.ResponseWriter, r *http.Request) {
		require.NoError(t, r.ParseForm())
		assert.Equal(t, "authorization_code", r.Form.Get("grant_type"))
		code := r.Form.Get("code")
		entry, ok := codeStore[code]
		if !ok {
			w.WriteHeader(http.StatusBadRequest)
			json.NewEncoder(w).Encode(map[string]string{"error": "invalid_grant", "error_description": "unknown code"})
			return
		}
		delete(codeStore, code) // single-use

		if r.Form.Get("client_id") != entry.clientID || r.Form.Get("redirect_uri") != entry.redirectURI {
			w.WriteHeader(http.StatusBadRequest)
			json.NewEncoder(w).Encode(map[string]string{"error": "invalid_grant", "error_description": "client_id or redirect_uri mismatch"})
			return
		}
		if S256Challenge(r.Form.Get("code_verifier")) != entry.codeChallenge {
			w.WriteHeader(http.StatusBadRequest)
			json.NewEncoder(w).Encode(map[string]string{"error": "invalid_grant", "error_description": "code_verifier mismatch"})
			return
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"access_token": "test-access-token",
			"token_type":   "Bearer",
			"expires_in":   3600,
		})
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
	mux.HandleFunc("/oauth/authorize", func(w http.ResponseWriter, r *http.Request) {
		go func() {
			resp, err := http.Get("http://localhost:18088/auth/callback?code=x&state=wrong")
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

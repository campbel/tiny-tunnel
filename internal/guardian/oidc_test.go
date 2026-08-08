package guardian

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
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

// fakeIdP implements the discovery, authorize, and token endpoints with
// mandatory PKCE S256 semantics matching Guardian's public-client model.
func newFakeIdP(t *testing.T) *httptest.Server {
	t.Helper()

	var storedChallenge string
	const authCode = "test-auth-code"

	mux := http.NewServeMux()
	var server *httptest.Server

	mux.HandleFunc("/.well-known/openid-configuration", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{
			"issuer":                 server.URL,
			"authorization_endpoint": server.URL + "/oauth/authorize",
			"token_endpoint":         server.URL + "/oauth/token",
			"jwks_uri":               server.URL + "/.well-known/jwks.json",
		})
	})

	mux.HandleFunc("/oauth/authorize", func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		assert.Equal(t, "code", q.Get("response_type"))
		assert.Equal(t, "svc_test", q.Get("client_id"))
		assert.Equal(t, "S256", q.Get("code_challenge_method"))
		require.NotEmpty(t, q.Get("code_challenge"))
		require.NotEmpty(t, q.Get("state"))
		storedChallenge = q.Get("code_challenge")

		// Simulate the user approving: redirect back with code + state.
		redirect, err := url.Parse(q.Get("redirect_uri"))
		require.NoError(t, err)
		rq := redirect.Query()
		rq.Set("code", authCode)
		rq.Set("state", q.Get("state"))
		redirect.RawQuery = rq.Encode()

		resp, err := http.Get(redirect.String())
		require.NoError(t, err)
		resp.Body.Close()
		assert.Equal(t, http.StatusOK, resp.StatusCode)
	})

	mux.HandleFunc("/oauth/token", func(w http.ResponseWriter, r *http.Request) {
		require.NoError(t, r.ParseForm())
		assert.Equal(t, "authorization_code", r.Form.Get("grant_type"))
		assert.Equal(t, authCode, r.Form.Get("code"))
		assert.Equal(t, "svc_test", r.Form.Get("client_id"))

		// PKCE check: BASE64URL(SHA256(verifier)) == stored challenge
		verifier := r.Form.Get("code_verifier")
		require.NotEmpty(t, verifier)
		sum := sha256.Sum256([]byte(verifier))
		if base64.RawURLEncoding.EncodeToString(sum[:]) != storedChallenge {
			w.WriteHeader(http.StatusBadRequest)
			json.NewEncoder(w).Encode(map[string]string{"error": "invalid_grant", "error_description": "code_verifier mismatch"})
			return
		}

		json.NewEncoder(w).Encode(map[string]any{
			"access_token":  "test-access-token",
			"id_token":      "test-id-token",
			"refresh_token": "test-refresh-token",
			"token_type":    "Bearer",
			"expires_in":    3600,
		})
	})

	server = httptest.NewServer(mux)
	t.Cleanup(server.Close)
	return server
}

func TestLoginPKCEFlow(t *testing.T) {
	idp := newFakeIdP(t)

	result, err := Login(context.Background(), LoginConfig{
		GuardianURL:  idp.URL,
		ClientID:     "svc_test",
		CallbackPort: 18085,
		Timeout:      10 * time.Second,
		// "Open the browser" by fetching the authorize URL; the fake IdP
		// immediately follows the redirect back to our callback listener.
		OpenBrowser: func(u string) error {
			go func() {
				resp, err := http.Get(u)
				if err == nil {
					resp.Body.Close()
				}
			}()
			return nil
		},
	})
	require.NoError(t, err)
	assert.Equal(t, "test-access-token", result.AccessToken)
	assert.Equal(t, "test-refresh-token", result.RefreshToken)
	assert.Equal(t, 3600, result.ExpiresIn)
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

func TestLoginStateMismatchRejected(t *testing.T) {
	// IdP that redirects back with the wrong state.
	mux := http.NewServeMux()
	var server *httptest.Server
	mux.HandleFunc("/.well-known/openid-configuration", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{
			"authorization_endpoint": server.URL + "/oauth/authorize",
			"token_endpoint":         server.URL + "/oauth/token",
		})
	})
	mux.HandleFunc("/oauth/authorize", func(w http.ResponseWriter, r *http.Request) {
		redirect := r.URL.Query().Get("redirect_uri")
		go func() {
			resp, err := http.Get(fmt.Sprintf("%s?code=x&state=wrong", redirect))
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
		CallbackPort: 18087,
		Timeout:      5 * time.Second,
		OpenBrowser: func(u string) error {
			go func() {
				resp, err := http.Get(u)
				if err == nil {
					resp.Body.Close()
				}
			}()
			return nil
		},
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "state mismatch")
}

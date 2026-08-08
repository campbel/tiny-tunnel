package server

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/campbel/tiny-tunnel/internal/log"
	"github.com/golang-jwt/jwt/v5"
	"github.com/gorilla/websocket"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// startFakeGuardian returns a fake Guardian (JWKS + keys/resolve) and a
// function to mint valid user access tokens.
func startFakeGuardian(t *testing.T) (*httptest.Server, func(claims jwt.MapClaims) string, string) {
	t.Helper()

	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	require.NoError(t, err)
	const kid = "k1"
	const apiKey = "dch_1_guardian_valid_crc"

	mux := http.NewServeMux()
	mux.HandleFunc("/.well-known/jwks.json", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{"keys": []map[string]any{{
			"kty": "OKP", "crv": "Ed25519", "kid": kid,
			"x": base64.RawURLEncoding.EncodeToString(pub),
		}}})
	})
	mux.HandleFunc("/api/v1/keys/resolve", func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer "+apiKey {
			w.WriteHeader(http.StatusUnauthorized)
			json.NewEncoder(w).Encode(map[string]string{"error": "invalid API key"})
			return
		}
		json.NewEncoder(w).Encode(map[string]string{"sub": "user-1", "type": "api_key", "key_name": "laptop"})
	})

	server := httptest.NewServer(mux)
	t.Cleanup(server.Close)

	mint := func(claims jwt.MapClaims) string {
		token := jwt.NewWithClaims(jwt.SigningMethodEdDSA, claims)
		token.Header["kid"] = kid
		signed, err := token.SignedString(priv)
		require.NoError(t, err)
		return signed
	}
	return server, mint, apiKey
}

func newAuthedServer(t *testing.T, guardianURL string) *httptest.Server {
	t.Helper()
	handler := NewHandler(Options{
		Hostname:         "example.com",
		EnableAuth:       true,
		GuardianURL:      guardianURL,
		GuardianAudience: "svc_tiny-tunnel_stable",
	}, log.NewTestLogger())
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	return server
}

func validClaims(iss string) jwt.MapClaims {
	return jwt.MapClaims{
		"iss":   iss,
		"aud":   []string{"svc_tiny-tunnel_stable"},
		"sub":   "user-1",
		"email": "ada@example.com",
		"type":  "access",
		"iat":   time.Now().Unix(),
		"exp":   time.Now().Add(time.Hour).Unix(),
	}
}

func TestRegisterRequiresGuardianCredential(t *testing.T) {
	guardian, mint, apiKey := startFakeGuardian(t)
	server := newAuthedServer(t, guardian.URL)

	wsURL := "ws" + server.URL[len("http"):] + "/register?name=test"

	t.Run("no token rejected", func(t *testing.T) {
		_, resp, err := websocket.DefaultDialer.Dial(wsURL, nil)
		require.Error(t, err)
		require.NotNil(t, resp)
		assert.Equal(t, http.StatusUnauthorized, resp.StatusCode)
	})

	t.Run("garbage token rejected", func(t *testing.T) {
		_, resp, err := websocket.DefaultDialer.Dial(wsURL, http.Header{"X-Auth-Token": {"garbage"}})
		require.Error(t, err)
		assert.Equal(t, http.StatusUnauthorized, resp.StatusCode)
	})

	t.Run("valid JWT accepted", func(t *testing.T) {
		conn, resp, err := websocket.DefaultDialer.Dial(wsURL, http.Header{"X-Auth-Token": {mint(validClaims(guardian.URL))}})
		require.NoError(t, err)
		assert.Equal(t, http.StatusSwitchingProtocols, resp.StatusCode)
		conn.Close()
	})

	t.Run("wrong audience rejected", func(t *testing.T) {
		claims := validClaims(guardian.URL)
		claims["aud"] = []string{"svc_other"}
		_, resp, err := websocket.DefaultDialer.Dial(wsURL, http.Header{"X-Auth-Token": {mint(claims)}})
		require.Error(t, err)
		assert.Equal(t, http.StatusUnauthorized, resp.StatusCode)
	})

	t.Run("valid API key accepted", func(t *testing.T) {
		conn, resp, err := websocket.DefaultDialer.Dial("ws"+server.URL[len("http"):]+"/register?name=test2", http.Header{"X-Auth-Token": {apiKey}})
		require.NoError(t, err)
		assert.Equal(t, http.StatusSwitchingProtocols, resp.StatusCode)
		conn.Close()
	})

	t.Run("invalid API key rejected", func(t *testing.T) {
		_, resp, err := websocket.DefaultDialer.Dial(wsURL, http.Header{"X-Auth-Token": {"dch_1_guardian_bogus_crc"}})
		require.Error(t, err)
		assert.Equal(t, http.StatusUnauthorized, resp.StatusCode)
	})
}

func TestAuthTestEndpoint(t *testing.T) {
	guardian, mint, apiKey := startFakeGuardian(t)
	server := newAuthedServer(t, guardian.URL)

	t.Run("JWT", func(t *testing.T) {
		req, _ := http.NewRequest("GET", server.URL+"/api/auth-test", nil)
		req.Header.Set("X-Auth-Token", mint(validClaims(guardian.URL)))
		resp, err := http.DefaultClient.Do(req)
		require.NoError(t, err)
		defer resp.Body.Close()
		require.Equal(t, http.StatusOK, resp.StatusCode)

		var body map[string]any
		require.NoError(t, json.NewDecoder(resp.Body).Decode(&body))
		assert.Equal(t, true, body["valid"])
		assert.Equal(t, "ada@example.com", body["email"])
		assert.Equal(t, "jwt", body["auth_method"])
		assert.NotEmpty(t, body["expires"])
	})

	t.Run("API key", func(t *testing.T) {
		req, _ := http.NewRequest("GET", server.URL+"/api/auth-test", nil)
		req.Header.Set("X-Auth-Token", apiKey)
		resp, err := http.DefaultClient.Do(req)
		require.NoError(t, err)
		defer resp.Body.Close()
		require.Equal(t, http.StatusOK, resp.StatusCode)

		var body map[string]any
		require.NoError(t, json.NewDecoder(resp.Body).Decode(&body))
		assert.Equal(t, true, body["valid"])
		assert.Equal(t, "api_key", body["auth_method"])
		assert.Equal(t, "user-1", body["sub"])
	})

	t.Run("unauthenticated", func(t *testing.T) {
		resp, err := http.Get(server.URL + "/api/auth-test")
		require.NoError(t, err)
		resp.Body.Close()
		assert.Equal(t, http.StatusUnauthorized, resp.StatusCode)
	})
}

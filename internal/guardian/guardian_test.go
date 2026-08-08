package guardian

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fakeGuardian serves a JWKS for a generated Ed25519 key and a keys/resolve
// endpoint that accepts a single known API key.
type fakeGuardian struct {
	server       *httptest.Server
	priv         ed25519.PrivateKey
	kid          string
	validAPIKey  string
	resolveCalls atomic.Int64
}

func newFakeGuardian(t *testing.T) *fakeGuardian {
	t.Helper()

	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	require.NoError(t, err)

	f := &fakeGuardian{
		priv:        priv,
		kid:         "test-key-1",
		validAPIKey: "dch_1_guardian_testkey_crc",
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/.well-known/jwks.json", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{
			"keys": []map[string]any{{
				"kty": "OKP",
				"crv": "Ed25519",
				"kid": f.kid,
				"x":   base64.RawURLEncoding.EncodeToString(pub),
			}},
		})
	})
	mux.HandleFunc("/api/v1/keys/resolve", func(w http.ResponseWriter, r *http.Request) {
		f.resolveCalls.Add(1)
		if r.Header.Get("Authorization") != "Bearer "+f.validAPIKey {
			w.WriteHeader(http.StatusUnauthorized)
			json.NewEncoder(w).Encode(map[string]string{"error": "invalid API key"})
			return
		}
		json.NewEncoder(w).Encode(map[string]string{
			"sub":      "user-123",
			"type":     "api_key",
			"key_id":   "key-row-1",
			"key_name": "ci-bot",
		})
	})

	f.server = httptest.NewServer(mux)
	t.Cleanup(f.server.Close)
	return f
}

func (f *fakeGuardian) mintToken(t *testing.T, claims jwt.MapClaims) string {
	t.Helper()
	token := jwt.NewWithClaims(jwt.SigningMethodEdDSA, claims)
	token.Header["kid"] = f.kid
	signed, err := token.SignedString(f.priv)
	require.NoError(t, err)
	return signed
}

func (f *fakeGuardian) defaultClaims() jwt.MapClaims {
	return jwt.MapClaims{
		"iss":   f.server.URL,
		"aud":   []string{"svc_tiny-tunnel_stable"},
		"sub":   "user-123",
		"email": "ada@example.com",
		"type":  "access",
		"iat":   time.Now().Unix(),
		"exp":   time.Now().Add(time.Hour).Unix(),
	}
}

func newTestVerifier(f *fakeGuardian) *Verifier {
	return NewVerifier(Config{URL: f.server.URL, Audience: "svc_tiny-tunnel_stable"})
}

func TestVerifyJWT(t *testing.T) {
	f := newFakeGuardian(t)
	v := newTestVerifier(f)

	identity, err := v.Verify(context.Background(), f.mintToken(t, f.defaultClaims()))
	require.NoError(t, err)
	assert.Equal(t, "user-123", identity.Sub)
	assert.Equal(t, "ada@example.com", identity.Email)
	assert.Equal(t, "jwt", identity.Method)
	assert.WithinDuration(t, time.Now().Add(time.Hour), identity.ExpiresAt, time.Minute)
}

func TestVerifyJWTBearerPrefixAccepted(t *testing.T) {
	f := newFakeGuardian(t)
	v := newTestVerifier(f)

	_, err := v.Verify(context.Background(), "Bearer "+f.mintToken(t, f.defaultClaims()))
	assert.NoError(t, err)
}

func TestVerifyJWTRejections(t *testing.T) {
	f := newFakeGuardian(t)

	tests := []struct {
		name   string
		mutate func(jwt.MapClaims)
	}{
		{"expired", func(c jwt.MapClaims) { c["exp"] = time.Now().Add(-time.Hour).Unix() }},
		{"wrong issuer", func(c jwt.MapClaims) { c["iss"] = "https://evil.example.com" }},
		{"wrong audience", func(c jwt.MapClaims) { c["aud"] = []string{"svc_other"} }},
		{"id_token not access", func(c jwt.MapClaims) { c["type"] = "id_token" }},
		{"refresh not access", func(c jwt.MapClaims) { c["type"] = "refresh" }},
		{"missing sub", func(c jwt.MapClaims) { delete(c, "sub") }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			v := newTestVerifier(f)
			claims := f.defaultClaims()
			tt.mutate(claims)
			_, err := v.Verify(context.Background(), f.mintToken(t, claims))
			assert.ErrorIs(t, err, ErrInvalidCredential)
		})
	}
}

func TestVerifyJWTWrongKey(t *testing.T) {
	f := newFakeGuardian(t)
	v := newTestVerifier(f)

	// Token signed by a different key but claiming the same kid
	_, otherPriv, err := ed25519.GenerateKey(rand.Reader)
	require.NoError(t, err)
	token := jwt.NewWithClaims(jwt.SigningMethodEdDSA, f.defaultClaims())
	token.Header["kid"] = f.kid
	signed, err := token.SignedString(otherPriv)
	require.NoError(t, err)

	_, err = v.Verify(context.Background(), signed)
	assert.ErrorIs(t, err, ErrInvalidCredential)
}

func TestVerifyJWTNoAudienceConfigSkipsCheck(t *testing.T) {
	f := newFakeGuardian(t)
	v := NewVerifier(Config{URL: f.server.URL}) // no audience configured

	claims := f.defaultClaims()
	claims["aud"] = []string{"svc_something_else"}
	_, err := v.Verify(context.Background(), f.mintToken(t, claims))
	assert.NoError(t, err)
}

func TestResolveAPIKey(t *testing.T) {
	f := newFakeGuardian(t)
	v := newTestVerifier(f)

	identity, err := v.Verify(context.Background(), f.validAPIKey)
	require.NoError(t, err)
	assert.Equal(t, "user-123", identity.Sub)
	assert.Equal(t, "api_key", identity.Method)
	assert.Equal(t, "ci-bot", identity.KeyName)

	// Second verify should hit the cache, not Guardian
	_, err = v.Verify(context.Background(), f.validAPIKey)
	require.NoError(t, err)
	assert.Equal(t, int64(1), f.resolveCalls.Load(), "second resolve should be cached")
}

func TestResolveAPIKeyInvalid(t *testing.T) {
	f := newFakeGuardian(t)
	v := newTestVerifier(f)

	_, err := v.Verify(context.Background(), "dch_1_guardian_bogus_crc")
	assert.ErrorIs(t, err, ErrInvalidCredential)
	// Invalid keys are not cached
	_, err = v.Verify(context.Background(), "dch_1_guardian_bogus_crc")
	assert.ErrorIs(t, err, ErrInvalidCredential)
	assert.Equal(t, int64(2), f.resolveCalls.Load())
}

func TestVerifyEmptyCredential(t *testing.T) {
	f := newFakeGuardian(t)
	v := newTestVerifier(f)

	_, err := v.Verify(context.Background(), "")
	assert.ErrorIs(t, err, ErrInvalidCredential)
	_, err = v.Verify(context.Background(), "   ")
	assert.ErrorIs(t, err, ErrInvalidCredential)
}

func TestIdentityString(t *testing.T) {
	assert.Equal(t, "ada@example.com", Identity{Sub: "u1", Email: "ada@example.com"}.String())
	assert.Equal(t, "u1 (key ci-bot)", Identity{Sub: "u1", KeyName: "ci-bot"}.String())
	assert.Equal(t, "u1", Identity{Sub: "u1"}.String())
}

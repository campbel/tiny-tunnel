// Package guardian authenticates callers against Dutchie's Guardian auth
// service. Two credential types are accepted:
//
//   - Guardian user JWTs (EdDSA / ES256), verified locally against Guardian's
//     JWKS. One JWKS fetch at startup (plus refresh on unknown key IDs) — no
//     per-request round-trip.
//   - Guardian API keys ("dch_..." prefix), resolved server-side via
//     GET /api/v1/keys/resolve. Resolutions are cached briefly to keep
//     reconnect storms from hammering Guardian.
package guardian

import (
	"context"
	"crypto/ecdsa"
	"crypto/ed25519"
	"crypto/elliptic"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

const (
	// APIKeyPrefix marks Guardian API keys, which must be resolved
	// server-side rather than verified locally.
	APIKeyPrefix = "dch_"

	resolveCacheTTL     = 60 * time.Second
	jwksMinFetchBackoff = 30 * time.Second
)

var ErrInvalidCredential = errors.New("invalid credential")

// Identity is the authenticated principal behind a credential.
type Identity struct {
	// Sub is the canonical Guardian user ID.
	Sub string
	// Email is the user's email (JWT path only; API keys don't carry it).
	Email string
	// Method is "jwt" or "api_key".
	Method string
	// KeyName is the human-readable API key label (API key path only).
	KeyName string
	// ExpiresAt is the credential expiry, when known (JWT path only).
	ExpiresAt time.Time
}

// String returns the best human-readable actor name.
func (i Identity) String() string {
	if i.Email != "" {
		return i.Email
	}
	if i.KeyName != "" {
		return fmt.Sprintf("%s (key %s)", i.Sub, i.KeyName)
	}
	return i.Sub
}

type Config struct {
	// URL is Guardian's base URL / issuer, e.g. https://id.stable.dexus.io
	URL string
	// Audience is the service client ID expected in the JWT aud claim,
	// e.g. svc_tiny-tunnel_stable. If empty, the audience check is skipped.
	Audience string
	// HTTPClient overrides the client used for JWKS + resolve calls.
	HTTPClient *http.Client
}

type Verifier struct {
	cfg  Config
	http *http.Client

	mu        sync.Mutex
	keys      map[string]any // kid -> ed25519.PublicKey | *ecdsa.PublicKey
	lastFetch time.Time

	cacheMu      sync.Mutex
	resolveCache map[string]resolveCacheEntry
}

type resolveCacheEntry struct {
	identity Identity
	expires  time.Time
}

func NewVerifier(cfg Config) *Verifier {
	httpClient := cfg.HTTPClient
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 10 * time.Second}
	}
	return &Verifier{
		cfg:          cfg,
		http:         httpClient,
		keys:         map[string]any{},
		resolveCache: map[string]resolveCacheEntry{},
	}
}

// Verify authenticates a credential (Guardian JWT or dch_ API key) and
// returns the identity behind it. Returns ErrInvalidCredential (possibly
// wrapped) for anything that should be treated as a 401.
func (v *Verifier) Verify(ctx context.Context, credential string) (Identity, error) {
	credential = strings.TrimSpace(strings.TrimPrefix(credential, "Bearer "))
	if credential == "" {
		return Identity{}, fmt.Errorf("%w: empty credential", ErrInvalidCredential)
	}
	if strings.HasPrefix(credential, APIKeyPrefix) {
		return v.resolveAPIKey(ctx, credential)
	}
	return v.verifyJWT(ctx, credential)
}

// --- JWT path ---

func (v *Verifier) verifyJWT(ctx context.Context, tokenString string) (Identity, error) {
	parser := jwt.NewParser(
		jwt.WithValidMethods([]string{"EdDSA", "ES256"}),
		jwt.WithIssuer(strings.TrimSuffix(v.cfg.URL, "/")),
		jwt.WithExpirationRequired(),
	)

	claims := jwt.MapClaims{}
	token, err := parser.ParseWithClaims(tokenString, claims, func(t *jwt.Token) (any, error) {
		kid, _ := t.Header["kid"].(string)
		return v.keyForKid(ctx, kid)
	})
	if err != nil {
		return Identity{}, fmt.Errorf("%w: %s", ErrInvalidCredential, err.Error())
	}
	if !token.Valid {
		return Identity{}, ErrInvalidCredential
	}

	// Guardian mints several token types from the same keys; only user
	// access tokens grant access here (not id_token, refresh, etc.).
	if typ, _ := claims["type"].(string); typ != "access" {
		return Identity{}, fmt.Errorf("%w: token type %q is not an access token", ErrInvalidCredential, claims["type"])
	}

	if v.cfg.Audience != "" {
		if !hasAudience(claims, v.cfg.Audience) {
			return Identity{}, fmt.Errorf("%w: token audience does not include %s", ErrInvalidCredential, v.cfg.Audience)
		}
	}

	identity := Identity{Method: "jwt"}
	identity.Sub, _ = claims["sub"].(string)
	identity.Email, _ = claims["email"].(string)
	if exp, err := claims.GetExpirationTime(); err == nil && exp != nil {
		identity.ExpiresAt = exp.Time
	}
	if identity.Sub == "" {
		return Identity{}, fmt.Errorf("%w: token has no sub claim", ErrInvalidCredential)
	}
	return identity, nil
}

func hasAudience(claims jwt.MapClaims, want string) bool {
	aud, err := claims.GetAudience()
	if err != nil {
		return false
	}
	for _, a := range aud {
		if a == want {
			return true
		}
	}
	return false
}

func (v *Verifier) keyForKid(ctx context.Context, kid string) (any, error) {
	v.mu.Lock()
	defer v.mu.Unlock()

	if key, ok := v.keys[kid]; ok {
		return key, nil
	}

	// Unknown kid: refresh the JWKS (rate-limited) to pick up rotations.
	if time.Since(v.lastFetch) < jwksMinFetchBackoff {
		return nil, fmt.Errorf("unknown key id %q", kid)
	}
	if err := v.fetchJWKSLocked(ctx); err != nil {
		return nil, err
	}
	if key, ok := v.keys[kid]; ok {
		return key, nil
	}
	return nil, fmt.Errorf("unknown key id %q", kid)
}

type jwks struct {
	Keys []jwk `json:"keys"`
}

type jwk struct {
	Kty string `json:"kty"`
	Kid string `json:"kid"`
	Crv string `json:"crv"`
	X   string `json:"x"`
	Y   string `json:"y"`
}

func (v *Verifier) fetchJWKSLocked(ctx context.Context) error {
	v.lastFetch = time.Now()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, strings.TrimSuffix(v.cfg.URL, "/")+"/.well-known/jwks.json", nil)
	if err != nil {
		return err
	}
	resp, err := v.http.Do(req)
	if err != nil {
		return fmt.Errorf("fetch jwks: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("fetch jwks: unexpected status %d", resp.StatusCode)
	}

	var doc jwks
	if err := json.NewDecoder(resp.Body).Decode(&doc); err != nil {
		return fmt.Errorf("decode jwks: %w", err)
	}

	keys := map[string]any{}
	for _, k := range doc.Keys {
		key, err := k.publicKey()
		if err != nil {
			// Skip unsupported key types rather than failing the whole set.
			continue
		}
		keys[k.Kid] = key
	}
	if len(keys) == 0 {
		return errors.New("jwks contains no usable keys")
	}
	v.keys = keys
	return nil
}

func (k jwk) publicKey() (any, error) {
	switch k.Kty {
	case "OKP": // Ed25519
		if k.Crv != "Ed25519" {
			return nil, fmt.Errorf("unsupported OKP curve %q", k.Crv)
		}
		x, err := base64.RawURLEncoding.DecodeString(k.X)
		if err != nil {
			return nil, err
		}
		if len(x) != ed25519.PublicKeySize {
			return nil, errors.New("invalid Ed25519 key length")
		}
		return ed25519.PublicKey(x), nil
	case "EC": // ES256
		if k.Crv != "P-256" {
			return nil, fmt.Errorf("unsupported EC curve %q", k.Crv)
		}
		xb, err := base64.RawURLEncoding.DecodeString(k.X)
		if err != nil {
			return nil, err
		}
		yb, err := base64.RawURLEncoding.DecodeString(k.Y)
		if err != nil {
			return nil, err
		}
		return &ecdsa.PublicKey{
			Curve: elliptic.P256(),
			X:     new(big.Int).SetBytes(xb),
			Y:     new(big.Int).SetBytes(yb),
		}, nil
	default:
		return nil, fmt.Errorf("unsupported key type %q", k.Kty)
	}
}

// --- API key path ---

func (v *Verifier) resolveAPIKey(ctx context.Context, key string) (Identity, error) {
	cacheKey := hashKey(key)

	v.cacheMu.Lock()
	if entry, ok := v.resolveCache[cacheKey]; ok && time.Now().Before(entry.expires) {
		v.cacheMu.Unlock()
		return entry.identity, nil
	}
	v.cacheMu.Unlock()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, strings.TrimSuffix(v.cfg.URL, "/")+"/api/v1/keys/resolve", nil)
	if err != nil {
		return Identity{}, err
	}
	req.Header.Set("Authorization", "Bearer "+key)

	resp, err := v.http.Do(req)
	if err != nil {
		return Identity{}, fmt.Errorf("resolve api key: %w", err)
	}
	defer resp.Body.Close()

	switch resp.StatusCode {
	case http.StatusOK:
		// fall through
	case http.StatusUnauthorized:
		return Identity{}, fmt.Errorf("%w: invalid or revoked API key", ErrInvalidCredential)
	default:
		return Identity{}, fmt.Errorf("guardian resolve: unexpected status %d", resp.StatusCode)
	}

	var out struct {
		Sub     string `json:"sub"`
		Type    string `json:"type"`
		KeyID   string `json:"key_id"`
		KeyName string `json:"key_name"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return Identity{}, fmt.Errorf("decode resolve response: %w", err)
	}
	if out.Sub == "" {
		return Identity{}, errors.New("guardian resolve: response has no sub")
	}

	identity := Identity{
		Sub:     out.Sub,
		Method:  "api_key",
		KeyName: out.KeyName,
	}

	v.cacheMu.Lock()
	v.resolveCache[cacheKey] = resolveCacheEntry{identity: identity, expires: time.Now().Add(resolveCacheTTL)}
	// Opportunistically drop expired entries so the cache can't grow unbounded.
	for k, e := range v.resolveCache {
		if time.Now().After(e.expires) {
			delete(v.resolveCache, k)
		}
	}
	v.cacheMu.Unlock()

	return identity, nil
}

func hashKey(key string) string {
	sum := sha256.Sum256([]byte(key))
	return hex.EncodeToString(sum[:])
}

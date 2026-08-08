// Package tunneltoken mints and verifies tiny-tunnel's own long-lived
// tunnel tokens. Users authenticate once with Guardian (browser SSO or
// device flow); the server then vends an Ed25519-signed JWT carrying the
// Guardian identity, so tunnel clients aren't bound to Guardian's 1h access
// token TTL and headless environments can hold a durable credential.
package tunneltoken

import (
	"crypto/ed25519"
	"encoding/base64"
	"errors"
	"fmt"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
)

const (
	// Issuer is the iss claim on all tnl-minted tokens.
	Issuer = "tiny-tunnel"
	// TokenType is the type claim on all tnl-minted tokens.
	TokenType = "tunnel"

	// DefaultTTL is how long vended tokens live unless configured otherwise.
	DefaultTTL = 30 * 24 * time.Hour
)

var ErrInvalidToken = errors.New("invalid tunnel token")

// Identity is the principal carried in a tunnel token.
type Identity struct {
	Sub       string
	Email     string
	ExpiresAt time.Time
}

func (i Identity) String() string {
	if i.Email != "" {
		return i.Email
	}
	return i.Sub
}

// Signer mints and verifies tunnel tokens with an Ed25519 keypair.
type Signer struct {
	priv ed25519.PrivateKey
	pub  ed25519.PublicKey
	ttl  time.Duration
}

// NewSigner builds a Signer from a base64-encoded Ed25519 seed (32 bytes),
// e.g. the value of TINY_TUNNEL_SIGNING_KEY. Generate one with:
//
//	openssl rand 32 | base64
func NewSigner(base64Seed string, ttl time.Duration) (*Signer, error) {
	seed, err := base64.StdEncoding.DecodeString(base64Seed)
	if err != nil {
		return nil, fmt.Errorf("signing key is not valid base64: %w", err)
	}
	if len(seed) != ed25519.SeedSize {
		return nil, fmt.Errorf("signing key must be %d bytes (got %d)", ed25519.SeedSize, len(seed))
	}
	priv := ed25519.NewKeyFromSeed(seed)
	if ttl == 0 {
		ttl = DefaultTTL
	}
	return &Signer{priv: priv, pub: priv.Public().(ed25519.PublicKey), ttl: ttl}, nil
}

// NewEphemeralSigner generates a random in-memory keypair. Tokens minted
// with it stop verifying when the process restarts — dev/test use only.
func NewEphemeralSigner(ttl time.Duration) (*Signer, error) {
	_, priv, err := ed25519.GenerateKey(nil)
	if err != nil {
		return nil, err
	}
	if ttl == 0 {
		ttl = DefaultTTL
	}
	return &Signer{priv: priv, pub: priv.Public().(ed25519.PublicKey), ttl: ttl}, nil
}

// Mint creates a signed tunnel token for the given identity.
func (s *Signer) Mint(sub, email string) (token string, expiresAt time.Time, err error) {
	expiresAt = time.Now().Add(s.ttl)
	claims := jwt.MapClaims{
		"iss":   Issuer,
		"sub":   sub,
		"email": email,
		"type":  TokenType,
		"jti":   uuid.New().String(),
		"iat":   time.Now().Unix(),
		"exp":   expiresAt.Unix(),
	}
	t := jwt.NewWithClaims(jwt.SigningMethodEdDSA, claims)
	signed, err := t.SignedString(s.priv)
	if err != nil {
		return "", time.Time{}, err
	}
	return signed, expiresAt, nil
}

// Verify checks a tunnel token and returns the identity behind it.
// Returns ErrInvalidToken (possibly wrapped) for anything 401-worthy.
func (s *Signer) Verify(token string) (Identity, error) {
	parser := jwt.NewParser(
		jwt.WithValidMethods([]string{"EdDSA"}),
		jwt.WithIssuer(Issuer),
		jwt.WithExpirationRequired(),
	)
	claims := jwt.MapClaims{}
	parsed, err := parser.ParseWithClaims(token, claims, func(t *jwt.Token) (any, error) {
		return s.pub, nil
	})
	if err != nil {
		return Identity{}, fmt.Errorf("%w: %s", ErrInvalidToken, err.Error())
	}
	if !parsed.Valid {
		return Identity{}, ErrInvalidToken
	}
	if typ, _ := claims["type"].(string); typ != TokenType {
		return Identity{}, fmt.Errorf("%w: unexpected token type %q", ErrInvalidToken, claims["type"])
	}
	identity := Identity{}
	identity.Sub, _ = claims["sub"].(string)
	identity.Email, _ = claims["email"].(string)
	if exp, err := claims.GetExpirationTime(); err == nil && exp != nil {
		identity.ExpiresAt = exp.Time
	}
	if identity.Sub == "" {
		return Identity{}, fmt.Errorf("%w: missing sub", ErrInvalidToken)
	}
	return identity, nil
}

// IsTunnelToken cheaply reports whether a JWT claims to be tnl-issued
// (unverified peek at the iss claim) so callers can route verification.
func IsTunnelToken(token string) bool {
	claims := jwt.MapClaims{}
	parser := jwt.NewParser()
	if _, _, err := parser.ParseUnverified(token, claims); err != nil {
		return false
	}
	iss, _ := claims["iss"].(string)
	return iss == Issuer
}

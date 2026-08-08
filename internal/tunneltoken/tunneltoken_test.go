package tunneltoken

import (
	"crypto/rand"
	"encoding/base64"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newSeed(t *testing.T) string {
	t.Helper()
	seed := make([]byte, 32)
	_, err := rand.Read(seed)
	require.NoError(t, err)
	return base64.StdEncoding.EncodeToString(seed)
}

func TestMintAndVerify(t *testing.T) {
	s, err := NewSigner(newSeed(t), time.Hour)
	require.NoError(t, err)

	token, expiresAt, err := s.Mint("user-1", "ada@example.com")
	require.NoError(t, err)
	assert.WithinDuration(t, time.Now().Add(time.Hour), expiresAt, time.Minute)

	identity, err := s.Verify(token)
	require.NoError(t, err)
	assert.Equal(t, "user-1", identity.Sub)
	assert.Equal(t, "ada@example.com", identity.Email)
	assert.WithinDuration(t, expiresAt, identity.ExpiresAt, time.Second)
	assert.True(t, IsTunnelToken(token))
}

func TestVerifyRejectsWrongKey(t *testing.T) {
	s1, err := NewSigner(newSeed(t), time.Hour)
	require.NoError(t, err)
	s2, err := NewSigner(newSeed(t), time.Hour)
	require.NoError(t, err)

	token, _, err := s1.Mint("user-1", "")
	require.NoError(t, err)

	_, err = s2.Verify(token)
	assert.ErrorIs(t, err, ErrInvalidToken)
}

func TestVerifyRejectsExpired(t *testing.T) {
	s, err := NewSigner(newSeed(t), -time.Hour) // already expired
	require.NoError(t, err)
	token, _, err := s.Mint("user-1", "")
	require.NoError(t, err)
	_, err = s.Verify(token)
	assert.ErrorIs(t, err, ErrInvalidToken)
}

func TestVerifyRejectsWrongType(t *testing.T) {
	s, err := NewSigner(newSeed(t), time.Hour)
	require.NoError(t, err)

	claims := jwt.MapClaims{
		"iss": Issuer, "sub": "user-1", "type": "access",
		"exp": time.Now().Add(time.Hour).Unix(),
	}
	tok := jwt.NewWithClaims(jwt.SigningMethodEdDSA, claims)
	signed, err := tok.SignedString(s.priv)
	require.NoError(t, err)

	_, err = s.Verify(signed)
	assert.ErrorIs(t, err, ErrInvalidToken)
}

func TestSameSeedVerifiesAcrossInstances(t *testing.T) {
	seed := newSeed(t)
	s1, err := NewSigner(seed, time.Hour)
	require.NoError(t, err)
	s2, err := NewSigner(seed, time.Hour)
	require.NoError(t, err)

	token, _, err := s1.Mint("user-1", "ada@example.com")
	require.NoError(t, err)
	_, err = s2.Verify(token)
	assert.NoError(t, err, "restart with same seed must keep tokens valid")
}

func TestNewSignerValidation(t *testing.T) {
	_, err := NewSigner("not-base64!!!", time.Hour)
	assert.Error(t, err)
	_, err = NewSigner(base64.StdEncoding.EncodeToString([]byte("short")), time.Hour)
	assert.Error(t, err)
}

func TestEphemeralSigner(t *testing.T) {
	s, err := NewEphemeralSigner(0)
	require.NoError(t, err)
	token, _, err := s.Mint("u", "e@x.com")
	require.NoError(t, err)
	_, err = s.Verify(token)
	assert.NoError(t, err)
}

func TestIsTunnelToken(t *testing.T) {
	assert.False(t, IsTunnelToken("dch_1_guardian_x_y"))
	assert.False(t, IsTunnelToken("garbage"))
	// Guardian-style token (different issuer)
	claims := jwt.MapClaims{"iss": "https://id.stable.dexus.io", "exp": time.Now().Add(time.Hour).Unix()}
	tok, _ := jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString([]byte("k"))
	assert.False(t, IsTunnelToken(tok))
}

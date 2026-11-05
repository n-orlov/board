package oidc

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"fmt"
)

// RandomToken returns n CSPRNG bytes as unpadded base64url, the form state,
// nonce and PKCE verifiers take.
func RandomToken(n int) (string, error) {
	raw := make([]byte, n)
	if _, err := rand.Read(raw); err != nil {
		return "", fmt.Errorf("read random bytes: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(raw), nil
}

// NewVerifier returns a PKCE code verifier: 32 random bytes, 43 characters.
func NewVerifier() (string, error) { return RandomToken(32) }

// Challenge is the S256 code challenge of verifier (RFC 7636).
func Challenge(verifier string) string {
	sum := sha256.Sum256([]byte(verifier))
	return base64.RawURLEncoding.EncodeToString(sum[:])
}

package oidc

import (
	"crypto"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"strings"
	"time"
)

// Errors Verify returns. ErrInvalidToken wraps every reason the token itself is
// refused (shape, algorithm, signature, issuer, audience, expiry), and
// ErrNonce is a token that is genuine but answers another login.
var (
	ErrInvalidToken = errors.New("invalid ID token")
	ErrNonce        = errors.New("ID token nonce does not match the login")
)

// clockSkew is how far past exp a token is still accepted.
const clockSkew = time.Minute

// Claims are the ID-token claims the board reads.
type Claims struct {
	Issuer        string   `json:"iss"`
	Subject       string   `json:"sub"`
	Audience      audience `json:"aud"`
	Expires       int64    `json:"exp"`
	Nonce         string   `json:"nonce"`
	Email         string   `json:"email"`
	EmailVerified flexBool `json:"email_verified"`
}

// audience is a JWT "aud": one string or a list.
type audience []string

func (a *audience) UnmarshalJSON(b []byte) error {
	var one string
	if json.Unmarshal(b, &one) == nil {
		*a = audience{one}
		return nil
	}
	var many []string
	if err := json.Unmarshal(b, &many); err != nil {
		return err
	}
	*a = many
	return nil
}

// flexBool is a JSON boolean that some providers send as the string "true".
type flexBool bool

func (f *flexBool) UnmarshalJSON(b []byte) error {
	switch string(b) {
	case "true", `"true"`:
		*f = true
	default:
		*f = false
	}
	return nil
}

type jwk struct {
	Kty string `json:"kty"`
	Kid string `json:"kid"`
	N   string `json:"n"`
	E   string `json:"e"`
}

// publicKey returns the RSA key of k, or false when k is not a usable one.
func (k jwk) publicKey() (*rsa.PublicKey, bool) {
	n, errN := base64.RawURLEncoding.DecodeString(k.N)
	e, errE := base64.RawURLEncoding.DecodeString(k.E)
	if k.Kty != "RSA" || errN != nil || errE != nil || len(n) == 0 || len(e) == 0 {
		return nil, false
	}
	return &rsa.PublicKey{N: new(big.Int).SetBytes(n), E: int(new(big.Int).SetBytes(e).Int64())}, true
}

// parsedToken is a compact JWS split into its parts.
type parsedToken struct {
	kid     string
	signed  string
	sig     []byte
	payload []byte
}

// parseToken splits raw and reads its header: only RS256 is accepted.
func parseToken(raw string) (parsedToken, error) {
	parts := strings.Split(raw, ".")
	if len(parts) != 3 {
		return parsedToken{}, fmt.Errorf("%w: not a compact JWS", ErrInvalidToken)
	}
	var header struct {
		Alg string `json:"alg"`
		Kid string `json:"kid"`
	}
	headerJSON, errH := base64.RawURLEncoding.DecodeString(parts[0])
	payload, errP := base64.RawURLEncoding.DecodeString(parts[1])
	sig, errS := base64.RawURLEncoding.DecodeString(parts[2])
	if err := errors.Join(errH, errP, errS, json.Unmarshal(headerJSON, &header)); err != nil {
		return parsedToken{}, fmt.Errorf("%w: unreadable", ErrInvalidToken)
	}
	if header.Alg != "RS256" {
		return parsedToken{}, fmt.Errorf("%w: algorithm %q is not RS256", ErrInvalidToken, header.Alg)
	}
	return parsedToken{kid: header.Kid, signed: parts[0] + "." + parts[1], sig: sig, payload: payload}, nil
}

// verifySignature checks t's RS256 signature under key.
func (t parsedToken) verifySignature(key *rsa.PublicKey) error {
	sum := sha256.Sum256([]byte(t.signed))
	if err := rsa.VerifyPKCS1v15(key, crypto.SHA256, sum[:], t.sig); err != nil {
		return fmt.Errorf("%w: bad signature", ErrInvalidToken)
	}
	return nil
}

// checkClaims decodes t's payload and checks issuer, audience and expiry.
func (t parsedToken) checkClaims(issuer, clientID string, now time.Time) (Claims, error) {
	var c Claims
	if err := json.Unmarshal(t.payload, &c); err != nil {
		return Claims{}, fmt.Errorf("%w: unreadable claims", ErrInvalidToken)
	}
	switch {
	case c.Issuer != issuer:
		return Claims{}, fmt.Errorf("%w: issuer %q", ErrInvalidToken, c.Issuer)
	case !contains(c.Audience, clientID):
		return Claims{}, fmt.Errorf("%w: audience does not name this client", ErrInvalidToken)
	case now.After(time.Unix(c.Expires, 0).Add(clockSkew)):
		return Claims{}, fmt.Errorf("%w: expired", ErrInvalidToken)
	}
	return c, nil
}

func contains(list []string, s string) bool {
	for _, v := range list {
		if v == s {
			return true
		}
	}
	return false
}

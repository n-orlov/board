package idempotency

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
)

// errSealed is a stored body that cannot be opened with the key it was
// sealed for.
var errSealed = errors.New("idempotency: stored answer cannot be read")

// aead is the cipher of r's answers: AES-256-GCM under a key derived from the
// principal and the idempotency key, neither of which is stored.
func aead(r Request) (cipher.AEAD, error) {
	sum := sha256.Sum256([]byte("vh-idempotency-body\x00" + r.Principal + "\x00" + r.Key))
	block, err := aes.NewCipher(sum[:])
	if err != nil {
		return nil, err
	}
	return cipher.NewGCM(block)
}

// seal encrypts body for storage: base64 of the nonce followed by the sealed
// text.
func seal(r Request, body []byte) (string, error) {
	g, err := aead(r)
	if err != nil {
		return "", err
	}
	nonce := make([]byte, g.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return "", err
	}
	return base64.StdEncoding.EncodeToString(g.Seal(nonce, nonce, body, nil)), nil
}

// open decrypts what seal stored.
func open(r Request, stored string) ([]byte, error) {
	raw, err := base64.StdEncoding.DecodeString(stored)
	if err != nil {
		return nil, errSealed
	}
	g, err := aead(r)
	if err != nil {
		return nil, err
	}
	if len(raw) < g.NonceSize() {
		return nil, errSealed
	}
	plain, err := g.Open(nil, raw[:g.NonceSize()], raw[g.NonceSize():], nil)
	if err != nil {
		return nil, errSealed
	}
	return plain, nil
}

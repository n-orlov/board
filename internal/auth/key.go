package auth

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base32"
	"encoding/hex"
	"fmt"
	"strings"
)

const (
	// KeyPrefix starts every API key, so secret scanners recognise one.
	KeyPrefix = "brd_"
	// KeyBytes is the number of CSPRNG bytes in a key.
	KeyBytes = 32
	// KeyBodyLength is the length of the base32 text after KeyPrefix.
	KeyBodyLength = 52 // ceil(32 * 8 / 5)
	// DisplayPrefixLength is how many characters after KeyPrefix a row keeps.
	DisplayPrefixLength = 8

	crockfordLower = "0123456789abcdefghjkmnpqrstvwxyz"
)

var encoding = base32.NewEncoding(crockfordLower).WithPadding(base32.NoPadding)

// NewKey returns a new API key: KeyPrefix and KeyBytes bytes from the OS
// CSPRNG in lowercase Crockford base32.
func NewKey() (string, error) {
	var raw [KeyBytes]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return "", fmt.Errorf("read random bytes: %w", err)
	}
	return KeyPrefix + encoding.EncodeToString(raw[:]), nil
}

// HashKey is the stored form of a key: the lowercase hex SHA-256 of the full
// key text. It is the only form of the key the database keeps.
func HashKey(key string) string {
	sum := sha256.Sum256([]byte(key))
	return hex.EncodeToString(sum[:])
}

// DisplayPrefix is the first DisplayPrefixLength characters after KeyPrefix,
// which a row keeps and a listing shows to tell keys apart.
func DisplayPrefix(key string) string {
	body := strings.TrimPrefix(key, KeyPrefix)
	if len(body) > DisplayPrefixLength {
		body = body[:DisplayPrefixLength]
	}
	return body
}

// WellFormed reports whether key has the shape NewKey produces.
func WellFormed(key string) bool {
	body, ok := strings.CutPrefix(key, KeyPrefix)
	if !ok || len(body) != KeyBodyLength {
		return false
	}
	for i := 0; i < len(body); i++ {
		if strings.IndexByte(crockfordLower, body[i]) < 0 {
			return false
		}
	}
	return true
}

package domain

import (
	"encoding/binary"
	"crypto/rand"
	"fmt"
	"time"
)

// ulidAlphabet is Crockford base32, uppercase, so ULIDs sort as text.
const ulidAlphabet = "0123456789ABCDEFGHJKMNPQRSTVWXYZ"

// ULIDLength is the length of a ULID's text form.
const ULIDLength = 26

// NewULID returns a ULID for the instant t: 48 bits of Unix milliseconds
// followed by 80 random bits from the OS CSPRNG, in 26 Crockford base32
// characters. ULIDs made at later milliseconds sort after earlier ones.
func NewULID(t time.Time) (string, error) {
	var b [16]byte
	var ms [8]byte
	binary.BigEndian.PutUint64(ms[:], uint64(t.UnixMilli()))
	copy(b[:6], ms[2:])
	if _, err := rand.Read(b[6:]); err != nil {
		return "", fmt.Errorf("read random bytes: %w", err)
	}
	return encodeULID(b), nil
}

// encodeULID writes 128 bits as 26 five-bit groups, the first group holding
// the two top bits.
func encodeULID(b [16]byte) string {
	out := make([]byte, ULIDLength)
	var acc uint16
	bits, pos := 2, 0 // 130 = 2 padding bits + 128
	for _, by := range b {
		acc = acc<<8 | uint16(by)
		bits += 8
		for bits >= 5 {
			bits -= 5
			out[pos] = ulidAlphabet[(acc>>bits)&31]
			pos++
		}
	}
	return string(out)
}

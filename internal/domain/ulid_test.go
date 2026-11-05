package domain

import (
	"strings"
	"testing"
	"time"
)

func TestNewULIDHasTheCanonicalShape(t *testing.T) {
	id, err := NewULID(time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if len(id) != ULIDLength {
		t.Fatalf("length %d, want %d", len(id), ULIDLength)
	}
	for _, c := range id {
		if !strings.ContainsRune(ulidAlphabet, c) {
			t.Fatalf("%q holds %q outside the Crockford alphabet", id, c)
		}
	}
	if id[0] > '7' {
		t.Fatalf("%q overflows 128 bits", id)
	}
}

func TestNewULIDEncodesTheTimestampInTheFirstTenCharacters(t *testing.T) {
	// 2^48-1 ms is the largest timestamp, the canonical 7ZZZZZZZZZ.
	var b [16]byte
	for i := 0; i < 6; i++ {
		b[i] = 0xff
	}
	if got := encodeULID(b); got != "7ZZZZZZZZZ"+"0000000000000000" {
		t.Fatalf("max timestamp encodes as %q", got)
	}
	var one [16]byte
	one[15] = 1
	if got := encodeULID(one); got != strings.Repeat("0", 25)+"1" {
		t.Fatalf("lowest bit encodes as %q", got)
	}
	var top [16]byte
	top[0] = 0x80
	if got := encodeULID(top); got != "4"+strings.Repeat("0", 25) {
		t.Fatalf("highest bit encodes as %q", got)
	}
}

func TestNewULIDSortsByTimeAndDiffersWithinAMillisecond(t *testing.T) {
	t0 := time.Date(2026, 11, 5, 12, 0, 0, 0, time.UTC)
	a, _ := NewULID(t0)
	b, _ := NewULID(t0.Add(time.Millisecond))
	c, _ := NewULID(t0)
	if a >= b {
		t.Fatalf("%q does not sort before %q", a, b)
	}
	if a[:10] != c[:10] || a == c {
		t.Fatalf("same millisecond: %q and %q should share a time prefix and differ in the random tail", a, c)
	}
}

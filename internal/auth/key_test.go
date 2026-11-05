package auth

import (
	"regexp"
	"strings"
	"testing"
)

var keyShape = regexp.MustCompile(`^brd_[0-9a-hjkmnp-tv-z]{52}$`)

func TestNewKeyHasThePrefixAnd32BytesOfLowercaseCrockfordBase32(t *testing.T) {
	key, err := NewKey()
	if err != nil {
		t.Fatal(err)
	}
	if !keyShape.MatchString(key) {
		t.Fatalf("key %q does not match brd_ + 52 lowercase Crockford base32 characters", key)
	}
	if !WellFormed(key) {
		t.Fatalf("WellFormed rejects %q", key)
	}
	if decoded, err := encoding.DecodeString(strings.TrimPrefix(key, KeyPrefix)); err != nil || len(decoded) != KeyBytes {
		t.Fatalf("body decodes to %d bytes (err %v), want %d", len(decoded), err, KeyBytes)
	}
}

func TestNewKeyIsDifferentEveryTime(t *testing.T) {
	seen := map[string]bool{}
	for i := 0; i < 50; i++ {
		key, err := NewKey()
		if err != nil {
			t.Fatal(err)
		}
		if seen[key] {
			t.Fatalf("key %q repeated", key)
		}
		seen[key] = true
	}
}

func TestHashKeyIsTheHexSHA256OfTheWholeKey(t *testing.T) {
	// printf 'brd_abc' | sha256sum
	const want = "dc188b1c97321ea7c8b13b8b53057ee0016214377eb19ec4e125f91ec33f0297"
	if got := HashKey("brd_abc"); got != want {
		t.Fatalf("HashKey = %q, want %q", got, want)
	}
}

func TestDisplayPrefixIsTheFirstEightCharactersAfterThePrefix(t *testing.T) {
	if got := DisplayPrefix("brd_0123456789abcdef"); got != "01234567" {
		t.Fatalf("prefix %q", got)
	}
	if got := DisplayPrefix("brd_abc"); got != "abc" {
		t.Fatalf("short key prefix %q", got)
	}
}

func TestWellFormedRejectsWhatNewKeyNeverMakes(t *testing.T) {
	good, _ := NewKey()
	for name, key := range map[string]string{
		"empty":        "",
		"no prefix":    strings.TrimPrefix(good, KeyPrefix),
		"too short":    good[:len(good)-1],
		"too long":     good + "0",
		"uppercase":    strings.ToUpper(good),
		"excluded 'i'": good[:4] + "i" + good[5:],
		"excluded 'u'": good[:4] + "u" + good[5:],
	} {
		if WellFormed(key) {
			t.Errorf("%s: WellFormed accepts %q", name, key)
		}
	}
}

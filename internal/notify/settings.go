package notify

import (
	"fmt"
	"strings"
	"time"
)

// DefaultTimeout is how long one POST may take when VH_NOTIFY_TIMEOUT is unset.
const DefaultTimeout = 10 * time.Second

// ParseTimeout reads VH_NOTIFY_TIMEOUT: empty is DefaultTimeout and anything
// else must be a Go duration above zero.
func ParseTimeout(v string) (time.Duration, error) {
	v = strings.TrimSpace(v)
	if v == "" {
		return DefaultTimeout, nil
	}
	d, err := time.ParseDuration(v)
	if err != nil || d <= 0 {
		return 0, fmt.Errorf("VH_NOTIFY_TIMEOUT=%q: want a duration above zero such as 10s", v)
	}
	return d, nil
}

// Header is the optional header sent with every POST.
type Header struct{ Name, Value string }

// ParseHeader reads VH_NOTIFY_HEADER: empty means no header, and otherwise
// `Name: value` with a valid header name and a value free of control
// characters. An error never repeats the value, which may be a secret.
func ParseHeader(v string) (Header, error) {
	if strings.TrimSpace(v) == "" {
		return Header{}, nil
	}
	name, value, ok := strings.Cut(v, ":")
	name, value = strings.TrimSpace(name), strings.TrimSpace(value)
	if !ok || !validName(name) || value == "" || hasControl(value) {
		return Header{}, fmt.Errorf("VH_NOTIFY_HEADER: want `Name: value` with a header name and a value on one line")
	}
	return Header{Name: name, Value: value}, nil
}

// tokenChars are the characters of a header name (RFC 9110 token).
const tokenChars = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789!#$%&'*+-.^_`|~"

func validName(name string) bool {
	if name == "" {
		return false
	}
	for _, c := range name {
		if !strings.ContainsRune(tokenChars, c) {
			return false
		}
	}
	return true
}

func hasControl(s string) bool {
	for _, c := range s {
		if c < 0x20 && c != '\t' || c == 0x7f {
			return true
		}
	}
	return false
}

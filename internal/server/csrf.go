package server

import (
	"crypto/subtle"
	"net/http"
	"net/url"
	"strings"
)

// CSRFHeader carries the session's CSRF token on a cookie-authenticated write.
const CSRFHeader = "X-CSRF-Token"

// safeMethod reports whether a request of method changes nothing, so needs no
// CSRF check.
func safeMethod(method string) bool {
	return method == http.MethodGet || method == http.MethodHead || method == http.MethodOptions
}

// originOf returns scheme://host of raw, lower-cased, or "" when raw is not an
// absolute URL.
func originOf(raw string) string {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || u.Scheme == "" || u.Host == "" {
		return ""
	}
	return strings.ToLower(u.Scheme + "://" + u.Host)
}

// csrfProblem returns why a cookie-authenticated request must be refused, or
// "" when it passes (SPEC 3.2): a write needs the session's CSRF token in
// X-CSRF-Token and an Origin, or failing that a Referer, on the public base URL.
func (l *Login) csrfProblem(r *http.Request, token string) string {
	if safeMethod(r.Method) {
		return ""
	}
	got := r.Header.Get(CSRFHeader)
	if got == "" || token == "" || subtle.ConstantTimeCompare([]byte(got), []byte(token)) != 1 {
		return "a write authenticated by a session cookie needs the session's " + CSRFHeader + " header"
	}
	from := r.Header.Get("Origin")
	if from == "" {
		from = r.Header.Get("Referer")
	}
	want := originOf(l.PublicURL)
	if want == "" || originOf(from) != want {
		return "a write authenticated by a session cookie needs an Origin (or Referer) equal to the public base URL"
	}
	return ""
}

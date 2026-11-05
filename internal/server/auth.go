package server

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"github.com/n-orlov/board/internal/auth"
)

// KeyVerifier checks a presented API key and returns the key's name. It
// returns auth.ErrKeyUnknown, auth.ErrKeyRevoked or auth.ErrKeyExpired for a
// key it refused, and any other error when it could not check.
type KeyVerifier func(ctx context.Context, key string) (string, error)

// ActorHeader names the actor a key-authenticated request acts as.
const ActorHeader = "Board-Actor"

const problemType = "about:blank"

// writeProblem writes an RFC 9457 problem document.
func writeProblem(w http.ResponseWriter, status int, detail string) {
	w.Header().Set("Content-Type", "application/problem+json")
	if status == http.StatusUnauthorized {
		w.Header().Set("WWW-Authenticate", "Bearer")
	}
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]any{
		"type":   problemType,
		"title":  http.StatusText(status),
		"status": status,
		"detail": detail,
	})
}

// bearerKey returns the key of an "Authorization: Bearer <key>" header.
func bearerKey(r *http.Request) (string, bool) {
	scheme, key, ok := strings.Cut(r.Header.Get("Authorization"), " ")
	if !ok || !strings.EqualFold(scheme, "Bearer") || strings.TrimSpace(key) == "" {
		return "", false
	}
	return strings.TrimSpace(key), true
}

// authenticated wraps next so that every request other than GET /healthz must
// carry a valid Bearer key and a Board-Actor header. A request with no
// credential is refused before verify, and so before the store, is called.
func authenticated(next http.Handler, verify KeyVerifier) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet && r.URL.Path == "/healthz" {
			next.ServeHTTP(w, r)
			return
		}
		key, ok := bearerKey(r)
		if !ok {
			writeProblem(w, http.StatusUnauthorized, "authentication required")
			return
		}
		if status, detail := checkKey(r, verify, key); status != 0 {
			writeProblem(w, status, detail)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// checkKey returns 0 when key is valid and the request names its actor, and
// otherwise the status and problem detail to answer with.
func checkKey(r *http.Request, verify KeyVerifier, key string) (int, string) {
	_, err := verify(r.Context(), key)
	switch {
	case errors.Is(err, auth.ErrKeyRevoked), errors.Is(err, auth.ErrKeyExpired), errors.Is(err, auth.ErrKeyUnknown):
		return http.StatusUnauthorized, err.Error()
	case err != nil:
		return http.StatusServiceUnavailable, "the key could not be checked"
	case strings.TrimSpace(r.Header.Get(ActorHeader)) == "":
		return http.StatusBadRequest, ActorHeader + " header is required"
	}
	return 0, ""
}

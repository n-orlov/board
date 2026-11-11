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

// WayBackHeader and MetaHeader carry the optional way back and the free-form
// JSON object of the actor a key-authenticated request acts as.
const (
	WayBackHeader = "Board-Actor-Way-Back"
	MetaHeader    = "Board-Actor-Meta"
)

// OnBehalfHeader names the actor a key-authenticated write is made for, such as
// `operator` when an agent closes a task on instruction.
const OnBehalfHeader = "Board-On-Behalf-Of"

const problemType = "about:blank"

// defaultNext is the suggested next action of an error that names none of its own.
func defaultNext(status int) string {
	switch {
	case status == http.StatusUnauthorized:
		return "Send an API key as 'Authorization: Bearer <key>' and your handle as 'Board-Actor' (create a key with 'board admin keys create <name>')."
	case status == http.StatusNotFound:
		return "Check the path and the ref, then try again."
	case status == http.StatusConflict:
		return "Read what exists, or choose another name."
	case status >= http.StatusInternalServerError:
		return "Try again shortly. If it keeps failing, check the server's log."
	}
	return "Fix the request and send it again."
}

// writeProblem writes an RFC 9457 problem document with the default next action
// of its status.
func writeProblem(w http.ResponseWriter, status int, detail string) {
	writeProblemNext(w, status, detail, "")
}

// writeProblemNext writes an RFC 9457 problem document whose `next` field is
// next, or the default for the status when next is empty.
func writeProblemNext(w http.ResponseWriter, status int, detail, next string) {
	writeProblemExtra(w, status, detail, next, nil)
}

// writeProblemExtra is writeProblemNext with the members of extra added to the
// document, such as the `expired_at` of a lease that cannot be renewed.
func writeProblemExtra(w http.ResponseWriter, status int, detail, next string, extra map[string]any) {
	if next == "" {
		next = defaultNext(status)
	}
	w.Header().Set("Content-Type", "application/problem+json")
	if status == http.StatusUnauthorized {
		w.Header().Set("WWW-Authenticate", "Bearer")
	}
	w.WriteHeader(status)
	doc := map[string]any{
		"type":   problemType,
		"title":  http.StatusText(status),
		"status": status,
		"detail": detail,
		"next":   next,
	}
	for k, v := range extra {
		doc[k] = v
	}
	_ = json.NewEncoder(w).Encode(doc)
}

// bearerKey returns the key of an "Authorization: Bearer <key>" header.
func bearerKey(r *http.Request) (string, bool) {
	scheme, key, ok := strings.Cut(r.Header.Get("Authorization"), " ")
	if !ok || !strings.EqualFold(scheme, "Bearer") || strings.TrimSpace(key) == "" {
		return "", false
	}
	return strings.TrimSpace(key), true
}

// publicAssets are the files the browser fetches without the session cookie: the
// PWA manifest (a manifest link fetches without credentials unless told
// otherwise) and the icons it and the page name. They carry no data.
var publicAssets = map[string]bool{
	"/manifest.json":        true,
	"/icon-192.png":         true,
	"/icon-512.png":         true,
	"/icon.svg":             true,
	"/apple-touch-icon.png": true,
	"/favicon.svg":          true,
}

// isPublic reports whether r needs no credential: GET /healthz, or a GET or
// HEAD of one of publicAssets.
func isPublic(r *http.Request) bool {
	if r.Method == http.MethodGet && r.URL.Path == "/healthz" {
		return true
	}
	return (r.Method == http.MethodGet || r.Method == http.MethodHead) && publicAssets[r.URL.Path]
}

// authenticated wraps next so that every request other than GET /healthz and
// the publicAssets is authenticated: by a Bearer key that verify accepts plus a
// Board-Actor header, or, when login is on, by a live session cookie. A request with no credential
// is refused before verify, and so before the store, is called: with 401, or,
// when login is on and the request is a browser navigation, by a redirect to
// /auth/login (which, like the callback, then answers the navigation).
func authenticated(next http.Handler, verify KeyVerifier, login *Login) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if isPublic(r) {
			next.ServeHTTP(w, r)
			return
		}
		if key, ok := bearerKey(r); ok {
			serveKeyed(next, verify, key, w, r)
			return
		}
		if login == nil {
			writeProblem(w, http.StatusUnauthorized, "authentication required")
			return
		}
		serveBrowser(next, login, w, r)
	})
}

// serveKeyed serves r when key is valid and the request names its actor.
func serveKeyed(next http.Handler, verify KeyVerifier, key string, w http.ResponseWriter, r *http.Request) {
	ctx, usage := auth.WithDeferredUsage(r.Context())
	r = r.WithContext(ctx)
	name, status, detail := checkKey(r, verify, key)
	if status != 0 {
		writeProblem(w, status, detail)
		return
	}
	who := auth.Identity{
		Principal:  "key:" + name,
		Actor:      strings.TrimSpace(r.Header.Get(ActorHeader)),
		WayBack:    strings.TrimSpace(r.Header.Get(WayBackHeader)),
		Meta:       strings.TrimSpace(r.Header.Get(MetaHeader)),
		OnBehalfOf: strings.TrimSpace(r.Header.Get(OnBehalfHeader)),
	}
	next.ServeHTTP(&usageWriter{ResponseWriter: w, usage: usage, ctx: r.Context()}, r.WithContext(withIdentity(r.Context(), who)))
}

// usageWriter commits the writes authentication deferred (a key's
// last_used_at, a session's last_seen_at or removal) just before the first
// success status goes out. A refused request (status 400 and up) never
// commits them, so it leaves every row as it was, and the next successful
// request still makes them.
type usageWriter struct {
	http.ResponseWriter
	usage *auth.Usage
	ctx   context.Context
	sent  bool
}

func (u *usageWriter) WriteHeader(status int) {
	if !u.sent && status >= 200 && status < 400 {
		u.usage.Commit(u.ctx)
	}
	if status >= 200 {
		u.sent = true
	}
	u.ResponseWriter.WriteHeader(status)
}

func (u *usageWriter) Write(b []byte) (int, error) {
	if !u.sent {
		u.WriteHeader(http.StatusOK)
	}
	return u.ResponseWriter.Write(b)
}

// Unwrap lets http.ResponseController reach the underlying writer.
func (u *usageWriter) Unwrap() http.ResponseWriter { return u.ResponseWriter }

// serveBrowser serves r on the strength of its session cookie, or answers a
// request that has none. The session writes the lookup makes (its last-seen
// time, the removal of a dead session) commit only with a response below 400,
// like a key's usage bump.
func serveBrowser(next http.Handler, login *Login, w http.ResponseWriter, r *http.Request) {
	ctx, usage := auth.WithDeferredUsage(r.Context())
	r = r.WithContext(ctx)
	w = &usageWriter{ResponseWriter: w, usage: usage, ctx: ctx}
	id, ok := sessionCookie(r)
	if !ok {
		serveAnonymous(next, w, r)
		return
	}
	sess, err := login.Sessions.Lookup(r.Context(), id)
	switch {
	case errors.Is(err, auth.ErrNoSession):
		serveAnonymous(next, w, r)
	case err != nil:
		writeProblem(w, http.StatusServiceUnavailable, "the session could not be checked")
	default:
		if detail := login.csrfProblem(r, sess.CSRFToken); detail != "" {
			writeProblem(w, http.StatusForbidden, detail)
			return
		}
		ctx := withSession(r.Context(), sessionIdentity{id: id, session: sess})
		who := auth.Identity{Principal: "oidc:" + sess.Email, Actor: login.Operator, Operator: true}
		next.ServeHTTP(w, r.WithContext(withIdentity(ctx, who)))
	}
}

// serveAnonymous answers a request that carries no credential. Only a browser
// navigation gets past 401: the login and callback pages are served, and any
// other page is a redirect to /auth/login.
func serveAnonymous(next http.Handler, w http.ResponseWriter, r *http.Request) {
	switch {
	case !isNavigation(r):
		writeProblem(w, http.StatusUnauthorized, "authentication required")
	case r.URL.Path == loginPath || r.URL.Path == callbackPath:
		next.ServeHTTP(w, r)
	default:
		http.Redirect(w, r, loginPath, http.StatusFound)
	}
}

// checkKey returns the key's name and 0 when key is valid and the request
// names its actor, and otherwise the status and problem detail to answer with.
func checkKey(r *http.Request, verify KeyVerifier, key string) (string, int, string) {
	name, err := verify(r.Context(), key)
	switch {
	case errors.Is(err, auth.ErrKeyRevoked), errors.Is(err, auth.ErrKeyExpired), errors.Is(err, auth.ErrKeyUnknown):
		return "", http.StatusUnauthorized, err.Error()
	case err != nil:
		return "", http.StatusServiceUnavailable, "the key could not be checked"
	case strings.TrimSpace(r.Header.Get(ActorHeader)) == "":
		return "", http.StatusBadRequest, ActorHeader + " header is required"
	case !validMeta(r.Header.Get(MetaHeader)):
		return "", http.StatusBadRequest, MetaHeader + " header must be a JSON object"
	}
	return name, 0, ""
}

// validMeta reports whether a Board-Actor-Meta header value is absent or a JSON object.
func validMeta(v string) bool {
	v = strings.TrimSpace(v)
	if v == "" {
		return true
	}
	var obj map[string]any
	return json.Unmarshal([]byte(v), &obj) == nil && obj != nil
}

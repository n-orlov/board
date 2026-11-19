package server

import (
	"context"
	"crypto/subtle"
	"errors"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/n-orlov/village-hall/internal/auth"
	"github.com/n-orlov/village-hall/internal/oidc"
)

// Cookie names and paths. The session cookie is HttpOnly, Secure and
// SameSite=Lax; the pre-login cookie carries the id of one login in progress
// and lives for loginTTL.
const (
	SessionCookie = "board_session"
	loginCookie   = "board_login"
	loginTTL      = 10 * time.Minute
	// maxPending bounds the logins in progress, so an unauthenticated caller
	// cannot grow the table without limit.
	maxPending = 10000

	// callbackPath is fixed (SPEC 3.2): it is the redirect URI registered
	// with the identity provider.
	callbackPath = "/auth/oidc/callback"
	loginPath    = "/auth/login"
)

// Identity is the identity-provider side of a login. *oidc.Provider is the
// production one.
type Identity interface {
	AuthURL(ctx context.Context, state, nonce, verifier string) (string, error)
	Exchange(ctx context.Context, code, verifier string) (string, error)
	Verify(ctx context.Context, raw, wantNonce string) (oidc.Claims, error)
}

// Login is the browser login: the provider, the allowed emails, the session
// store and the logins in progress. A nil *Login means browser login is off.
type Login struct {
	Identity Identity
	Sessions auth.Sessions
	// Allowed are the lower-case emails that log in as the operator.
	Allowed map[string]bool
	// PublicURL is the external base URL: a cookie-authenticated write must
	// come from its origin (SPEC 3.2).
	PublicURL string
	// Operator is the handle of the operator actor a session acts as when its
	// email has no entry in Operators (VH_OPERATOR_HANDLE).
	Operator string
	// Operators maps a lower-case admitted email to the operator handle its
	// sessions act as (VH_OIDC_OPERATORS). Nil or empty maps nothing.
	Operators map[string]string
	// Now is the clock; nil means time.Now.
	Now func() time.Time

	mu      sync.Mutex
	pending map[string]pendingLogin
}

type pendingLogin struct {
	state, nonce, verifier string
	expires                time.Time
}

func (l *Login) now() time.Time {
	if l.Now != nil {
		return l.Now()
	}
	return time.Now()
}

// begin records a login in progress and returns its id.
func (l *Login) begin(p pendingLogin) (string, error) {
	id, err := oidc.RandomToken(24)
	if err != nil {
		return "", err
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.pending == nil {
		l.pending = map[string]pendingLogin{}
	}
	now := l.now()
	if len(l.pending) >= maxPending {
		for k, v := range l.pending {
			if now.After(v.expires) {
				delete(l.pending, k)
			}
		}
	}
	if len(l.pending) >= maxPending {
		return "", errors.New("too many logins in progress")
	}
	p.expires = now.Add(loginTTL)
	l.pending[id] = p
	return id, nil
}

// take returns and removes the login id names: state and nonce are single-use.
func (l *Login) take(id string) (pendingLogin, bool) {
	l.mu.Lock()
	defer l.mu.Unlock()
	p, ok := l.pending[id]
	delete(l.pending, id)
	return p, ok && l.now().Before(p.expires)
}

// ActorFor is the operator handle a session of email acts as: the handle
// Operators maps the email to (case-insensitively), else Operator.
func (l *Login) ActorFor(email string) string {
	if handle, ok := l.Operators[strings.ToLower(strings.TrimSpace(email))]; ok {
		return handle
	}
	return l.Operator
}

// admitted reports whether claims may log in: the provider vouches for the
// email and the email is on the allowed list.
func (l *Login) admitted(c oidc.Claims) bool {
	email := strings.ToLower(strings.TrimSpace(c.Email))
	return bool(c.EmailVerified) && l.Allowed[email]
}

// sessionIdentity is the browser session a request authenticated with.
type sessionIdentity struct {
	id      string
	session auth.Session
}

type sessionContextKey struct{}

func withSession(ctx context.Context, s sessionIdentity) context.Context {
	return context.WithValue(ctx, sessionContextKey{}, s)
}

func sessionFrom(ctx context.Context) (sessionIdentity, bool) {
	s, ok := ctx.Value(sessionContextKey{}).(sessionIdentity)
	return s, ok
}

// sessionCookie returns the session ID the request carries.
func sessionCookie(r *http.Request) (string, bool) {
	c, err := r.Cookie(SessionCookie)
	if err != nil || c.Value == "" {
		return "", false
	}
	return c.Value, true
}

// isNavigation reports whether r is a browser navigation: a GET that asks for
// an HTML page. Everything else is an API call.
func isNavigation(r *http.Request) bool {
	return r.Method == http.MethodGet && strings.Contains(r.Header.Get("Accept"), "text/html")
}

// stateMatches compares the state the provider returned with the one issued.
func stateMatches(got, want string) bool {
	return subtle.ConstantTimeCompare([]byte(got), []byte(want)) == 1
}

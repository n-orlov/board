package server

import (
	"context"
	"errors"
	"net/http"
	"time"

	"github.com/n-orlov/board/internal/auth"
	"github.com/n-orlov/board/internal/oidc"
)

// problemResponse answers one of the /auth operations with a problem document.
// It satisfies the generated response interface of each of them, so one type
// stands for every 4xx and 5xx the document declares.
type problemResponse struct {
	status int
	detail string
	// next is the suggested next action; empty means the status's default.
	next string
	// extra holds members added to the document, such as expired_at.
	extra map[string]any
}

func (p problemResponse) write(w http.ResponseWriter) error {
	writeProblemExtra(w, p.status, p.detail, p.next, p.extra)
	return nil
}

func (p problemResponse) VisitGetAuthLoginResponse(w http.ResponseWriter) error { return p.write(w) }
func (p problemResponse) VisitGetAuthOidcCallbackResponse(w http.ResponseWriter) error {
	return p.write(w)
}
func (p problemResponse) VisitGetAuthSessionResponse(w http.ResponseWriter) error { return p.write(w) }
func (p problemResponse) VisitPostAuthLogoutResponse(w http.ResponseWriter) error { return p.write(w) }

func failure(status int, detail string) problemResponse {
	return problemResponse{status: status, detail: detail}
}

// cookie is an HttpOnly, Secure, SameSite=Lax cookie that lives maxAge seconds
// (negative: the browser drops it), as a Set-Cookie header value.
func cookie(name, value, path string, maxAge int) *string {
	c := &http.Cookie{Name: name, Value: value, Path: path, MaxAge: maxAge, HttpOnly: true, Secure: true, SameSite: http.SameSiteLaxMode}
	return ptr(c.String())
}

type requestContextKey struct{}

// withRequest is a strict middleware that hands an operation the http.Request,
// for the operations that read query parameters and cookies of their own.
func withRequest(next StrictHandlerFunc, _ string) StrictHandlerFunc {
	return func(ctx context.Context, w http.ResponseWriter, r *http.Request, request any) (any, error) {
		return next(context.WithValue(ctx, requestContextKey{}, r), w, r, request)
	}
}

func requestFrom(ctx context.Context) *http.Request {
	r, _ := ctx.Value(requestContextKey{}).(*http.Request)
	return r
}

// loginOps is the /auth operations of one Login. A nil login (browser login is
// not configured) answers 404 to a request that reaches them.
type loginOps struct{ login *Login }

func (o loginOps) off() (problemResponse, bool) {
	return failure(http.StatusNotFound, "browser login is not configured"), o.login == nil
}

// GetAuthLogin starts a login: it remembers state, nonce and the PKCE verifier
// under a pre-login cookie and sends the browser to the provider.
func (o loginOps) GetAuthLogin(ctx context.Context, _ GetAuthLoginRequestObject) (GetAuthLoginResponseObject, error) {
	if r, off := o.off(); off {
		return r, nil
	}
	state, err1 := oidc.RandomToken(24)
	nonce, err2 := oidc.RandomToken(24)
	verifier, err3 := oidc.NewVerifier()
	if err := errors.Join(err1, err2, err3); err != nil {
		return nil, err
	}
	target, err := o.login.Identity.AuthURL(ctx, state, nonce, verifier)
	if err != nil {
		return failure(http.StatusBadGateway, "the identity provider is not reachable"), nil
	}
	id, err := o.login.begin(pendingLogin{state: state, nonce: nonce, verifier: verifier})
	if err != nil {
		return failure(http.StatusServiceUnavailable, "too many logins are in progress"), nil
	}
	return GetAuthLogin302Response{Headers: GetAuthLogin302ResponseHeaders{
		Location: &target, SetCookie: cookie(loginCookie, id, "/auth", int(loginTTL/time.Second)),
	}}, nil
}

// callbackParams are what the provider's redirect carries, read from the raw
// request: query parameters code, state and error, and the pre-login cookie.
type callbackParams struct {
	code, state, providerError string
	loginID                    string
}

func readCallback(r *http.Request) callbackParams {
	q := r.URL.Query()
	p := callbackParams{code: q.Get("code"), state: q.Get("state"), providerError: q.Get("error")}
	if c, err := r.Cookie(loginCookie); err == nil {
		p.loginID = c.Value
	}
	return p
}

// GetAuthOidcCallback finishes a login. state, nonce and the verifier come from
// the pre-login cookie's pending login and are spent whatever the outcome.
func (o loginOps) GetAuthOidcCallback(ctx context.Context, _ GetAuthOidcCallbackRequestObject) (GetAuthOidcCallbackResponseObject, error) {
	if r, off := o.off(); off {
		return r, nil
	}
	params := readCallback(requestFrom(ctx))
	p, ok := o.login.take(params.loginID)
	switch {
	case !ok:
		return failure(http.StatusBadRequest, "no login is in progress: the pre-login cookie is missing, used or expired, so there is no PKCE verifier"), nil
	case !stateMatches(params.state, p.state):
		return failure(http.StatusBadRequest, "state does not match the login in progress"), nil
	case params.providerError != "":
		return failure(http.StatusForbidden, "the identity provider refused the login"), nil
	case params.code == "":
		return failure(http.StatusBadRequest, "the callback carries no authorization code"), nil
	}
	return o.login.finish(ctx, params.code, p)
}

// finish redeems code, checks the ID token and admits the user.
func (l *Login) finish(ctx context.Context, code string, p pendingLogin) (GetAuthOidcCallbackResponseObject, error) {
	raw, err := l.Identity.Exchange(ctx, code, p.verifier)
	if err != nil {
		return failure(http.StatusBadGateway, "the identity provider refused the code"), nil
	}
	claims, err := l.Identity.Verify(ctx, raw, p.nonce)
	if err != nil {
		return tokenProblem(err), nil
	}
	if !l.admitted(claims) {
		return failure(http.StatusForbidden, "this account may not log in: its email is not verified or not allowed"), nil
	}
	id, _, err := l.Sessions.Create(ctx, claims.Email)
	if err != nil {
		return nil, err
	}
	return GetAuthOidcCallback302Response{Headers: GetAuthOidcCallback302ResponseHeaders{
		Location: ptr("/"), SetCookie: cookie(SessionCookie, id, "/", int(l.sessionTTL()/time.Second)),
	}}, nil
}

func (l *Login) sessionTTL() time.Duration {
	if l.Sessions.TTL > 0 {
		return l.Sessions.TTL
	}
	return auth.DefaultSessionTTL
}

// tokenProblem maps an ID-token failure to its problem document.
func tokenProblem(err error) problemResponse {
	switch {
	case errors.Is(err, oidc.ErrNonce):
		return failure(http.StatusBadRequest, "the ID token's nonce does not belong to this login")
	case errors.Is(err, oidc.ErrInvalidToken):
		return failure(http.StatusUnauthorized, "the ID token is not valid")
	}
	return failure(http.StatusBadGateway, "the identity provider could not be checked")
}

// GetAuthSession describes the caller's browser session.
func (o loginOps) GetAuthSession(ctx context.Context, _ GetAuthSessionRequestObject) (GetAuthSessionResponseObject, error) {
	if r, off := o.off(); off {
		return r, nil
	}
	s, ok := sessionFrom(ctx)
	if !ok {
		return failure(http.StatusUnauthorized, "no browser session"), nil
	}
	return GetAuthSession200JSONResponse(Session{
		Email: s.session.Email, Principal: "oidc:" + s.session.Email, Actor: o.login.Operator, CsrfToken: s.session.CSRFToken,
	}), nil
}

// PostAuthLogout ends the caller's browser session.
func (o loginOps) PostAuthLogout(ctx context.Context, _ PostAuthLogoutRequestObject) (PostAuthLogoutResponseObject, error) {
	if r, off := o.off(); off {
		return r, nil
	}
	s, ok := sessionFrom(ctx)
	if !ok {
		return failure(http.StatusUnauthorized, "no browser session"), nil
	}
	if err := o.login.Sessions.Delete(ctx, s.id); err != nil {
		return nil, err
	}
	return PostAuthLogout204Response{Headers: PostAuthLogout204ResponseHeaders{SetCookie: cookie(SessionCookie, "", "/", -1)}}, nil
}

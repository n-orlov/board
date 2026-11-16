package features

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"

	"github.com/cucumber/godog"

	"github.com/n-orlov/village-hall/internal/oidc"
	"github.com/n-orlov/village-hall/internal/oidc/oidctest"
)

const (
	oidcClientID  = "board-godog"
	sessionCookie = "board_session"
	loginCookie   = "board_login"
	maxRedirects  = 10
)

// browser is the scenario's stand-in for a person's browser: it keeps cookies
// per origin, sends Accept: text/html and does not follow a redirect on its own.
type browser struct {
	cookies map[string]map[string]string
	// location and body are the last response's Location header and body.
	location string
	body     string
	// stateOverride replaces the state of a callback it follows, and dropLogin
	// makes it discard the pre-login cookie the moment it is set.
	stateOverride string
	dropLogin     bool
	// csrf is the CSRF token the browser read from /auth/session, remembered
	// the session cookie one it set aside to present again.
	csrf, remembered string
}

func (b *browser) jar(host string) map[string]string {
	if b.cookies == nil {
		b.cookies = map[string]map[string]string{}
	}
	if b.cookies[host] == nil {
		b.cookies[host] = map[string]string{}
	}
	return b.cookies[host]
}

// remember applies a response's Set-Cookie headers to the jar of host.
func (b *browser) remember(host string, resp *http.Response) {
	jar := b.jar(host)
	for _, c := range resp.Cookies() {
		if c.MaxAge < 0 || c.Value == "" || (b.dropLogin && c.Name == loginCookie) {
			delete(jar, c.Name)
			continue
		}
		jar[c.Name] = c.Value
	}
}

// resolve makes target absolute against the board.
func (h *harness) resolve(target string) string {
	if strings.HasPrefix(target, "http://") || strings.HasPrefix(target, "https://") {
		return target
	}
	return "http://" + h.addr + target
}

// browse sends GET target as the browser and records the response.
func (h *harness) browse(ctx context.Context, target string) error {
	return h.send(ctx, http.MethodGet, target, "text/html", nil)
}

// send sends method target as the browser, with its cookies, the Accept header
// and extra headers, and records the response.
func (h *harness) send(ctx context.Context, method, target, accept string, headers map[string]string) error {
	return h.sendBody(ctx, method, target, accept, headers, "")
}

// sendBody is send with a JSON request payload, none when payload is empty.
func (h *harness) sendBody(ctx context.Context, method, target, accept string, headers map[string]string, payload string) error {
	target = h.resolve(target)
	req, err := http.NewRequestWithContext(ctx, method, target, strings.NewReader(payload))
	if err != nil {
		return err
	}
	if payload != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	req.Header.Set("Accept", accept)
	for name, value := range headers {
		req.Header.Set(name, value)
	}
	for name, value := range h.web.jar(req.URL.Host) {
		req.AddCookie(&http.Cookie{Name: name, Value: value, Secure: true, HttpOnly: true, SameSite: http.SameSiteLaxMode})
	}
	client := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return err
	}
	h.web.remember(req.URL.Host, resp)
	h.status, h.web.body, h.web.location = resp.StatusCode, string(body), resp.Header.Get("Location")
	return nil
}

func (h *harness) browserOpens(ctx context.Context, path string) error { return h.browse(ctx, path) }

// followRedirects follows redirects until a response is not one, applying the
// browser's tampering to the callback.
func (h *harness) followRedirects(ctx context.Context) error {
	for i := 0; i < maxRedirects; i++ {
		if h.status < 300 || h.status >= 400 {
			return nil
		}
		next, err := h.nextHop()
		if err != nil {
			return err
		}
		if err := h.browse(ctx, next); err != nil {
			return err
		}
	}
	return fmt.Errorf("more than %d redirects", maxRedirects)
}

// nextHop is the URL the last redirect points at, as the browser would request it.
func (h *harness) nextHop() (string, error) {
	base, err := url.Parse(h.resolve("/"))
	if err != nil {
		return "", err
	}
	ref, err := url.Parse(h.web.location)
	if err != nil {
		return "", err
	}
	next := base.ResolveReference(ref)
	if h.web.stateOverride != "" && next.Path == "/auth/oidc/callback" {
		q := next.Query()
		q.Set("state", h.web.stateOverride)
		next.RawQuery = q.Encode()
	}
	return next.String(), nil
}

func (h *harness) followRedirectsReplacingState(ctx context.Context, state string) error {
	h.web.stateOverride = state
	return h.followRedirects(ctx)
}

func (h *harness) browserDiscardsLoginCookie() error {
	h.web.dropLogin = true
	return nil
}

func (h *harness) responseRedirectsTo(want string) error {
	if h.status != http.StatusFound || h.web.location != want {
		return fmt.Errorf("response %d to %q, want a 302 to %q", h.status, h.web.location, want)
	}
	return nil
}

func (h *harness) responseRedirectsToProvider() error {
	want := h.idp.URL() + "/authorize?"
	if h.status != http.StatusFound || !strings.HasPrefix(h.web.location, want) {
		return fmt.Errorf("response %d to %q, want a 302 to a URL starting with %s", h.status, h.web.location, want)
	}
	return nil
}

func (h *harness) browserHoldsLoginCookie() error {
	if _, has := h.web.jar(h.addr)[loginCookie]; !has {
		return errors.New("the browser holds no pre-login cookie")
	}
	return nil
}

func (h *harness) browserHoldsSession(want bool) error {
	_, has := h.web.jar(h.addr)[sessionCookie]
	if has != want {
		return fmt.Errorf("browser holds a session cookie: %v, want %v", has, want)
	}
	return nil
}

func (h *harness) browserForgets() error {
	h.web.cookies = nil
	return nil
}

func (h *harness) problemSaying(want string) error {
	var doc struct {
		Status int    `json:"status"`
		Detail string `json:"detail"`
	}
	if err := json.Unmarshal([]byte(h.web.body), &doc); err != nil || doc.Status != h.status {
		return fmt.Errorf("body %q is not a problem document with status %d", h.web.body, h.status)
	}
	if !strings.Contains(doc.Detail, want) {
		return fmt.Errorf("problem detail %q does not mention %q", doc.Detail, want)
	}
	return nil
}

func (h *harness) sessionResponseIs(email, actor string) error {
	var raw map[string]string
	if err := json.Unmarshal([]byte(h.web.body), &raw); err != nil {
		return fmt.Errorf("session body %q: %w", h.web.body, err)
	}
	if raw["email"] != email || raw["principal"] != "oidc:"+email || raw["actor"] != actor || raw["csrf_token"] == "" {
		return fmt.Errorf("session %v, want %s acting as %s with a CSRF token", raw, email, actor)
	}
	return nil
}

// startIdentityProvider starts the mock provider for the scenario, once.
func (h *harness) startIdentityProvider() error {
	if h.idp != nil {
		return nil
	}
	secret, err := oidc.RandomToken(24)
	if err != nil {
		return err
	}
	h.idpSecret = secret
	h.idp, err = oidctest.New(oidcClientID, secret)
	return err
}

func (h *harness) providerSignsIn(email, kind string) error {
	if err := h.startIdentityProvider(); err != nil {
		return err
	}
	h.idp.SignIn(email, kind == "a verified")
	return nil
}

func (h *harness) serverWithOIDC(ctx context.Context, allowed string) (context.Context, error) {
	return h.serverWithOIDCSetting(ctx, allowed, "", "")
}

// serverWithOIDCSetting starts the server with OIDC login and, when name is
// not empty, one more BOARD_* setting.
func (h *harness) serverWithOIDCSetting(ctx context.Context, allowed, name, value string) (context.Context, error) {
	if h.idp == nil {
		return ctx, errors.New("the mock OIDC provider is not running")
	}
	extra := []string{
		"BOARD_PUBLIC_URL=http://{addr}", "BOARD_OIDC_ISSUER=" + h.idp.URL(), "BOARD_OIDC_CLIENT_ID=" + oidcClientID,
		"BOARD_OIDC_CLIENT_SECRET=" + h.idpSecret, "BOARD_OIDC_ALLOWED_EMAILS=" + allowed,
	}
	if name != "" {
		extra = append(extra, name+"="+value)
	}
	return h.startServerWith(ctx, extra...)
}

func (h *harness) providerSawAuthorization() error {
	for _, r := range h.idp.Requests() {
		q := r.Params
		if r.Authorize && q.Get("code_challenge_method") == "S256" && q.Get("code_challenge") != "" && q.Get("state") != "" && q.Get("nonce") != "" &&
			q.Get("response_type") == "code" && strings.HasSuffix(q.Get("redirect_uri"), "/auth/oidc/callback") {
			return nil
		}
	}
	return fmt.Errorf("the provider saw no authorization request with PKCE S256, state and nonce: %v", h.idp.Requests())
}

func (h *harness) providerSawVerifier() error {
	for _, r := range h.idp.Requests() {
		if !r.Authorize && r.VerifierMatches {
			return nil
		}
	}
	return fmt.Errorf("the provider saw no token request with a matching code verifier: %v", h.idp.Requests())
}

func (h *harness) providerSawNoTokenRequest() error {
	for _, r := range h.idp.Requests() {
		if !r.Authorize {
			return errors.New("the provider received a token request")
		}
	}
	return nil
}

func (h *harness) providerReusesNonce() error {
	if !h.idp.ReuseNonce() {
		return errors.New("no login has completed yet, so there is no nonce to reuse")
	}
	return nil
}

func (h *harness) providerRewritesEmail(email string) error {
	if err := h.startIdentityProvider(); err != nil {
		return err
	}
	h.idp.ForgeEmail(email)
	return nil
}

// sessionRows reads every id_hash and email of the sessions table.
func (h *harness) sessionRows(ctx context.Context) (map[string]string, error) {
	db, err := h.openDB()
	if err != nil {
		return nil, err
	}
	defer db.Close()
	rows, err := db.QueryContext(ctx, "SELECT id_hash, csrf_token FROM sessions")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]string{}
	for rows.Next() {
		var hash, csrf string
		if err := rows.Scan(&hash, &csrf); err != nil {
			return nil, err
		}
		out[hash] = csrf
	}
	return out, rows.Err()
}

func (h *harness) databaseHoldsSessions(ctx context.Context, n int) error {
	rows, err := h.sessionRows(ctx)
	if err != nil {
		return err
	}
	if len(rows) != n {
		return fmt.Errorf("the sessions table holds %d rows, want %d", len(rows), n)
	}
	return nil
}

// sessionsStoredHashed requires one session row keyed by the SHA-256 of the
// browser's cookie, and no column of it equal to the cookie itself.
func (h *harness) sessionsStoredHashed(ctx context.Context, n int) error {
	if err := h.databaseHoldsSessions(ctx, n); err != nil {
		return err
	}
	cookie := h.web.jar(h.addr)[sessionCookie]
	rows, _ := h.sessionRows(ctx)
	if cookie != "" {
		sum := sha256.Sum256([]byte(cookie))
		if _, ok := rows[hex.EncodeToString(sum[:])]; !ok {
			return errors.New("no session row is keyed by the SHA-256 of the session cookie")
		}
	}
	for hash, csrf := range rows {
		if hash == cookie || csrf == cookie {
			return errors.New("the sessions table holds the session cookie itself")
		}
	}
	return nil
}

func registerOIDCSteps(sc *godog.ScenarioContext, h *harness) {
	sc.Step(`^the mock OIDC provider signs in "([^"]*)" with (a verified|an unverified) email$`, h.providerSignsIn)
	sc.Step(`^the board server is running with OIDC login allowing "([^"]*)"$`, h.serverWithOIDC)
	sc.Step(`^the board server is running with OIDC login allowing "([^"]*)" and (BOARD_SESSION_TTL|BOARD_SESSION_IDLE|BOARD_SWEEP_INTERVAL) set to "([^"]*)"$`, h.serverWithOIDCSetting)
	registerSessionSteps(sc, h)
	sc.Step(`^the browser opens "([^"]*)"$`, h.browserOpens)
	sc.Step(`^the browser requests GET "([^"]*)"$`, h.browserOpens)
	sc.Step(`^the browser follows the redirects$`, h.followRedirects)
	sc.Step(`^the browser follows the redirects but replaces the callback's state with "([^"]*)"$`, h.followRedirectsReplacingState)
	sc.Step(`^the browser discards the pre-login cookie it is given$`, h.browserDiscardsLoginCookie)
	sc.Step(`^the browser forgets its cookies$`, h.browserForgets)
	sc.Step(`^the browser holds a session cookie$`, func() error { return h.browserHoldsSession(true) })
	sc.Step(`^the browser holds no session cookie$`, func() error { return h.browserHoldsSession(false) })
	sc.Step(`^the response redirects to "([^"]*)"$`, h.responseRedirectsTo)
	sc.Step(`^the response redirects to the mock provider's authorization endpoint$`, h.responseRedirectsToProvider)
	sc.Step(`^the browser holds a pre-login cookie$`, h.browserHoldsLoginCookie)
	sc.Step(`^the response is a problem document saying "([^"]*)"$`, h.problemSaying)
	sc.Step(`^the session response is for "([^"]*)" acting as "([^"]*)"$`, h.sessionResponseIs)
	sc.Step(`^the provider received an authorization request with PKCE method S256, a state and a nonce$`, h.providerSawAuthorization)
	sc.Step(`^the provider received a token request whose code verifier matches the code challenge$`, h.providerSawVerifier)
	sc.Step(`^the provider received no token request$`, h.providerSawNoTokenRequest)
	sc.Step(`^the provider signs its next ID token with the nonce of the previous login$`, h.providerReusesNonce)
	sc.Step(`^the provider rewrites the email of the ID tokens it signs to "([^"]*)"$`, h.providerRewritesEmail)
	sc.Step(`^the database holds (\d+) sessions?$`, h.databaseHoldsSessions)
	sc.Step(`^the database holds (\d+) sessions? stored only as the SHA-256 hash of the session cookie$`, h.sessionsStoredHashed)
}

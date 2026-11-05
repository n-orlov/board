package oidctest

import (
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/n-orlov/board/internal/oidc"
)

func start(t *testing.T) *Provider {
	t.Helper()
	p, err := New("board", "s3cret")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(p.Close)
	p.SignIn("a@example.com", true)
	return p
}

var noRedirect = &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}

// authorize sends an authorization request and returns the status and Location.
func authorize(t *testing.T, p *Provider, q url.Values) (int, string) {
	t.Helper()
	req, err := http.NewRequest(http.MethodGet, p.URL()+"/authorize?"+q.Encode(), nil)
	if err != nil {
		t.Fatal(err)
	}
	resp, err := noRedirect.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()
	return resp.StatusCode, resp.Header.Get("Location")
}

func goodQuery(verifier string) url.Values {
	return url.Values{
		"response_type": {"code"}, "client_id": {"board"}, "redirect_uri": {"http://board.test/cb"}, "state": {"s"}, "nonce": {"n"},
		"code_challenge": {oidc.Challenge(verifier)}, "code_challenge_method": {"S256"},
	}
}

// token sends a token request and returns the status.
func token(t *testing.T, p *Provider, user, pass string, form url.Values) int {
	t.Helper()
	req, err := http.NewRequest(http.MethodPost, p.URL()+"/token", strings.NewReader(form.Encode()))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.SetBasicAuth(user, pass)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()
	return resp.StatusCode
}

func issueCode(t *testing.T, p *Provider) string {
	t.Helper()
	status, location := authorize(t, p, goodQuery("v1"))
	loc, err := url.Parse(location)
	if err != nil || status != http.StatusFound || loc.Query().Get("code") == "" {
		t.Fatalf("authorize answered %d %q", status, location)
	}
	return loc.Query().Get("code")
}

func TestAuthorizeRefusesARequestWithoutPKCEStateOrNonce(t *testing.T) {
	p := start(t)
	for _, drop := range []string{"code_challenge", "state", "nonce", "client_id", "response_type", "redirect_uri"} {
		q := goodQuery("v")
		q.Del(drop)
		if status, _ := authorize(t, p, q); status != http.StatusBadRequest {
			t.Errorf("without %s: %d, want 400", drop, status)
		}
	}
	q := goodQuery("v")
	q.Set("code_challenge_method", "plain")
	if status, _ := authorize(t, p, q); status != http.StatusBadRequest {
		t.Errorf("plain PKCE: %d, want 400", status)
	}
}

func TestTokenRefusesAMissingOrWrongVerifierAndASpentCode(t *testing.T) {
	p := start(t)
	redeem := func(code, verifier, user, pass string) int {
		form := url.Values{"grant_type": {"authorization_code"}, "code": {code}, "redirect_uri": {"http://board.test/cb"}}
		if verifier != "" {
			form.Set("code_verifier", verifier)
		}
		return token(t, p, user, pass, form)
	}
	if got := redeem(issueCode(t, p), "", "board", "s3cret"); got != http.StatusBadRequest {
		t.Errorf("no verifier: %d, want 400", got)
	}
	if got := redeem(issueCode(t, p), "other", "board", "s3cret"); got != http.StatusBadRequest {
		t.Errorf("wrong verifier: %d, want 400", got)
	}
	if got := redeem(issueCode(t, p), "v1", "board", "wrong"); got != http.StatusUnauthorized {
		t.Errorf("wrong secret: %d, want 401", got)
	}
	code := issueCode(t, p)
	if got := redeem(code, "v1", "board", "s3cret"); got != http.StatusOK {
		t.Fatalf("good redemption: %d", got)
	}
	if got := redeem(code, "v1", "board", "s3cret"); got != http.StatusBadRequest {
		t.Errorf("a spent code: %d, want 400", got)
	}
}

func TestReuseNonceNeedsAnEarlierLogin(t *testing.T) {
	p := start(t)
	if p.ReuseNonce() {
		t.Fatal("ReuseNonce reported a nonce before any login")
	}
	if n := len(p.Requests()); n != 0 {
		t.Fatalf("%d requests recorded before any", n)
	}
}

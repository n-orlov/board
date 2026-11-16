package oidc_test

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/n-orlov/village-hall/internal/oidc"
	"github.com/n-orlov/village-hall/internal/oidc/oidctest"
)

const clientID, secret = "board", "s3cret"

func newProvider(t *testing.T) (*oidctest.Provider, *oidc.Provider) {
	t.Helper()
	idp, err := oidctest.New(clientID, secret)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(idp.Close)
	idp.SignIn("a@example.com", true)
	return idp, oidc.New(oidc.Config{Issuer: idp.URL() + "/", ClientID: clientID, ClientSecret: secret, RedirectURL: "http://board.test/auth/oidc/callback"})
}

// login runs the flow up to the raw ID token, the way a browser would.
func login(t *testing.T, p *oidc.Provider) (idToken, nonce string) {
	t.Helper()
	ctx := context.Background()
	verifier, err := oidc.NewVerifier()
	if err != nil {
		t.Fatal(err)
	}
	target, err := p.AuthURL(ctx, "st", "nn", verifier)
	if err != nil {
		t.Fatal(err)
	}
	client := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	resp, err := client.Get(target)
	if err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()
	loc, err := url.Parse(resp.Header.Get("Location"))
	if err != nil || loc.Query().Get("state") != "st" {
		t.Fatalf("authorization answered %d %q", resp.StatusCode, resp.Header.Get("Location"))
	}
	raw, err := p.Exchange(ctx, loc.Query().Get("code"), verifier)
	if err != nil {
		t.Fatalf("Exchange: %v", err)
	}
	return raw, "nn"
}

func TestAuthURLCarriesPKCEStateAndNonce(t *testing.T) {
	_, p := newProvider(t)
	target, err := p.AuthURL(context.Background(), "the-state", "the-nonce", "the-verifier")
	if err != nil {
		t.Fatal(err)
	}
	q := mustQuery(t, target)
	want := map[string]string{
		"response_type": "code", "client_id": clientID, "state": "the-state", "nonce": "the-nonce",
		"code_challenge_method": "S256", "code_challenge": oidc.Challenge("the-verifier"),
		"redirect_uri": "http://board.test/auth/oidc/callback", "scope": "openid email profile",
	}
	for k, v := range want {
		if q.Get(k) != v {
			t.Errorf("%s = %q, want %q", k, q.Get(k), v)
		}
	}
}

func mustQuery(t *testing.T, raw string) url.Values {
	t.Helper()
	u, err := url.Parse(raw)
	if err != nil {
		t.Fatal(err)
	}
	return u.Query()
}

func TestChallengeIsRFC7636S256(t *testing.T) {
	// The worked example of RFC 7636 appendix B.
	got := oidc.Challenge("dBjftJeZ4CVP-mB92K27uhbUJU1p1r_wW1gFWFOEjXk")
	if got != "E9Melhoa2OwvFrEMTJguCHaoeK1t8URWbuGJSstw-cM" {
		t.Fatalf("challenge %q", got)
	}
	v, err := oidc.NewVerifier()
	if err != nil || len(v) != 43 {
		t.Fatalf("verifier %q: %v", v, err)
	}
}

func TestVerifyAcceptsAGenuineTokenOnce(t *testing.T) {
	_, p := newProvider(t)
	raw, nonce := login(t, p)
	c, err := p.Verify(context.Background(), raw, nonce)
	if err != nil || c.Email != "a@example.com" || !bool(c.EmailVerified) {
		t.Fatalf("Verify = %+v, %v", c, err)
	}
}

func TestVerifyRefusesWhatIsNotAGenuineTokenForThisLogin(t *testing.T) {
	idp, p := newProvider(t)
	raw, nonce := login(t, p)
	parts := strings.Split(raw, ".")
	ctx := context.Background()
	cases := map[string]struct {
		token, nonce string
		want         error
	}{
		"other nonce":    {raw, "other", oidc.ErrNonce},
		"not a JWS":      {"abc", nonce, oidc.ErrInvalidToken},
		"bad base64":     {"!.!.!", nonce, oidc.ErrInvalidToken},
		"tampered":       {parts[0] + "." + parts[1] + "A." + parts[2], nonce, oidc.ErrInvalidToken},
		"wrong sig":      {parts[0] + "." + parts[1] + "." + parts[2][:len(parts[2])-4] + "AAAA", nonce, oidc.ErrInvalidToken},
		"alg none":       {"eyJhbGciOiJub25lIiwia2lkIjoibW9jay1rZXktMSJ9." + parts[1] + ".", nonce, oidc.ErrInvalidToken},
		"unknown key id": {"eyJhbGciOiJSUzI1NiIsImtpZCI6Im5vcGUifQ." + parts[1] + "." + parts[2], nonce, oidc.ErrInvalidToken},
	}
	for name, tc := range cases {
		if _, err := p.Verify(ctx, tc.token, tc.nonce); !errors.Is(err, tc.want) {
			t.Errorf("%s: %v, want %v", name, err, tc.want)
		}
	}
	other := oidc.New(oidc.Config{Issuer: idp.URL(), ClientID: "someone-else", ClientSecret: secret})
	if _, err := other.Verify(ctx, raw, nonce); !errors.Is(err, oidc.ErrInvalidToken) {
		t.Errorf("another audience: %v", err)
	}
}

func TestVerifyRefusesAnExpiredToken(t *testing.T) {
	idp, _ := newProvider(t)
	late := oidc.New(oidc.Config{Issuer: idp.URL(), ClientID: clientID, ClientSecret: secret, RedirectURL: "http://board.test/cb",
		Now: func() time.Time { return time.Now().Add(time.Hour) }})
	raw, nonce := login(t, late)
	if _, err := late.Verify(context.Background(), raw, nonce); !errors.Is(err, oidc.ErrInvalidToken) {
		t.Fatalf("an hour-old token: %v", err)
	}
}

func TestProviderFailuresAreErrProvider(t *testing.T) {
	idp, p := newProvider(t)
	ctx := context.Background()
	if _, err := p.Exchange(ctx, "no-such-code", "v"); !errors.Is(err, oidc.ErrProvider) {
		t.Errorf("bad code: %v", err)
	}
	wrong := oidc.New(oidc.Config{Issuer: idp.URL(), ClientID: clientID, ClientSecret: "wrong"})
	if _, err := wrong.Exchange(ctx, "c", "v"); !errors.Is(err, oidc.ErrProvider) {
		t.Errorf("wrong secret: %v", err)
	}
	idp.Close()
	down := oidc.New(oidc.Config{Issuer: idp.URL(), ClientID: clientID})
	if _, err := down.AuthURL(ctx, "s", "n", "v"); !errors.Is(err, oidc.ErrProvider) {
		t.Errorf("provider down: %v", err)
	}
	if _, err := down.Verify(ctx, "a.b.c", "n"); err == nil {
		t.Error("Verify with the provider down succeeded")
	}
}

func TestDiscoveryMustDescribeTheConfiguredIssuer(t *testing.T) {
	idp, _ := newProvider(t)
	p := oidc.New(oidc.Config{Issuer: idp.URL() + "/other", ClientID: clientID})
	if _, err := p.AuthURL(context.Background(), "s", "n", "v"); !errors.Is(err, oidc.ErrProvider) {
		t.Fatalf("no discovery document at that issuer: %v", err)
	}
}

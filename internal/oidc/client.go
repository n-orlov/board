package oidc

import (
	"context"
	"crypto/rsa"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
)

// ErrProvider is wrapped by every error caused by the identity provider being
// unreachable or answering something unusable, as opposed to a refused token.
var ErrProvider = errors.New("identity provider failure")

// jwksRefreshInterval is the least time between two JWKS fetches caused by an
// unknown kid.
const jwksRefreshInterval = 10 * time.Second

// maxBody bounds every provider response the client reads.
const maxBody = 1 << 20

// Config is one relying-party registration.
type Config struct {
	Issuer       string
	ClientID     string
	ClientSecret string
	// RedirectURL is the registered callback, <public URL>/auth/oidc/callback.
	RedirectURL string
	// HTTP is the client used for every provider request; nil means a client
	// with a 10 s timeout.
	HTTP *http.Client
	// Now is the clock; nil means time.Now.
	Now func() time.Time
}

type discovery struct {
	Issuer                string `json:"issuer"`
	AuthorizationEndpoint string `json:"authorization_endpoint"`
	TokenEndpoint         string `json:"token_endpoint"`
	JWKSURI               string `json:"jwks_uri"`
}

// Provider talks to one issuer. Discovery and the JWKS are fetched on first
// use and cached, so the board starts while the issuer is down.
type Provider struct {
	cfg Config

	mu        sync.Mutex
	disc      *discovery
	keys      map[string]*rsa.PublicKey
	refreshed time.Time
}

// New returns a Provider for cfg.
func New(cfg Config) *Provider {
	if cfg.HTTP == nil {
		cfg.HTTP = &http.Client{Timeout: 10 * time.Second}
	}
	cfg.Issuer = strings.TrimRight(cfg.Issuer, "/")
	return &Provider{cfg: cfg}
}

func (p *Provider) now() time.Time {
	if p.cfg.Now != nil {
		return p.cfg.Now()
	}
	return time.Now()
}

// getJSON fetches u and decodes the JSON answer into out.
func (p *Provider) getJSON(ctx context.Context, u string, out any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return fmt.Errorf("%w: %w", ErrProvider, err)
	}
	return p.do(req, out)
}

func (p *Provider) do(req *http.Request, out any) error {
	resp, err := p.cfg.HTTP.Do(req)
	if err != nil {
		return fmt.Errorf("%w: %w", ErrProvider, err)
	}
	defer func() { _ = resp.Body.Close() }()
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxBody))
	if err != nil || resp.StatusCode != http.StatusOK {
		return fmt.Errorf("%w: %s answered %d", ErrProvider, req.URL.Path, resp.StatusCode)
	}
	if err := json.Unmarshal(body, out); err != nil {
		return fmt.Errorf("%w: %s: unreadable answer", ErrProvider, req.URL.Path)
	}
	return nil
}

// endpoints returns the discovery document, fetching it once.
func (p *Provider) endpoints(ctx context.Context) (discovery, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.disc != nil {
		return *p.disc, nil
	}
	var d discovery
	if err := p.getJSON(ctx, p.cfg.Issuer+"/.well-known/openid-configuration", &d); err != nil {
		return discovery{}, err
	}
	if strings.TrimRight(d.Issuer, "/") != p.cfg.Issuer || d.AuthorizationEndpoint == "" || d.TokenEndpoint == "" || d.JWKSURI == "" {
		return discovery{}, fmt.Errorf("%w: the discovery document does not describe %s", ErrProvider, p.cfg.Issuer)
	}
	p.disc = &d
	return d, nil
}

// AuthURL is where the browser is sent to sign in: the authorization endpoint
// with PKCE (S256), state and nonce.
func (p *Provider) AuthURL(ctx context.Context, state, nonce, verifier string) (string, error) {
	d, err := p.endpoints(ctx)
	if err != nil {
		return "", err
	}
	q := url.Values{
		"response_type":         {"code"},
		"client_id":             {p.cfg.ClientID},
		"redirect_uri":          {p.cfg.RedirectURL},
		"scope":                 {"openid email profile"},
		"state":                 {state},
		"nonce":                 {nonce},
		"code_challenge":        {Challenge(verifier)},
		"code_challenge_method": {"S256"},
	}
	u, err := url.Parse(d.AuthorizationEndpoint)
	if err != nil {
		return "", fmt.Errorf("%w: authorization endpoint: %w", ErrProvider, err)
	}
	u.RawQuery = mergeQuery(u.RawQuery, q)
	return u.String(), nil
}

// mergeQuery appends q to an existing raw query.
func mergeQuery(existing string, q url.Values) string {
	if existing == "" {
		return q.Encode()
	}
	return existing + "&" + q.Encode()
}

// Exchange trades an authorization code and its PKCE verifier for the raw ID
// token.
func (p *Provider) Exchange(ctx context.Context, code, verifier string) (string, error) {
	d, err := p.endpoints(ctx)
	if err != nil {
		return "", err
	}
	form := url.Values{
		"grant_type":    {"authorization_code"},
		"code":          {code},
		"redirect_uri":  {p.cfg.RedirectURL},
		"code_verifier": {verifier},
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, d.TokenEndpoint, strings.NewReader(form.Encode()))
	if err != nil {
		return "", fmt.Errorf("%w: %w", ErrProvider, err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.SetBasicAuth(url.QueryEscape(p.cfg.ClientID), url.QueryEscape(p.cfg.ClientSecret))
	var out struct {
		IDToken string `json:"id_token"`
	}
	if err := p.do(req, &out); err != nil {
		return "", err
	}
	if out.IDToken == "" {
		return "", fmt.Errorf("%w: the token response holds no id_token", ErrProvider)
	}
	return out.IDToken, nil
}

var errUnknownKey = fmt.Errorf("%w: unknown signing key", ErrInvalidToken)

// key returns the signing key kid names, refreshing the JWKS once when it is
// not cached (and the last refresh is not recent).
func (p *Provider) key(ctx context.Context, kid string) (*rsa.PublicKey, error) {
	d, err := p.endpoints(ctx)
	if err != nil {
		return nil, err
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if k, ok := p.keys[kid]; ok {
		return k, nil
	}
	if p.keys != nil && p.now().Sub(p.refreshed) < jwksRefreshInterval {
		return nil, errUnknownKey
	}
	if err := p.refreshKeys(ctx, d.JWKSURI); err != nil {
		return nil, err
	}
	if k, ok := p.keys[kid]; ok {
		return k, nil
	}
	return nil, errUnknownKey
}

// refreshKeys replaces the cached JWKS with the issuer's current one. The
// caller holds p.mu.
func (p *Provider) refreshKeys(ctx context.Context, jwksURI string) error {
	var set struct {
		Keys []jwk `json:"keys"`
	}
	if err := p.getJSON(ctx, jwksURI, &set); err != nil {
		return err
	}
	p.keys, p.refreshed = map[string]*rsa.PublicKey{}, p.now()
	for _, k := range set.Keys {
		if pub, ok := k.publicKey(); ok {
			p.keys[k.Kid] = pub
		}
	}
	return nil
}

// Verify checks raw's signature against the issuer's JWKS, its issuer,
// audience and expiry, and that it carries wantNonce.
func (p *Provider) Verify(ctx context.Context, raw, wantNonce string) (Claims, error) {
	t, err := parseToken(raw)
	if err != nil {
		return Claims{}, err
	}
	key, err := p.key(ctx, t.kid)
	if err != nil {
		return Claims{}, err
	}
	if err := t.verifySignature(key); err != nil {
		return Claims{}, err
	}
	c, err := t.checkClaims(p.cfg.Issuer, p.cfg.ClientID, p.now())
	if err != nil {
		return Claims{}, err
	}
	if c.Nonce != wantNonce {
		return Claims{}, ErrNonce
	}
	return c, nil
}

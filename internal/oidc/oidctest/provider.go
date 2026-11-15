package oidctest

import (
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/n-orlov/board/internal/oidc"
)

const keyID = "mock-key-1"

// Request is what the provider saw of one authorization or token request.
type Request struct {
	// Authorize is true for an authorization request, false for a token request.
	Authorize bool
	// Params are the query (authorization) or form (token) parameters.
	Params url.Values
	// VerifierMatches is set on a token request: the code_verifier hashes to
	// the code_challenge the code was issued for.
	VerifierMatches bool
}

type issued struct {
	nonce, challenge, redirect, email string
	verified                          bool
}

// Provider is a running mock OIDC issuer. It signs in whoever SignIn names,
// with no login page: the authorization endpoint redirects straight back.
type Provider struct {
	server       *httptest.Server
	issuer       string
	key          *rsa.PrivateKey
	clientID     string
	clientSecret string

	mu         sync.Mutex
	email      string
	verified   bool
	forgeEmail string
	nextNonce  string
	lastNonce  string
	codes      map[string]issued
	requests   []Request
}

// New starts a provider that serves clientID with clientSecret. Close stops it.
func New(clientID, clientSecret string) (*Provider, error) {
	return serveWith(clientID, clientSecret, httptest.NewServer)
}

// NewAt starts a provider on the listener l and announces issuer as its URL: the address a browser
// reaches it at (say, behind a TLS proxy on another host), which may differ from l's own. Close stops
// it and closes l. fake-oidc uses it to sign in a phone to a local instance.
func NewAt(clientID, clientSecret string, l net.Listener, issuer string) (*Provider, error) {
	p, err := serveWith(clientID, clientSecret, func(h http.Handler) *httptest.Server {
		s := httptest.NewUnstartedServer(h)
		_ = s.Listener.Close()
		s.Listener = l
		s.Start()
		return s
	})
	if err == nil {
		p.issuer = strings.TrimRight(issuer, "/")
	}
	return p, err
}

func serveWith(clientID, clientSecret string, serve func(http.Handler) *httptest.Server) (*Provider, error) {
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		return nil, err
	}
	p := &Provider{key: key, clientID: clientID, clientSecret: clientSecret, codes: map[string]issued{}}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /.well-known/openid-configuration", p.discovery)
	mux.HandleFunc("GET /jwks", p.jwks)
	mux.HandleFunc("GET /authorize", p.authorize)
	mux.HandleFunc("POST /token", p.token)
	p.server = serve(mux)
	return p, nil
}

// URL is the issuer URL: the one NewAt was given, else the listener's own.
func (p *Provider) URL() string {
	if p.issuer != "" {
		return p.issuer
	}
	return p.server.URL
}

// Close stops the provider.
func (p *Provider) Close() { p.server.Close() }

// SignIn sets who the provider signs in next: an email and whether the
// provider vouches for it.
func (p *Provider) SignIn(email string, verified bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.email, p.verified = email, verified
}

// ForgeEmail makes the provider rewrite the email claim of the ID tokens it
// signs after signing them, so the signature no longer covers the payload.
func (p *Provider) ForgeEmail(email string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.forgeEmail = email
}

// ReuseNonce makes the next ID token carry the nonce of the one issued before
// it, instead of the nonce of the login it answers.
func (p *Provider) ReuseNonce() bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.nextNonce = p.lastNonce
	return p.lastNonce != ""
}

// Requests returns the requests the provider has seen, oldest first.
func (p *Provider) Requests() []Request {
	p.mu.Lock()
	defer p.mu.Unlock()
	return append([]Request(nil), p.requests...)
}

func (p *Provider) record(r Request) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.requests = append(p.requests, r)
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(v)
}

func (p *Provider) discovery(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, map[string]any{
		"issuer":                                p.URL(),
		"authorization_endpoint":                p.URL() + "/authorize",
		"token_endpoint":                        p.URL() + "/token",
		"jwks_uri":                              p.URL() + "/jwks",
		"response_types_supported":              []string{"code"},
		"id_token_signing_alg_values_supported": []string{"RS256"},
		"code_challenge_methods_supported":      []string{"S256"},
	})
}

func (p *Provider) jwks(w http.ResponseWriter, _ *http.Request) {
	pub := p.key.PublicKey
	writeJSON(w, map[string]any{"keys": []map[string]string{{
		"kty": "RSA", "use": "sig", "alg": "RS256", "kid": keyID,
		"n": base64.RawURLEncoding.EncodeToString(pub.N.Bytes()),
		"e": base64.RawURLEncoding.EncodeToString([]byte{1, 0, 1}),
	}}})
}

// authorize issues a code for whoever SignIn named and redirects to the
// client's redirect_uri. It refuses a request that does not use PKCE S256.
func (p *Provider) authorize(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	p.record(Request{Authorize: true, Params: q})
	redirect, err := p.checkAuthorize(q)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	code, _ := oidc.RandomToken(16)
	p.mu.Lock()
	p.codes[code] = issued{nonce: q.Get("nonce"), challenge: q.Get("code_challenge"), redirect: redirect.String(), email: p.email, verified: p.verified}
	p.mu.Unlock()
	rq := redirect.Query()
	rq.Set("code", code)
	rq.Set("state", q.Get("state"))
	redirect.RawQuery = rq.Encode()
	w.Header().Set("Location", redirect.String())
	w.WriteHeader(http.StatusFound)
}

// checkAuthorize validates an authorization request and returns its redirect URI.
func (p *Provider) checkAuthorize(q url.Values) (*url.URL, error) {
	redirect, err := url.Parse(q.Get("redirect_uri"))
	if q.Get("client_id") != p.clientID || q.Get("response_type") != "code" || err != nil || redirect.Scheme == "" {
		return nil, errors.New("invalid_request")
	}
	if !usesPKCE(q) || q.Get("state") == "" || q.Get("nonce") == "" {
		return nil, errors.New("invalid_request: PKCE S256, state and nonce are required")
	}
	return redirect, nil
}

// usesPKCE reports whether q carries a code challenge made with S256.
func usesPKCE(q url.Values) bool {
	return q.Get("code_challenge") != "" && q.Get("code_challenge_method") == "S256"
}

// token redeems a code once, for an ID token. It refuses a request with no
// client credentials, or whose code_verifier does not hash to the challenge.
func (p *Provider) token(w http.ResponseWriter, r *http.Request) {
	_ = r.ParseForm()
	p.mu.Lock()
	grant, found := p.codes[r.PostForm.Get("code")]
	delete(p.codes, r.PostForm.Get("code"))
	p.mu.Unlock()
	verifier := r.PostForm.Get("code_verifier")
	matches := found && verifier != "" && oidc.Challenge(verifier) == grant.challenge
	p.record(Request{Params: r.PostForm, VerifierMatches: matches})
	if !p.clientAuthenticated(r) {
		http.Error(w, `{"error":"invalid_client"}`, http.StatusUnauthorized)
		return
	}
	if !matches || r.PostForm.Get("redirect_uri") != grant.redirect {
		http.Error(w, `{"error":"invalid_grant"}`, http.StatusBadRequest)
		return
	}
	writeJSON(w, map[string]string{"token_type": "Bearer", "id_token": p.idToken(grant)})
}

// clientAuthenticated checks the client_secret_basic credentials and grant type.
func (p *Provider) clientAuthenticated(r *http.Request) bool {
	id, secret, ok := r.BasicAuth()
	id, _ = url.QueryUnescape(id)
	secret, _ = url.QueryUnescape(secret)
	return ok && id == p.clientID && secret == p.clientSecret && r.PostForm.Get("grant_type") == "authorization_code"
}

// idToken signs the ID token for grant, applying the provider's knobs.
func (p *Provider) idToken(grant issued) string {
	p.mu.Lock()
	nonce, forged := grant.nonce, p.forgeEmail
	if p.nextNonce != "" {
		nonce, p.nextNonce = p.nextNonce, ""
	}
	p.lastNonce = grant.nonce
	p.mu.Unlock()
	now := time.Now()
	claims := map[string]any{
		"iss": p.URL(), "aud": p.clientID, "sub": "mock|" + grant.email, "iat": now.Unix(), "exp": now.Add(5 * time.Minute).Unix(),
		"nonce": nonce, "email": grant.email, "email_verified": grant.verified,
	}
	token := p.sign(claims)
	if forged != "" {
		claims["email"], claims["email_verified"] = forged, true
		return replacePayload(token, claims)
	}
	return token
}

func b64(v any) string {
	raw, _ := json.Marshal(v)
	return base64.RawURLEncoding.EncodeToString(raw)
}

func (p *Provider) sign(claims map[string]any) string {
	signed := b64(map[string]string{"alg": "RS256", "typ": "JWT", "kid": keyID}) + "." + b64(claims)
	sum := sha256.Sum256([]byte(signed))
	sig, _ := rsa.SignPKCS1v15(rand.Reader, p.key, crypto.SHA256, sum[:])
	return signed + "." + base64.RawURLEncoding.EncodeToString(sig)
}

// replacePayload returns token with its payload swapped for claims and its
// header and signature untouched.
func replacePayload(token string, claims map[string]any) string {
	parts := strings.Split(token, ".")
	return parts[0] + "." + b64(claims) + "." + parts[2]
}

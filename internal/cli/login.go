package cli

import (
	"errors"
	"fmt"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/n-orlov/village-hall/internal/auth"
	"github.com/n-orlov/village-hall/internal/domain"
	"github.com/n-orlov/village-hall/internal/envvar"
	"github.com/n-orlov/village-hall/internal/oidc"
	"github.com/n-orlov/village-hall/internal/server"
	"github.com/n-orlov/village-hall/internal/store"
)

// DefaultOperatorHandle is the operator actor's handle when
// VH_OPERATOR_HANDLE is unset.
const DefaultOperatorHandle = "operator"

// loginSettings are the VH_* variables of browser login (SPEC 13).
type loginSettings struct {
	issuer, clientID, secret, publicURL string
	allowed                             map[string]bool
	operators                           map[string]string
	ttl, idle                           time.Duration
	operator                            string
}

// loginFromEnv builds the browser login from the environment. With
// VH_OIDC_ISSUER unset, browser login is off (nil, nil); with it set, every
// other OIDC setting must be set too.
func loginFromEnv(env Env, st *store.Store) (*server.Login, error) {
	if envvar.OIDCIssuer.Get(env) == "" {
		// Login is off, but a malformed mapping still refuses to start.
		_, err := parseOperators(envvar.OIDCOperators.Get(env))
		return nil, err
	}
	cfg, err := readLoginSettings(env)
	if err != nil {
		return nil, err
	}
	provider := oidc.New(oidc.Config{
		Issuer: cfg.issuer, ClientID: cfg.clientID, ClientSecret: cfg.secret,
		RedirectURL: strings.TrimRight(cfg.publicURL, "/") + "/auth/oidc/callback",
	})
	return &server.Login{
		Identity:  provider,
		Sessions:  auth.Sessions{Store: st, TTL: cfg.ttl, Idle: cfg.idle},
		Allowed:   cfg.allowed,
		Operators: cfg.operators,
		PublicURL: cfg.publicURL,
		Operator:  cfg.operator,
	}, nil
}

func readLoginSettings(env Env) (loginSettings, error) {
	secret, secretErr := clientSecret(env)
	ttl, ttlErr := duration(env, envvar.SessionTTL, auth.DefaultSessionTTL)
	idle, idleErr := duration(env, envvar.SessionIdle, auth.DefaultSessionIdle)
	operators, opErr := parseOperators(envvar.OIDCOperators.Get(env))
	cfg := loginSettings{
		issuer: envvar.OIDCIssuer.Get(env), clientID: envvar.OIDCClientID.Get(env), secret: secret,
		publicURL: envvar.PublicURL.Get(env), allowed: allowedEmails(envvar.OIDCAllowedEmails.Get(env)),
		ttl: ttl, idle: idle, operator: envvar.OperatorHandle.Get(env), operators: operators,
	}
	if cfg.operator == "" {
		cfg.operator = DefaultOperatorHandle
	}
	err := errors.Join(secretErr, ttlErr, idleErr, opErr, requireSet(env, envvar.OIDCClientID, envvar.OIDCAllowedEmails, envvar.PublicURL), validPublicURL(cfg.publicURL))
	if len(cfg.allowed) == 0 && err == nil {
		err = fmt.Errorf("%s names no email", envvar.OIDCAllowedEmails.Name())
	}
	return cfg, err
}

// requireSet fails for each of names that env leaves empty.
func requireSet(env Env, vars ...envvar.Var) error {
	var errs []error
	for _, v := range vars {
		if v.Get(env) == "" {
			errs = append(errs, fmt.Errorf("%s is required when %s is set", v.Name(), envvar.OIDCIssuer.Name()))
		}
	}
	return errors.Join(errs...)
}

// validPublicURL checks VH_PUBLIC_URL, when set, is an absolute http(s) URL.
func validPublicURL(raw string) error {
	if raw == "" {
		return nil
	}
	u, err := url.Parse(raw)
	if err != nil || (u.Scheme != "https" && u.Scheme != "http") || u.Host == "" {
		return fmt.Errorf("%s=%q: want an absolute http(s) URL", envvar.PublicURL.Name(), raw)
	}
	return nil
}

// clientSecret reads VH_OIDC_CLIENT_SECRET, or the file named by
// VH_OIDC_CLIENT_SECRET_FILE.
func clientSecret(env Env) (string, error) {
	if path := envvar.OIDCClientSecretFile.Get(env); path != "" {
		raw, err := os.ReadFile(path) //nolint:gosec // G304: the path is operator configuration (VH_OIDC_CLIENT_SECRET_FILE), not request input
		if err != nil {
			return "", fmt.Errorf("%s: %w", envvar.OIDCClientSecretFile.Name(), err)
		}
		return strings.TrimSpace(string(raw)), nil
	}
	if v := envvar.OIDCClientSecret.Get(env); v != "" {
		return v, nil
	}
	return "", fmt.Errorf("%s (or %s) is required when %s is set", envvar.OIDCClientSecret.Name(), envvar.OIDCClientSecretFile.Name(), envvar.OIDCIssuer.Name())
}

// allowedEmails parses the comma-separated list into lower-case emails.
func allowedEmails(list string) map[string]bool {
	out := map[string]bool{}
	for _, e := range strings.Split(list, ",") {
		if e = strings.ToLower(strings.TrimSpace(e)); e != "" {
			out[e] = true
		}
	}
	return out
}

// parseOperators parses VH_OIDC_OPERATORS (`email=handle,email=handle`) into a
// map of lower-case email to operator handle (SPEC 13). Entries are trimmed and
// empty ones skipped, like VH_OIDC_ALLOWED_EMAILS. Two emails may share a
// handle; one email may not name two. An entry without `=`, with an empty or
// space-holding email, or whose handle is not valid under SPEC 9 is an error,
// and every bad entry is reported.
func parseOperators(list string) (map[string]string, error) {
	out := map[string]string{}
	var errs []error
	for _, entry := range strings.Split(list, ",") {
		if entry = strings.TrimSpace(entry); entry == "" {
			continue
		}
		email, handle, err := parseOperatorEntry(entry)
		if err == nil && out[email] != "" && out[email] != handle {
			err = fmt.Errorf("%q: %s is already mapped to %q", entry, email, out[email])
		}
		if err != nil {
			errs = append(errs, fmt.Errorf("%s: %w", envvar.OIDCOperators.Name(), err))
			continue
		}
		out[email] = handle
	}
	return out, errors.Join(errs...)
}

// parseOperatorEntry splits one `email=handle` entry at its first `=`.
func parseOperatorEntry(entry string) (email, handle string, err error) {
	email, handle, found := strings.Cut(entry, "=")
	email, handle = strings.ToLower(strings.TrimSpace(email)), strings.TrimSpace(handle)
	switch {
	case !found:
		return "", "", fmt.Errorf("%q: want email=handle", entry)
	case !strings.Contains(email, "@") || strings.ContainsAny(email, " \t"):
		return "", "", fmt.Errorf("%q: %q is not an email", entry, email)
	}
	if err := domain.ValidateHandle(handle); err != nil {
		return "", "", fmt.Errorf("%q: %w", entry, err)
	}
	return email, handle, nil
}

// duration reads a Go duration from name, or returns def when it is unset.
func duration(env Env, name envvar.Var, def time.Duration) (time.Duration, error) {
	v := name.Get(env)
	if v == "" {
		return def, nil
	}
	d, err := time.ParseDuration(v)
	if err != nil || d <= 0 {
		return 0, fmt.Errorf("%s=%q: want a positive duration such as 24h", name.Name(), v)
	}
	return d, nil
}

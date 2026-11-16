package cli

import (
	"errors"
	"fmt"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/n-orlov/village-hall/internal/auth"
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
	ttl, idle                           time.Duration
	operator                            string
}

// loginFromEnv builds the browser login from the environment. With
// VH_OIDC_ISSUER unset, browser login is off (nil, nil); with it set, every
// other OIDC setting must be set too.
func loginFromEnv(env Env, st *store.Store) (*server.Login, error) {
	if envvar.OIDCIssuer.Get(env) == "" {
		return nil, nil
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
		PublicURL: cfg.publicURL,
		Operator:  cfg.operator,
	}, nil
}

func readLoginSettings(env Env) (loginSettings, error) {
	secret, secretErr := clientSecret(env)
	ttl, ttlErr := duration(env, envvar.SessionTTL, auth.DefaultSessionTTL)
	idle, idleErr := duration(env, envvar.SessionIdle, auth.DefaultSessionIdle)
	cfg := loginSettings{
		issuer: envvar.OIDCIssuer.Get(env), clientID: envvar.OIDCClientID.Get(env), secret: secret,
		publicURL: envvar.PublicURL.Get(env), allowed: allowedEmails(envvar.OIDCAllowedEmails.Get(env)),
		ttl: ttl, idle: idle, operator: envvar.OperatorHandle.Get(env),
	}
	if cfg.operator == "" {
		cfg.operator = DefaultOperatorHandle
	}
	err := errors.Join(secretErr, ttlErr, idleErr, requireSet(env, envvar.OIDCClientID, envvar.OIDCAllowedEmails, envvar.PublicURL), validPublicURL(cfg.publicURL))
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

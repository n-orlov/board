package cli

import (
	"errors"
	"fmt"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/n-orlov/board/internal/auth"
	"github.com/n-orlov/board/internal/oidc"
	"github.com/n-orlov/board/internal/server"
	"github.com/n-orlov/board/internal/store"
)

// DefaultOperatorHandle is the operator actor's handle when
// BOARD_OPERATOR_HANDLE is unset.
const DefaultOperatorHandle = "operator"

// loginSettings are the BOARD_* variables of browser login (SPEC 13).
type loginSettings struct {
	issuer, clientID, secret, publicURL string
	allowed                             map[string]bool
	ttl, idle                           time.Duration
	operator                            string
}

// loginFromEnv builds the browser login from the environment. With
// BOARD_OIDC_ISSUER unset, browser login is off (nil, nil); with it set, every
// other OIDC setting must be set too.
func loginFromEnv(env Env, st *store.Store) (*server.Login, error) {
	if env("BOARD_OIDC_ISSUER") == "" {
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
		Identity: provider,
		Sessions: auth.Sessions{Store: st, TTL: cfg.ttl, Idle: cfg.idle},
		Allowed:  cfg.allowed,
		Operator: cfg.operator,
	}, nil
}

func readLoginSettings(env Env) (loginSettings, error) {
	secret, secretErr := clientSecret(env)
	ttl, ttlErr := duration(env, "BOARD_SESSION_TTL", auth.DefaultSessionTTL)
	idle, idleErr := duration(env, "BOARD_SESSION_IDLE", auth.DefaultSessionIdle)
	cfg := loginSettings{
		issuer: env("BOARD_OIDC_ISSUER"), clientID: env("BOARD_OIDC_CLIENT_ID"), secret: secret,
		publicURL: env("BOARD_PUBLIC_URL"), allowed: allowedEmails(env("BOARD_OIDC_ALLOWED_EMAILS")),
		ttl: ttl, idle: idle, operator: env("BOARD_OPERATOR_HANDLE"),
	}
	if cfg.operator == "" {
		cfg.operator = DefaultOperatorHandle
	}
	err := errors.Join(secretErr, ttlErr, idleErr, requireSet(env, "BOARD_OIDC_CLIENT_ID", "BOARD_OIDC_ALLOWED_EMAILS", "BOARD_PUBLIC_URL"), validPublicURL(cfg.publicURL))
	if len(cfg.allowed) == 0 && err == nil {
		err = errors.New("BOARD_OIDC_ALLOWED_EMAILS names no email")
	}
	return cfg, err
}

// requireSet fails for each of names that env leaves empty.
func requireSet(env Env, names ...string) error {
	var errs []error
	for _, name := range names {
		if env(name) == "" {
			errs = append(errs, fmt.Errorf("%s is required when BOARD_OIDC_ISSUER is set", name))
		}
	}
	return errors.Join(errs...)
}

// validPublicURL checks BOARD_PUBLIC_URL, when set, is an absolute http(s) URL.
func validPublicURL(raw string) error {
	if raw == "" {
		return nil
	}
	u, err := url.Parse(raw)
	if err != nil || (u.Scheme != "https" && u.Scheme != "http") || u.Host == "" {
		return fmt.Errorf("BOARD_PUBLIC_URL=%q: want an absolute http(s) URL", raw)
	}
	return nil
}

// clientSecret reads BOARD_OIDC_CLIENT_SECRET, or the file named by
// BOARD_OIDC_CLIENT_SECRET_FILE.
func clientSecret(env Env) (string, error) {
	if path := env("BOARD_OIDC_CLIENT_SECRET_FILE"); path != "" {
		raw, err := os.ReadFile(path) //nolint:gosec // G304: the path is operator configuration (BOARD_OIDC_CLIENT_SECRET_FILE), not request input
		if err != nil {
			return "", fmt.Errorf("BOARD_OIDC_CLIENT_SECRET_FILE: %w", err)
		}
		return strings.TrimSpace(string(raw)), nil
	}
	if v := env("BOARD_OIDC_CLIENT_SECRET"); v != "" {
		return v, nil
	}
	return "", errors.New("BOARD_OIDC_CLIENT_SECRET (or BOARD_OIDC_CLIENT_SECRET_FILE) is required when BOARD_OIDC_ISSUER is set")
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
func duration(env Env, name string, def time.Duration) (time.Duration, error) {
	v := env(name)
	if v == "" {
		return def, nil
	}
	d, err := time.ParseDuration(v)
	if err != nil || d <= 0 {
		return 0, fmt.Errorf("%s=%q: want a positive duration such as 24h", name, v)
	}
	return d, nil
}

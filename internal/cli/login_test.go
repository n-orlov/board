package cli

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func envOf(m map[string]string) Env { return func(k string) string { return m[k] } }

func oidcEnv() map[string]string {
	return map[string]string{
		"BOARD_OIDC_ISSUER": "https://idp.test", "BOARD_OIDC_CLIENT_ID": "board", "BOARD_OIDC_CLIENT_SECRET": "s3cret",
		"BOARD_OIDC_ALLOWED_EMAILS": " Alice@Example.com, bob@example.com ,", "BOARD_PUBLIC_URL": "https://board.test/",
	}
}

func TestLoginIsOffWithoutAnIssuer(t *testing.T) {
	l, err := loginFromEnv(envOf(map[string]string{"BOARD_OIDC_CLIENT_ID": "x"}), nil)
	if l != nil || err != nil {
		t.Fatalf("loginFromEnv = %v, %v, want nil, nil", l, err)
	}
}

func TestLoginReadsEverySetting(t *testing.T) {
	env := oidcEnv()
	env["BOARD_SESSION_TTL"], env["BOARD_SESSION_IDLE"], env["BOARD_OPERATOR_HANDLE"] = "48h", "2h", "boss"
	l, err := loginFromEnv(envOf(env), nil)
	if err != nil {
		t.Fatal(err)
	}
	if !l.Allowed["alice@example.com"] || !l.Allowed["bob@example.com"] || len(l.Allowed) != 2 {
		t.Errorf("allowed %v", l.Allowed)
	}
	if l.Sessions.TTL != 48*time.Hour || l.Sessions.Idle != 2*time.Hour || l.Operator != "boss" {
		t.Errorf("ttl %v idle %v operator %q", l.Sessions.TTL, l.Sessions.Idle, l.Operator)
	}
}

func TestLoginDefaultsTheLifetimesAndTheOperator(t *testing.T) {
	l, err := loginFromEnv(envOf(oidcEnv()), nil)
	if err != nil || l.Operator != "operator" || l.Sessions.TTL != 168*time.Hour || l.Sessions.Idle != 24*time.Hour {
		t.Fatalf("login %+v, %v", l, err)
	}
}

func TestLoginReadsTheSecretFromAFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "secret")
	if err := os.WriteFile(path, []byte("from-file\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	env := oidcEnv()
	delete(env, "BOARD_OIDC_CLIENT_SECRET")
	env["BOARD_OIDC_CLIENT_SECRET_FILE"] = path
	if got, err := clientSecret(envOf(env)); err != nil || got != "from-file" {
		t.Fatalf("clientSecret = %q, %v", got, err)
	}
	env["BOARD_OIDC_CLIENT_SECRET_FILE"] = path + ".missing"
	if _, err := clientSecret(envOf(env)); err == nil {
		t.Fatal("a missing secret file was accepted")
	}
}

func TestLoginRefusesAnIncompleteOrMalformedConfiguration(t *testing.T) {
	cases := map[string]struct {
		set, drop, want string
	}{
		"no client id":      {"", "BOARD_OIDC_CLIENT_ID", "BOARD_OIDC_CLIENT_ID"},
		"no secret":         {"", "BOARD_OIDC_CLIENT_SECRET", "BOARD_OIDC_CLIENT_SECRET"},
		"no allowed emails": {"", "BOARD_OIDC_ALLOWED_EMAILS", "BOARD_OIDC_ALLOWED_EMAILS"},
		"no public url":     {"", "BOARD_PUBLIC_URL", "BOARD_PUBLIC_URL"},
		"relative url":      {"BOARD_PUBLIC_URL=board.test", "", "absolute"},
		"only commas":       {"BOARD_OIDC_ALLOWED_EMAILS=, ,", "", "names no email"},
		"bad ttl":           {"BOARD_SESSION_TTL=soon", "", "BOARD_SESSION_TTL"},
		"negative idle":     {"BOARD_SESSION_IDLE=-1h", "", "BOARD_SESSION_IDLE"},
	}
	for name, tc := range cases {
		env := oidcEnv()
		delete(env, tc.drop)
		if k, v, ok := strings.Cut(tc.set, "="); ok {
			env[k] = v
		}
		_, err := loginFromEnv(envOf(env), nil)
		if err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("%s: %v, want an error naming %q", name, err, tc.want)
		}
	}
}

func TestServeRefusesToStartWithAnIncompleteLoginConfiguration(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), serveRefusalBound)
	defer cancel()
	var errb bytes.Buffer
	env := envOf(map[string]string{"BOARD_OIDC_ISSUER": "https://idp.test"})
	if code := serveUntil(ctx, &errb, env, sqliteURL(t), "127.0.0.1:0", true); code != ExitError || !strings.Contains(errb.String(), "BOARD_OIDC_CLIENT_ID") {
		t.Fatalf("code=%d err=%q", code, errb.String())
	}
}

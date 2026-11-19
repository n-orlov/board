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
		"no client id":      {"", "BOARD_OIDC_CLIENT_ID", "VH_OIDC_CLIENT_ID"},
		"no secret":         {"", "BOARD_OIDC_CLIENT_SECRET", "VH_OIDC_CLIENT_SECRET"},
		"no allowed emails": {"", "BOARD_OIDC_ALLOWED_EMAILS", "VH_OIDC_ALLOWED_EMAILS"},
		"no public url":     {"", "BOARD_PUBLIC_URL", "VH_PUBLIC_URL"},
		"relative url":      {"BOARD_PUBLIC_URL=board.test", "", "absolute"},
		"only commas":       {"VH_OIDC_ALLOWED_EMAILS=, ,", "", "names no email"},
		"bad ttl":           {"VH_SESSION_TTL=soon", "", "VH_SESSION_TTL"},
		"negative idle":     {"BOARD_SESSION_IDLE=-1h", "", "VH_SESSION_IDLE"},
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
	if code := serveUntil(ctx, &errb, env, sqliteURL(t), "127.0.0.1:0", true); code != ExitError || !strings.Contains(errb.String(), "VH_OIDC_CLIENT_ID") {
		t.Fatalf("code=%d err=%q", code, errb.String())
	}
}

func TestParseOperatorsReadsEmailHandlePairs(t *testing.T) {
	t.Parallel()
	got, err := parseOperators(" Alice@Example.com = ann , bob@example.com=shared,carol@example.com=shared,, ")
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]string{"alice@example.com": "ann", "bob@example.com": "shared", "carol@example.com": "shared"}
	if len(got) != len(want) {
		t.Fatalf("parseOperators = %v, want %v", got, want)
	}
	for email, handle := range want {
		if got[email] != handle {
			t.Errorf("%s maps to %q, want %q", email, got[email], handle)
		}
	}
	if none, err := parseOperators(""); err != nil || len(none) != 0 {
		t.Errorf("an empty setting = %v, %v, want nothing and no error", none, err)
	}
	if same, err := parseOperators("a@example.com=x,A@example.com=x"); err != nil || same["a@example.com"] != "x" {
		t.Errorf("one email twice for one handle = %v, %v, want it accepted", same, err)
	}
}

func TestParseOperatorsRefusesAMalformedEntryAndNamesIt(t *testing.T) {
	t.Parallel()
	for name, tc := range map[string]struct{ in, want string }{
		"missing equals":         {"alice@example.com", `"alice@example.com": want email=handle`},
		"invalid handle":         {"alice@example.com=a b", "no whitespace"},
		"empty handle":           {"alice@example.com=", "not empty"},
		"handle too long":        {"alice@example.com=" + strings.Repeat("h", 101), "at most 100"},
		"empty email":            {"=ann", "is not an email"},
		"email without at sign":  {"alice=ann", "is not an email"},
		"email with a space":     {"al ice@example.com=ann", "is not an email"},
		"one email, two handles": {"alice@example.com=ann,ALICE@example.com=bea", `is already mapped to "ann"`},
	} {
		_, err := parseOperators(tc.in)
		if err == nil || !strings.Contains(err.Error(), tc.want) || !strings.Contains(err.Error(), "VH_OIDC_OPERATORS") {
			t.Errorf("%s: parseOperators(%q) = %v, want an error naming VH_OIDC_OPERATORS and saying %q", name, tc.in, err, tc.want)
		}
	}
	_, err := parseOperators("good@example.com=ok,bad,worse=x")
	if err == nil || !strings.Contains(err.Error(), `"bad"`) || !strings.Contains(err.Error(), `"worse=x"`) {
		t.Errorf("two bad entries = %v, want both named", err)
	}
}

func TestLoginCarriesTheOperatorMapAndRefusesAMalformedOne(t *testing.T) {
	t.Parallel()
	env := oidcEnv()
	env["VH_OIDC_OPERATORS"] = "Alice@example.com=ann,bob@example.com=bea"
	l, err := loginFromEnv(envOf(env), nil)
	if err != nil || l.Operators["alice@example.com"] != "ann" || l.Operators["bob@example.com"] != "bea" || l.ActorFor("zed@example.com") != "operator" {
		t.Fatalf("login %+v, %v", l, err)
	}
	env["BOARD_OIDC_OPERATORS"] = "ignored"
	if l, err = loginFromEnv(envOf(env), nil); err != nil || l.Operators["bob@example.com"] != "bea" {
		t.Errorf("VH_ must win over BOARD_: %+v, %v", l, err)
	}
	env["VH_OIDC_OPERATORS"] = "alice@example.com"
	if _, err := loginFromEnv(envOf(env), nil); err == nil || !strings.Contains(err.Error(), "VH_OIDC_OPERATORS") {
		t.Errorf("a malformed mapping with login on: %v", err)
	}
	off := map[string]string{"VH_OIDC_OPERATORS": "alice@example.com"}
	if l, err := loginFromEnv(envOf(off), nil); l != nil || err == nil || !strings.Contains(err.Error(), "VH_OIDC_OPERATORS") {
		t.Errorf("a malformed mapping with login off: %v, %v", l, err)
	}
}

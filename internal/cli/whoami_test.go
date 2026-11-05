package cli

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

const whoamiKey = "brd_abcdefgh" + "0000000000000000000000000000000000000000"

func TestWhoamiPrintsHandleKeyNameAndServerOK(t *testing.T) {
	f := &fakeBoard{handler: func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"items":[{"name":"other","display_prefix":"zzzzzzzz"},{"name":"workstation","display_prefix":"abcdefgh"}]}`))
	}}
	srv := httptest.NewServer(f)
	defer srv.Close()
	code, out, errs := runEnv(f.env(srv), "whoami")
	if code != ExitOK || errs != "" || out != "you: claude:test (key workstation)  way back: wb  server ok\n" {
		t.Fatalf("got %d %q %q", code, out, errs)
	}
	if f.requests[0].URL.Path != "/api/v1/keys" || strings.Contains(out, "0000") {
		t.Fatalf("request %v, out %q", f.requests[0].URL, out)
	}
}

func TestWhoamiWithTheFallbackActor(t *testing.T) {
	withHost(t, "box", nil, 99, "/w", nil)
	f := &fakeBoard{handler: func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte(`{"items":[]}`)) }}
	srv := httptest.NewServer(f)
	defer srv.Close()
	env := envWith(map[string]string{"BOARD_URL": srv.URL, "BOARD_API_KEY": whoamiKey})
	code, out, _ := runEnv(env, "whoami")
	if code != ExitOK || out != "you: agent:box:99 (key unknown)  way back: pid 99 on box, cwd /w  server ok\n" {
		t.Fatalf("got %d %q", code, out)
	}
	if f.requests[0].Header.Get("Board-Actor") != "agent:box:99" || f.requests[0].Header.Get("Board-Actor-Way-Back") != "pid 99 on box, cwd /w" {
		t.Fatalf("headers %v", f.requests[0].Header)
	}
}

func TestWhoamiOmitsAnEmptyWayBack(t *testing.T) {
	f := &fakeBoard{handler: func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte(`{"items":[]}`)) }}
	srv := httptest.NewServer(f)
	defer srv.Close()
	env := envWith(map[string]string{"BOARD_URL": srv.URL, "BOARD_API_KEY": "brd_short", "BOARD_ACTOR": "me"})
	if _, out, _ := runEnv(env, "whoami"); out != "you: me (key unknown)  server ok\n" {
		t.Fatalf("got %q", out)
	}
}

func TestWhoamiRefusals(t *testing.T) {
	f := &fakeBoard{handler: func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(401)
		_, _ = w.Write([]byte(`{"detail":"key revoked","next":"ask the operator for a new key"}`))
	}}
	srv := httptest.NewServer(f)
	defer srv.Close()
	if code, out, errs := runEnv(f.env(srv), "whoami"); code != ExitAuth || out != "" || errs != "key revoked → ask the operator for a new key\n" {
		t.Fatalf("got %d %q %q", code, out, errs)
	}
	for _, args := range [][]string{{"whoami", "extra"}, {"whoami", "--json"}} {
		if code, out, errs := runEnv(f.env(srv), args...); code != ExitUsage || out != "" || errs != WhoamiUsage+"\n" {
			t.Fatalf("%v: got %d %q %q", args, code, out, errs)
		}
	}
}

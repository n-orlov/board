package server

import (
	"context"
	"errors"
	"io/fs"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/n-orlov/board/internal/auth"
	"github.com/n-orlov/board/internal/webui"
)

func ok(context.Context) error { return nil }

// testKey is a well-formed key the accepting verifier knows.
const testKey = "brd_00000000000000000000000000000000000000000000000000ab"

func acceptKey(_ context.Context, key string) (string, error) {
	if key != testKey {
		return "", auth.ErrKeyUnknown
	}
	return "test", nil
}

// authed sends a GET request with the test key and an actor.
func authed(path string) *http.Request {
	r := httptest.NewRequest(http.MethodGet, path, nil)
	r.Header.Set("Authorization", "Bearer "+testKey)
	r.Header.Set(ActorHeader, "tester")
	return r
}

func TestServeListensOnConfiguredAddressAndStopsOnCancel(t *testing.T) {
	probe, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := probe.Addr().String()
	_ = probe.Close()

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- Serve(ctx, addr, ok, acceptKey, nil) }()

	deadline := time.Now().Add(5 * time.Second)
	for {
		conn, err := net.Dial("tcp", addr)
		if err == nil {
			_ = conn.Close()
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("server never listened on %s: %v", addr, err)
		}
		time.Sleep(20 * time.Millisecond)
	}
	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("Serve returned %v on clean shutdown", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Serve did not return after cancel")
	}
}

func TestServeReportsBusyAddress(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	if err := Serve(context.Background(), ln.Addr().String(), ok, acceptKey, nil); err == nil {
		t.Fatal("expected an error for an address already in use")
	}
}

func TestHealthzIsAlwaysOKAndReadyzFollowsReadiness(t *testing.T) {
	notReady := func(context.Context) error { return errors.New("down") }
	cases := []struct {
		name  string
		ready Readiness
		path  string
		want  int
	}{
		{"healthz ignores a failing readiness", notReady, "/healthz", http.StatusOK},
		{"readyz is 200 when ready", ok, "/readyz", http.StatusOK},
		{"readyz is 503 when not ready", notReady, "/readyz", http.StatusServiceUnavailable},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rec := httptest.NewRecorder()
			NewHandler(tc.ready, acceptKey, nil).ServeHTTP(rec, authed(tc.path))
			if rec.Code != tc.want {
				t.Fatalf("GET %s = %d, want %d", tc.path, rec.Code, tc.want)
			}
		})
	}
}

func TestRootServesTheEmbeddedIndexHTML(t *testing.T) {
	want, err := fs.ReadFile(webui.Files(), "index.html")
	if err != nil {
		t.Fatalf("the embedded UI has no index.html: %v", err)
	}
	rec := httptest.NewRecorder()
	NewHandler(ok, acceptKey, nil).ServeHTTP(rec, authed("/"))
	if rec.Code != http.StatusOK {
		t.Fatalf("GET / = %d, want 200", rec.Code)
	}
	if ct := rec.Header().Get("Content-Type"); !strings.HasPrefix(ct, "text/html") {
		t.Errorf("GET / Content-Type = %q, want text/html", ct)
	}
	if got := rec.Body.String(); got != string(want) {
		t.Errorf("GET / body is not the embedded index.html:\n got %q\nwant %q", got, want)
	}
	if !strings.Contains(rec.Body.String(), `<div id="app">`) {
		t.Errorf("GET / body has no app mount point: %q", rec.Body.String())
	}
}

func TestProbesAreNotShadowedByTheUI(t *testing.T) {
	for _, path := range []string{"/healthz", "/readyz"} {
		rec := httptest.NewRecorder()
		NewHandler(ok, acceptKey, nil).ServeHTTP(rec, authed(path))
		if rec.Code != http.StatusOK || rec.Body.String() != "ok\n" {
			t.Errorf("GET %s = %d %q, want 200 \"ok\\n\"", path, rec.Code, rec.Body.String())
		}
	}
}

func TestUnknownPathIs404NotTheIndex(t *testing.T) {
	rec := httptest.NewRecorder()
	NewHandler(ok, acceptKey, nil).ServeHTTP(rec, authed("/no-such-asset.js"))
	if rec.Code != http.StatusNotFound {
		t.Errorf("GET /no-such-asset.js = %d, want 404", rec.Code)
	}
}

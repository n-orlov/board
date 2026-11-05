package server

import (
	"context"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func ok(context.Context) error { return nil }

func TestServeListensOnConfiguredAddressAndStopsOnCancel(t *testing.T) {
	probe, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := probe.Addr().String()
	probe.Close()

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- Serve(ctx, addr, ok) }()

	deadline := time.Now().Add(5 * time.Second)
	for {
		conn, err := net.Dial("tcp", addr)
		if err == nil {
			conn.Close()
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
	if err := Serve(context.Background(), ln.Addr().String(), ok); err == nil {
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
			Handler(tc.ready).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, tc.path, nil))
			if rec.Code != tc.want {
				t.Fatalf("GET %s = %d, want %d", tc.path, rec.Code, tc.want)
			}
		})
	}
}

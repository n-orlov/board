package server

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"time"

	"github.com/n-orlov/board/internal/auth"
	"github.com/n-orlov/board/internal/webui"
)

// DefaultListen is the listen address when BOARD_LISTEN is unset.
const DefaultListen = ":8080"

// Readiness reports nil when the service can take traffic: the database is
// reachable and migrated.
type Readiness func(ctx context.Context) error

// NewHandler returns the HTTP handler the server serves. GET /healthz answers 200
// whenever the process is up and needs no credential. Every other request needs
// a Bearer key that verify accepts and a Board-Actor header (401, 400 or 503
// otherwise). GET /readyz answers 200 when ready returns nil and 503
// otherwise. Every other GET is answered from the web UI embedded in the
// binary, so GET / is its index.html.
//
// login, when not nil, turns on browser login (SPEC 3.2): a request with a valid
// session cookie is authenticated as the operator, and a request with no
// credential that is a browser navigation is sent to /auth/login.
//
// keys is the store behind /api/v1/keys; nil answers those operations with 503.
func NewHandler(ready Readiness, verify KeyVerifier, login *Login, keys *auth.Keys) http.Handler {
	mux := http.NewServeMux()
	HandlerFromMux(strictOperations(operations{ready: ready, loginOps: loginOps{login: login}, keyOps: keyOps{keys: keys}}), mux)
	mux.Handle("GET /", http.FileServerFS(webui.Files()))
	return authenticated(mux, verify, login)
}

// Serve listens on addr and serves NewHandler(ready, verify, login, keys) until ctx is cancelled, then shuts
// down gracefully. It returns nil on a clean shutdown.
func Serve(ctx context.Context, addr string, ready Readiness, verify KeyVerifier, login *Login, keys *auth.Keys) error {
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return fmt.Errorf("listen on %s: %w", addr, err)
	}
	return serveOn(ctx, ln, ready, verify, login, keys)
}

func serveOn(ctx context.Context, ln net.Listener, ready Readiness, verify KeyVerifier, login *Login, keys *auth.Keys) error {
	srv := &http.Server{Handler: NewHandler(ready, verify, login, keys), ReadHeaderTimeout: 10 * time.Second}
	errc := make(chan error, 1)
	go func() { errc <- srv.Serve(ln) }()
	select {
	case err := <-errc:
		return err
	case <-ctx.Done():
	}
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := srv.Shutdown(shutdownCtx); err != nil {
		return err
	}
	if err := <-errc; err != nil && !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	return nil
}

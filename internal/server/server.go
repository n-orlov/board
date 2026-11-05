package server

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"time"

	"github.com/n-orlov/board/internal/webui"
)

// DefaultListen is the listen address when BOARD_LISTEN is unset.
const DefaultListen = ":8080"

// Readiness reports nil when the service can take traffic: the database is
// reachable and migrated.
type Readiness func(ctx context.Context) error

// Handler returns the HTTP handler the server serves. GET /healthz answers 200
// whenever the process is up. GET /readyz answers 200 when ready returns nil
// and 503 otherwise. Every other GET is answered from the web UI embedded in the
// binary, so GET / is its index.html.
func Handler(ready Readiness) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) {
		writeText(w, http.StatusOK, "ok\n")
	})
	mux.HandleFunc("GET /readyz", func(w http.ResponseWriter, r *http.Request) {
		ctx, cancel := context.WithTimeout(r.Context(), 3*time.Second)
		defer cancel()
		if err := ready(ctx); err != nil {
			writeText(w, http.StatusServiceUnavailable, "not ready\n")
			return
		}
		writeText(w, http.StatusOK, "ok\n")
	})
	mux.Handle("GET /", http.FileServerFS(webui.Files()))
	return mux
}

func writeText(w http.ResponseWriter, status int, body string) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.WriteHeader(status)
	_, _ = w.Write([]byte(body))
}

// Serve listens on addr and serves Handler(ready) until ctx is cancelled, then shuts
// down gracefully. It returns nil on a clean shutdown.
func Serve(ctx context.Context, addr string, ready Readiness) error {
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return fmt.Errorf("listen on %s: %w", addr, err)
	}
	return serveOn(ctx, ln, ready)
}

func serveOn(ctx context.Context, ln net.Listener, ready Readiness) error {
	srv := &http.Server{Handler: Handler(ready), ReadHeaderTimeout: 10 * time.Second}
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

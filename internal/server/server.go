package server

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"time"
)

// DefaultListen is the listen address when BOARD_LISTEN is unset.
const DefaultListen = ":8080"

// Handler returns the HTTP handler the server serves.
func Handler() http.Handler {
	return http.NewServeMux()
}

// Serve listens on addr and serves Handler until ctx is cancelled, then shuts
// down gracefully. It returns nil on a clean shutdown.
func Serve(ctx context.Context, addr string) error {
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return fmt.Errorf("listen on %s: %w", addr, err)
	}
	return serveOn(ctx, ln)
}

func serveOn(ctx context.Context, ln net.Listener) error {
	srv := &http.Server{Handler: Handler(), ReadHeaderTimeout: 10 * time.Second}
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

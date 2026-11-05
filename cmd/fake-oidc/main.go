// Command fake-oidc runs the mock OIDC provider (internal/oidc/oidctest) as a process, for the
// Playwright specs: e2e/serve.sh starts it beside the real binary. It is test-only like the
// package it wraps: cmd/board never imports either (ci/repocheck checks `go list -deps`).
//
//	FAKE_OIDC_CLIENT_SECRET=... fake-oidc -client-id board -email who@example.test
//
// It prints the issuer URL on one line and serves until it receives SIGINT or SIGTERM. The
// signing key is generated at start and the client secret is read from the environment, so
// nothing secret is ever written down.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"syscall"

	"github.com/n-orlov/board/internal/oidc/oidctest"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	code := exitCode(ctx, os.Args[1:], os.Getenv, os.Stdout, os.Stderr)
	stop()
	os.Exit(code)
}

// exitCode runs the provider until ctx is done and returns the process's exit status.
func exitCode(ctx context.Context, args []string, getenv func(string) string, stdout, stderr io.Writer) int {
	if err := run(ctx, args, getenv, stdout); err != nil {
		_, _ = fmt.Fprintln(stderr, "fake-oidc:", err)
		return 1
	}
	return 0
}

// run starts the provider, prints its URL to stdout, and serves until ctx is done.
func run(ctx context.Context, args []string, getenv func(string) string, stdout io.Writer) error {
	fs := flag.NewFlagSet("fake-oidc", flag.ContinueOnError)
	clientID := fs.String("client-id", "board", "the client ID the provider serves")
	email := fs.String("email", "", "the verified email the provider signs in")
	if err := fs.Parse(args); err != nil {
		return err
	}
	secret := getenv("FAKE_OIDC_CLIENT_SECRET")
	if secret == "" || *email == "" {
		return errors.New("FAKE_OIDC_CLIENT_SECRET and -email are required")
	}
	p, err := oidctest.New(*clientID, secret)
	if err != nil {
		return err
	}
	defer p.Close()
	p.SignIn(*email, true)
	if _, err := fmt.Fprintln(stdout, p.URL()); err != nil {
		return err
	}
	<-ctx.Done()
	return nil
}

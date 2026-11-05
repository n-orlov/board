package cli

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/signal"
	"syscall"

	"github.com/n-orlov/board/internal/server"
	"github.com/n-orlov/board/internal/version"
)

// Exit codes (SPEC §8.1).
const (
	ExitOK    = 0
	ExitError = 1
	ExitUsage = 2
)

// Usage is the single line printed to stderr for a usage error.
const Usage = "usage: board <serve|version>"

// Env looks up an environment variable.
type Env func(string) string

// Run executes the board command line and returns the process exit code.
func Run(args []string, stdout, stderr io.Writer, env Env) int {
	if len(args) == 0 {
		return usage(stderr)
	}
	switch args[0] {
	case "version":
		if len(args) != 1 {
			return usage(stderr)
		}
		fmt.Fprintln(stdout, version.Current())
		return ExitOK
	case "serve":
		if len(args) != 1 {
			return usage(stderr)
		}
		return serve(stderr, env)
	default:
		return usage(stderr)
	}
}

func usage(stderr io.Writer) int {
	fmt.Fprintln(stderr, Usage)
	return ExitUsage
}

func serve(stderr io.Writer, env Env) int {
	addr := env("BOARD_LISTEN")
	if addr == "" {
		addr = server.DefaultListen
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err := server.Serve(ctx, addr); err != nil {
		fmt.Fprintf(stderr, "board: %v\n", err)
		return ExitError
	}
	return ExitOK
}

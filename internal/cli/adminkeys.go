package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/n-orlov/board/internal/auth"
	"github.com/n-orlov/board/internal/store"
)

// KeysUsage is the single line printed to stderr for a malformed
// `board admin keys` command line.
const KeysUsage = "usage: board admin keys create <name> [--expires 90d] | list | revoke <name>"

// ExitNotFound is the exit code for an unknown name (SPEC §8.1).
const ExitNotFound = 4

// adminCreatedBy is the created_by of a key made from the command line.
const adminCreatedBy = "admin-cli"

const dateLayout = "2006-01-02"

// ParseExpiry reads an --expires value (see auth.ParseExpiry).
func ParseExpiry(s string) (time.Duration, error) {
	d, err := auth.ParseExpiry(s)
	if err != nil {
		return 0, fmt.Errorf("--expires %w", err)
	}
	return d, nil
}

// parseCreate reads `<name> [--expires D]` in either order.
func parseCreate(args []string) (name string, ttl time.Duration, err error) {
	for i := 0; i < len(args); i++ {
		name, ttl, i, err = createArg(args, i, name, ttl)
		if err != nil {
			return "", 0, err
		}
	}
	if name == "" {
		return "", 0, errUsage
	}
	return name, ttl, nil
}

// createArg reads args[i], which is the name or an --expires flag with its
// value, and returns the state after it and the index of the last argument it
// consumed.
func createArg(args []string, i int, name string, ttl time.Duration) (string, time.Duration, int, error) {
	var err error
	switch a := args[i]; {
	case a == "--expires" && i+1 < len(args):
		i++
		ttl, err = ParseExpiry(args[i])
	case strings.HasPrefix(a, "--expires="):
		ttl, err = ParseExpiry(strings.TrimPrefix(a, "--expires="))
	case strings.HasPrefix(a, "-") || name != "":
		err = errUsage
	default:
		name = a
	}
	return name, ttl, i, err
}

var errUsage = errors.New("usage")

// keysAction is one `board admin keys` subcommand, parsed and ready to run on
// an open database.
type keysAction func(context.Context, auth.Keys) int

// parseKeys reads the subcommand and its arguments.
func parseKeys(args []string, stdout, stderr io.Writer) (keysAction, error) {
	if len(args) == 0 {
		return nil, errUsage
	}
	switch rest := args[1:]; args[0] {
	case "create":
		name, ttl, err := parseCreate(rest)
		if err != nil {
			return nil, err
		}
		return func(ctx context.Context, k auth.Keys) int { return createKey(ctx, k, stdout, stderr, name, ttl) }, nil
	case "list":
		if len(rest) == 0 {
			return func(ctx context.Context, k auth.Keys) int { return listKeys(ctx, k, stdout, stderr) }, nil
		}
	case "revoke":
		if len(rest) == 1 {
			return func(ctx context.Context, k auth.Keys) int { return revokeKey(ctx, k, stdout, stderr, rest[0]) }, nil
		}
	}
	return nil, errUsage
}

// adminKeys runs `board admin keys ...` against the database directly.
func adminKeys(args []string, stdout, stderr io.Writer, env Env) int {
	run, err := parseKeys(args, stdout, stderr)
	if err != nil {
		return keysArgError(stderr, err)
	}
	return withKeys(env, stderr, run)
}

func keysUsage(stderr io.Writer) int {
	_, _ = fmt.Fprintln(stderr, KeysUsage)
	return ExitUsage
}

func keysArgError(stderr io.Writer, err error) int {
	if errors.Is(err, errUsage) {
		return keysUsage(stderr)
	}
	_, _ = fmt.Fprintf(stderr, "board: %v\n", err)
	return ExitUsage
}

// withKeys opens the migrated database named by VH_DB_URL and runs f.
func withKeys(env Env, stderr io.Writer, f keysAction) int {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	st, err := store.Open(dbURL(env))
	if err != nil {
		return fail(stderr, err)
	}
	defer func() { _ = st.Close() }()
	if err := st.Ready(ctx); err != nil {
		if errors.Is(err, store.ErrNotMigrated) {
			err = fmt.Errorf("%w → board admin migrate", err)
		}
		return fail(stderr, err)
	}
	return f(ctx, auth.Keys{Store: st})
}

func createKey(ctx context.Context, k auth.Keys, stdout, stderr io.Writer, name string, ttl time.Duration) int {
	plain, row, err := k.Create(ctx, name, adminCreatedBy, ttl)
	switch {
	case errors.Is(err, auth.ErrInvalidName):
		return keysArgError(stderr, err)
	case errors.Is(err, auth.ErrKeyExists):
		_, _ = fmt.Fprintf(stderr, "board: key %q already exists → board admin keys list\n", name)
		return ExitError
	case err != nil:
		return fail(stderr, err)
	}
	exp := ""
	if row.ExpiresAt != nil {
		exp = " exp " + row.ExpiresAt.Format(dateLayout)
	}
	_, _ = fmt.Fprintf(stdout, "ok key %s created%s %s\n", row.Name, exp, plain)
	return ExitOK
}

func listKeys(ctx context.Context, k auth.Keys, stdout, stderr io.Writer) int {
	keys, err := k.List(ctx)
	if err != nil {
		return fail(stderr, err)
	}
	now := time.Now().UTC()
	for _, key := range keys {
		_, _ = fmt.Fprintln(stdout, keyLine(key, now))
	}
	return ExitOK
}

// keyLine is one list line: name, display prefix, status, then the dates the
// key has.
func keyLine(key auth.Key, now time.Time) string {
	line := fmt.Sprintf("%s %s%s %s created %s", key.Name, auth.KeyPrefix, key.DisplayPrefix,
		key.Status(now), key.CreatedAt.Format(dateLayout))
	for _, d := range []struct {
		label string
		at    *time.Time
	}{{"exp", key.ExpiresAt}, {"used", key.LastUsedAt}, {"revoked", key.RevokedAt}} {
		if d.at != nil {
			line += " " + d.label + " " + d.at.Format(dateLayout)
		}
	}
	return line
}

func revokeKey(ctx context.Context, k auth.Keys, stdout, stderr io.Writer, name string) int {
	already, err := k.Revoke(ctx, name)
	switch {
	case errors.Is(err, auth.ErrKeyNotFound):
		_, _ = fmt.Fprintf(stderr, "board: key %q not found → board admin keys list\n", name)
		return ExitNotFound
	case err != nil:
		return fail(stderr, err)
	case already:
		_, _ = fmt.Fprintf(stdout, "ok key %s already revoked\n", name)
	default:
		_, _ = fmt.Fprintf(stdout, "ok key %s revoked\n", name)
	}
	return ExitOK
}

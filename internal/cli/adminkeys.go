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

	"github.com/n-orlov/village-hall/internal/auth"
	"github.com/n-orlov/village-hall/internal/domain"
	"github.com/n-orlov/village-hall/internal/envvar"
	"github.com/n-orlov/village-hall/internal/store"
)

// KeysUsage is the single line printed to stderr for a malformed
// `vh admin keys` command line.
const KeysUsage = "usage: vh admin keys create <name> [--expires 90d] [--owner <handle>] | list | revoke <name>"

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

// createOpts is what `vh admin keys create` was asked for: the key's name, its
// lifetime and the handle of the operator that owns it (empty: the default).
type createOpts struct {
	name  string
	ttl   time.Duration
	owner string
	// defaultOwner is VH_OPERATOR_HANDLE, the owner when --owner is not given.
	defaultOwner string
}

// parseCreate reads `<name> [--expires D] [--owner HANDLE]` in any order.
func parseCreate(args []string) (createOpts, error) {
	var o createOpts
	for i := 0; i < len(args); i++ {
		var err error
		if o, i, err = createArg(args, i, o); err != nil {
			return createOpts{}, err
		}
	}
	if o.name == "" {
		return createOpts{}, errUsage
	}
	return o, nil
}

// createArg reads args[i], which is the name or a flag with its value, and
// returns the options after it and the index of the last argument it consumed.
func createArg(args []string, i int, o createOpts) (createOpts, int, error) {
	var err error
	switch a := args[i]; {
	case (a == "--expires" || a == "--owner") && i+1 < len(args):
		i++
		o, err = createFlag(o, a, args[i])
	case strings.HasPrefix(a, "--expires="), strings.HasPrefix(a, "--owner="):
		flag, value, _ := strings.Cut(a, "=")
		o, err = createFlag(o, flag, value)
	case strings.HasPrefix(a, "-") || o.name != "":
		err = errUsage
	default:
		o.name = a
	}
	return o, i, err
}

// createFlag sets the option the flag names to its value.
func createFlag(o createOpts, flag, value string) (createOpts, error) {
	if flag == "--owner" {
		if err := domain.ValidateHandle(value); err != nil {
			return o, fmt.Errorf("--owner %w", err)
		}
		o.owner = value
		return o, nil
	}
	var err error
	o.ttl, err = ParseExpiry(value)
	return o, err
}

var errUsage = errors.New("usage")

// keysAction is one `vh admin keys` subcommand, parsed and ready to run on
// an open database.
type keysAction func(context.Context, auth.Keys) int

// parseKeys reads the subcommand and its arguments.
func parseKeys(args []string, stdout, stderr io.Writer, env Env) (keysAction, error) {
	if len(args) == 0 {
		return nil, errUsage
	}
	switch rest := args[1:]; args[0] {
	case "create":
		opts, err := parseCreate(rest)
		if err != nil {
			return nil, err
		}
		opts.defaultOwner = envvar.OperatorHandle.Get(env)
		return func(ctx context.Context, k auth.Keys) int { return createKey(ctx, k, stdout, stderr, opts) }, nil
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

// adminKeys runs `vh admin keys ...` against the database directly.
func adminKeys(args []string, stdout, stderr io.Writer, env Env) int {
	run, err := parseKeys(args, stdout, stderr, env)
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
	_, _ = fmt.Fprintf(stderr, "vh: %v\n", err)
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
			err = fmt.Errorf("%w → vh admin migrate", err)
		}
		return fail(stderr, err)
	}
	return f(ctx, auth.Keys{Store: st})
}

// makeKey creates the key. An --owner that names no operator is refused and
// changes nothing. Without --owner the owner is the operator VH_OPERATOR_HANDLE
// names (default "operator"); when no such operator exists yet (a database
// bootstrapped before anyone signed in) the key has no owner.
func makeKey(ctx context.Context, k auth.Keys, o createOpts) (string, auth.Key, error) {
	if o.owner != "" {
		return k.CreateOwned(ctx, o.name, adminCreatedBy, o.owner, o.ttl)
	}
	owner := o.defaultOwner
	if owner == "" {
		owner = DefaultOperatorHandle
	}
	plain, row, err := k.CreateOwned(ctx, o.name, adminCreatedBy, owner, o.ttl)
	if errors.Is(err, auth.ErrOwnerNotOperator) {
		return k.Create(ctx, o.name, adminCreatedBy, o.ttl)
	}
	return plain, row, err
}

func createKey(ctx context.Context, k auth.Keys, stdout, stderr io.Writer, o createOpts) int {
	name := o.name
	plain, row, err := makeKey(ctx, k, o)
	switch {
	case errors.Is(err, auth.ErrOwnerNotOperator):
		_, _ = fmt.Fprintf(stderr, "vh: --owner %q is not an operator (an operator exists once its browser session has signed in)\n", o.owner)
		return ExitUsage
	case errors.Is(err, auth.ErrInvalidName):
		return keysArgError(stderr, err)
	case errors.Is(err, auth.ErrKeyExists):
		_, _ = fmt.Fprintf(stderr, "vh: key %q already exists → vh admin keys list\n", name)
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
		_, _ = fmt.Fprintf(stderr, "vh: key %q not found → vh admin keys list\n", name)
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

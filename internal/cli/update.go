package cli

import (
	"fmt"
	"io"
	"os"
	"runtime"

	upd "github.com/n-orlov/village-hall/internal/update"
	"github.com/n-orlov/village-hall/internal/version"
)

// UpdateUsage is the single stderr line for a malformed `vh update`.
const UpdateUsage = "usage: vh update [--version vX.Y.Z] [--check]"

var updateFlags = []flagSpec{{"--version", true}, {"--check", false}}

// currentVersion and executable are the running binary's identity. Tests replace them.
var (
	currentVersion = version.Current
	executable     = os.Executable
)

// updateCmd runs `vh update [--version vX.Y.Z] [--check]`: it replaces this binary with the
// latest release (or the pinned one), or with --check only says whether that would change it.
// Every refusal exits 1 with a `→` line and leaves the binary as it was.
func updateCmd(args []string, stdout, stderr io.Writer, env Env) int {
	p, ok := parseArgs(args, updateFlags)
	if !ok || len(p.pos) != 0 || len(p.values["--version"]) > 1 {
		return usageLine(stderr, UpdateUsage)
	}
	o := updateOptions(env)
	if pins := p.values["--version"]; len(pins) == 1 {
		o.Pinned = pins[0]
	}
	ctx, stop := clientContext()
	defer stop()
	if p.has("--check") {
		return finishUpdate(stdout, stderr, o, true)(upd.Check(ctx, o))
	}
	return finishUpdate(stdout, stderr, o, false)(upd.Run(ctx, o))
}

// updateOptions reads the release source from the environment: the repository, the two base
// URLs and the immutability opt-out (exactly 0), the same variables install.sh reads.
func updateOptions(env Env) upd.Options {
	exe, _ := executable()
	return upd.Options{
		Current:          currentVersion(),
		Exe:              exe,
		Repo:             orDefault(env("VH_REPO"), upd.DefaultRepo),
		APIBase:          orDefault(env("VH_API_URL"), upd.DefaultAPIBase),
		DownloadBase:     orDefault(env("VH_DOWNLOAD_URL"), upd.DefaultDownloadURL),
		RequireImmutable: env("VH_REQUIRE_IMMUTABLE") != "0",
		OS:               runtime.GOOS,
		Arch:             runtime.GOARCH,
	}
}

func orDefault(v, def string) string {
	if v == "" {
		return def
	}
	return v
}

// finishUpdate returns the function that prints what upd.Check or upd.Run found, or
// the refusal, and picks the exit code.
func finishUpdate(stdout, stderr io.Writer, o upd.Options, check bool) func(upd.Outcome, error) int {
	return func(out upd.Outcome, err error) int {
		if err != nil {
			return report(stderr, err)
		}
		if out.Immutable != "true" && !o.RequireImmutable {
			_, _ = fmt.Fprintln(stderr, "vh update: note: VH_REQUIRE_IMMUTABLE=0, the release is not confirmed immutable")
		}
		_, _ = fmt.Fprintln(stdout, updateLine(out, check))
		return ExitOK
	}
}

// updateLine is the one line `vh update` prints on success.
func updateLine(out upd.Outcome, check bool) string {
	switch {
	case !out.Newer:
		return fmt.Sprintf("up to date (%s)", out.Current)
	case check:
		return fmt.Sprintf("update available: %s → %s", out.Current, out.Tag)
	default:
		return fmt.Sprintf("updated: %s → %s (%s)", out.Current, out.Tag, out.Path)
	}
}

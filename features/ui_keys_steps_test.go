package features

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"time"

	"github.com/cucumber/godog"
)

// uiBinary is the board binary with the built web UI embedded, made once per
// run by the first scenario that needs a browser.
var uiBinary struct {
	once sync.Once
	path string
	err  error
}

// repoRoot is the module root: the features package runs in <root>/features.
func repoRoot() (string, error) {
	wd, err := os.Getwd()
	if err != nil {
		return "", err
	}
	return filepath.Dir(wd), nil
}

// runIn runs name with args in dir and returns its error with the output.
func runIn(ctx context.Context, dir, name string, args ...string) error {
	var out bytes.Buffer
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Dir, cmd.Stdout, cmd.Stderr = dir, &out, &out
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("%s %v in %s: %w\n%s", name, args, dir, err, out.String())
	}
	return nil
}

// installed reports whether dir/node_modules exists.
func installed(dir string) bool {
	_, err := os.Stat(filepath.Join(dir, "node_modules"))
	return err == nil
}

// buildUIBinary builds web/ into the embedded dist directory (installing its
// packages first when they are missing) and compiles a board binary that embeds
// it. The harness's own binary may predate the build, on a fresh checkout.
func buildUIBinary() (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	root, err := repoRoot()
	if err != nil {
		return "", err
	}
	web := filepath.Join(root, "web")
	if !installed(web) {
		if err := runIn(ctx, web, "npm", "ci", "--no-audit", "--no-fund"); err != nil {
			return "", err
		}
	}
	if err := runIn(ctx, web, "npm", "run", "build"); err != nil {
		return "", err
	}
	dir, err := os.MkdirTemp("", "board-godog-ui-")
	if err != nil {
		return "", err
	}
	bin := filepath.Join(dir, "board")
	build := exec.CommandContext(ctx, "go", "build", "-o", bin, "github.com/n-orlov/board/cmd/board")
	build.Env = append(os.Environ(), "CGO_ENABLED=0")
	if out, err := build.CombinedOutput(); err != nil {
		return "", fmt.Errorf("building the board binary with the UI: %w\n%s", err, out)
	}
	return bin, nil
}

// uiBinaryEmbedsWebUI makes the scenario use a board binary with the built web UI.
func (h *harness) uiBinaryEmbedsWebUI() error {
	uiBinary.once.Do(func() { uiBinary.path, uiBinary.err = buildUIBinary() })
	if uiBinary.err != nil {
		return uiBinary.err
	}
	h.binary = uiBinary.path
	return nil
}

// browserRevokesKey runs e2e/scripts/revoke-key.mjs: a headless Chromium that logs in
// through the mock provider and presses Revoke on the key's row of the page.
func (h *harness) browserRevokesKey(ctx context.Context, name string) error {
	if h.addr == "" {
		return errors.New("the board server is not running")
	}
	root, err := repoRoot()
	if err != nil {
		return err
	}
	e2e := filepath.Join(root, "e2e")
	if !installed(e2e) {
		if err := runIn(ctx, e2e, "npm", "ci", "--no-audit", "--no-fund"); err != nil {
			return err
		}
	}
	ctx, cancel := context.WithTimeout(ctx, 90*time.Second)
	defer cancel()
	return runIn(ctx, e2e, "node", filepath.Join("scripts", "revoke-key.mjs"), "http://"+h.addr, name)
}

func (h *harness) revokedEventHasPrincipal(ctx context.Context, name, want string) error {
	got, err := h.eventValue(ctx, name, `SELECT e.principal FROM events e JOIN api_keys k ON k.id = e.ref_id WHERE e.type = 'key_revoked' AND k.name = ?`)
	if err != nil {
		return err
	}
	if got != want {
		return fmt.Errorf("principal column of the key_revoked event %q, want %q", got, want)
	}
	return nil
}

func registerUIKeySteps(sc *godog.ScenarioContext, h *harness) {
	sc.Step(`^the board binary embeds the built web UI$`, h.uiBinaryEmbedsWebUI)
	sc.Step(`^a headless browser signs in through the mock provider and revokes the key "([^"]*)" on the Settings -> API keys page$`, h.browserRevokesKey)
	sc.Step(`^the key_revoked event for "([^"]*)" has principal "([^"]*)" in the principal column$`, h.revokedEventHasPrincipal)
}

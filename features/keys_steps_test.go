package features

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"regexp"
	"strings"

	"github.com/cucumber/godog"
)

var keyPattern = regexp.MustCompile(`brd_[0-9a-hjkmnp-tv-z]{52}$`)

// printedKey is the brd_ key ending the one line stdout holds.
func (h *harness) printedKey() (string, error) {
	line := strings.TrimSuffix(h.stdout, "\n")
	if line == "" || strings.Contains(line, "\n") {
		return "", fmt.Errorf("stdout %q is not exactly one line", h.stdout)
	}
	fields := strings.Fields(line)
	last := fields[len(fields)-1]
	if !keyPattern.MatchString(last) || len(last) != len("brd_")+52 {
		return "", fmt.Errorf("stdout %q does not end with a brd_ key", h.stdout)
	}
	return last, nil
}

func (h *harness) rememberKey() error {
	key, err := h.printedKey()
	h.key = key
	return err
}

func (h *harness) keyAppearsOnce() error {
	key, err := h.printedKey()
	if err != nil {
		return err
	}
	if n := strings.Count(h.stdout, key); n != 1 {
		return fmt.Errorf("the key appears %d times in stdout %q, want 1", n, h.stdout)
	}
	return nil
}

func (h *harness) stdoutOneLineStarting(prefix string) error {
	if strings.Count(h.stdout, "\n") != 1 || !strings.HasPrefix(h.stdout, prefix) {
		return fmt.Errorf("stdout %q is not one line starting with %q", h.stdout, prefix)
	}
	return nil
}

func (h *harness) stdoutLacksKey() error {
	if h.key == "" {
		return fmt.Errorf("no key was remembered")
	}
	if strings.Contains(h.stdout, h.key) {
		return fmt.Errorf("stdout %q contains the key", h.stdout)
	}
	return nil
}

// keyRow reads the whole api_keys row named name as text columns.
func (h *harness) keyRow(ctx context.Context, name string) (map[string]string, error) {
	db, err := h.openDB()
	if err != nil {
		return nil, err
	}
	defer db.Close()
	query := "SELECT id, name, key_hash, display_prefix, created_by FROM api_keys WHERE name = ?"
	if h.leg == "postgres" {
		query = "SELECT id, name, key_hash, display_prefix, created_by FROM api_keys WHERE name = $1"
	}
	cols := []string{"id", "name", "key_hash", "display_prefix", "created_by"}
	vals := make([]string, len(cols))
	dest := make([]any, len(cols))
	for i := range vals {
		dest[i] = &vals[i]
	}
	row := db.QueryRowContext(ctx, query, name)
	if err := row.Scan(dest...); err != nil {
		return nil, fmt.Errorf("api_keys row %q: %w", name, err)
	}
	out := map[string]string{}
	for i, c := range cols {
		out[c] = vals[i]
	}
	return out, nil
}

func (h *harness) rowStoresHashAndPrefix(ctx context.Context, name string) error {
	row, err := h.keyRow(ctx, name)
	if err != nil {
		return err
	}
	sum := sha256.Sum256([]byte(h.key))
	if row["key_hash"] != hex.EncodeToString(sum[:]) {
		return fmt.Errorf("key_hash %q is not the SHA-256 of the key", row["key_hash"])
	}
	if want := strings.TrimPrefix(h.key, "brd_")[:8]; row["display_prefix"] != want {
		return fmt.Errorf("display_prefix %q, want %q", row["display_prefix"], want)
	}
	return nil
}

func (h *harness) rowLacksKey(ctx context.Context) error {
	row, err := h.keyRow(ctx, "workstation")
	if err != nil {
		return err
	}
	body := strings.TrimPrefix(h.key, "brd_")
	for col, v := range row {
		if strings.Contains(v, h.key) || strings.Contains(v, body[8:]) {
			return fmt.Errorf("column %s holds the key", col)
		}
	}
	return nil
}

func registerKeySteps(sc *godog.ScenarioContext, h *harness) {
	sc.Step(`^stdout is one line that ends with a brd_ key$`, func() error { _, err := h.printedKey(); return err })
	sc.Step(`^the key appears in stdout exactly once$`, h.keyAppearsOnce)
	sc.Step(`^I remember the key stdout printed$`, h.rememberKey)
	sc.Step(`^stdout is one line starting with "([^"]*)"$`, h.stdoutOneLineStarting)
	sc.Step(`^stdout does not contain the remembered key$`, h.stdoutLacksKey)
	sc.Step(`^the api_keys row named "([^"]*)" stores the SHA-256 hash of the key and an 8-character prefix$`, h.rowStoresHashAndPrefix)
	sc.Step(`^no column of that row contains the remembered key$`, h.rowLacksKey)
}

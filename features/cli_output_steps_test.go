package features

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/cucumber/godog"
)

// runClientWith runs a client command with the remembered key and one
// environment override: BOARD_URL set to url, or BOARD_API_KEY left out.
func (h *harness) runClientWith(ctx context.Context, line, actor string, override func(extra []string) []string) error {
	words := splitQuoted(line)
	if len(words) == 0 || words[0] != "board" {
		return fmt.Errorf("command %q must start with board", line)
	}
	extra := override([]string{"BOARD_ACTOR=" + actor})
	return h.runArgs(ctx, words[1:], extra...)
}

func (h *harness) runClientAtAddress(ctx context.Context, line, actor, url string) error {
	return h.runClientWith(ctx, line, actor, func(extra []string) []string {
		return append(extra, "BOARD_URL="+url, "BOARD_API_KEY="+h.key)
	})
}

func (h *harness) runClientWithoutKey(ctx context.Context, line, actor string) error {
	return h.runClientWith(ctx, line, actor, func(extra []string) []string {
		return append(extra, "BOARD_URL=http://"+h.addr)
	})
}

// initiativesExist creates n initiatives, item-01 to item-<n>.
// initiativesExist creates n initiatives; the scenario states the total it expects with the
// inbox serve created on start (SPEC 4.2), which the step checks.
func (h *harness) initiativesExist(ctx context.Context, n int, actor string, total int) error {
	if n+1 != total {
		return fmt.Errorf("%d initiatives and the inbox make %d, not %d", n, n+1, total)
	}
	for i := 1; i <= n; i++ {
		slug := fmt.Sprintf("item-%02d", i)
		if err := h.createInitiative(ctx, slug, "Item "+slug, "", actor); err != nil {
			return err
		}
		if h.status != 201 {
			return fmt.Errorf("creating %s answered %d: %s", slug, h.status, h.api.body)
		}
	}
	return nil
}

func (h *harness) stdoutHasLines(want int) error {
	got := 0
	if h.stdout != "" {
		got = len(strings.Split(strings.TrimSuffix(h.stdout, "\n"), "\n"))
	}
	if got != want {
		return fmt.Errorf("stdout has %d lines, want %d: %q", got, want, h.stdout)
	}
	return nil
}

func (h *harness) stdoutHasNoMoreLine() error {
	if strings.Contains(h.stdout, " more → ") {
		return fmt.Errorf("stdout holds a more line: %q", h.stdout)
	}
	return nil
}

// stdoutIsJSONArrayOf checks that stdout is one JSON array of n objects that
// each carry a 26-character ULID id.
func (h *harness) stdoutIsJSONArrayOf(n int) error {
	var items []map[string]any
	if err := json.Unmarshal([]byte(h.stdout), &items); err != nil {
		return fmt.Errorf("stdout %q is not a JSON array of objects: %w", h.stdout, err)
	}
	if len(items) != n {
		return fmt.Errorf("stdout has %d objects, want %d", len(items), n)
	}
	for _, it := range items {
		if id, _ := it["id"].(string); len(id) != 26 {
			return fmt.Errorf("object %v has no 26-character id", it)
		}
	}
	return nil
}

func (h *harness) stdoutIsJSONObjectWithID() error {
	var it map[string]any
	if err := json.Unmarshal([]byte(h.stdout), &it); err != nil {
		return fmt.Errorf("stdout %q is not a JSON object: %w", h.stdout, err)
	}
	if id, _ := it["id"].(string); len(id) != 26 {
		return fmt.Errorf("object %v has no 26-character id", it)
	}
	return nil
}

func (h *harness) stderrLineEndsWith(suffix string) error {
	if !strings.HasSuffix(h.stderr, suffix+"\n") || strings.Count(h.stderr, "\n") != 1 {
		return fmt.Errorf("stderr %q is not one line ending %q", h.stderr, suffix)
	}
	return nil
}

func registerCLIOutputSteps(sc *godog.ScenarioContext, h *harness) {
	sc.Step(`^I run the client '([^']*)' as actor "([^"]*)" against the address "([^"]*)"$`, h.runClientAtAddress)
	sc.Step(`^I run the client '([^']*)' as actor "([^"]*)" with no BOARD_API_KEY$`, h.runClientWithoutKey)
	sc.Step(`^(\d+) initiatives exist as actor "([^"]*)", which with the inbox make (\d+)$`, h.initiativesExist)
	sc.Step(`^stdout has (\d+) lines?$`, h.stdoutHasLines)
	sc.Step(`^stdout has no more line$`, h.stdoutHasNoMoreLine)
	sc.Step(`^stdout is a JSON array of (\d+) objects with ULID ids$`, h.stdoutIsJSONArrayOf)
	sc.Step(`^stdout is a JSON object with a ULID id$`, h.stdoutIsJSONObjectWithID)
	sc.Step(`^stderr is one line ending with "([^"]*)"$`, h.stderrLineEndsWith)
}

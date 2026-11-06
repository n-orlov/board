package features

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	"github.com/cucumber/godog"
)

// stdoutPath walks the JSON on stdout (one line) along a dotted path, where a
// number indexes an array, and returns the value it ends on.
func (h *harness) stdoutPath(path string) (any, error) {
	var doc any
	if err := json.Unmarshal([]byte(h.stdout), &doc); err != nil || strings.Count(h.stdout, "\n") != 1 {
		return nil, fmt.Errorf("stdout %q is not exactly one JSON line", h.stdout)
	}
	for _, key := range strings.Split(path, ".") {
		if path == "" {
			break
		}
		switch v := doc.(type) {
		case map[string]any:
			doc = v[key]
		case []any:
			i, err := strconv.Atoi(key)
			if err != nil || i < 0 || i >= len(v) {
				return nil, fmt.Errorf("stdout has no member %q in %q", key, path)
			}
			doc = v[i]
		default:
			return nil, fmt.Errorf("stdout has no member %q in %q", key, path)
		}
	}
	return doc, nil
}

// stdoutPathIs checks the value at the path of the JSON on stdout.
func (h *harness) stdoutPathIs(path, want string) error {
	got, err := h.stdoutPath(path)
	if err != nil {
		return err
	}
	if fmt.Sprint(got) != want {
		return fmt.Errorf("stdout path %q is %v, want %q", path, got, want)
	}
	return nil
}

// stdoutPathHasItems checks the array at the path of the JSON on stdout.
func (h *harness) stdoutPathHasItems(path string, want int) error {
	got, err := h.stdoutPath(path)
	if err != nil {
		return err
	}
	if items, ok := got.([]any); !ok || len(items) != want {
		return fmt.Errorf("stdout path %q is %v, want an array of %d items", path, got, want)
	}
	return nil
}

// stdoutIsLine checks stdout is exactly one line.
func (h *harness) stdoutIsLine(want string) error {
	if h.stdout != want+"\n" {
		return fmt.Errorf("stdout %q, want %q", h.stdout, want+"\n")
	}
	return nil
}

func registerCLILeaseSteps(sc *godog.ScenarioContext, h *harness) {
	// The single-quoted forms take text that holds double quotes.
	sc.Step(`^stdout is exactly '([^']*)'$`, h.stdoutIsLine)
	sc.Step(`^stderr is exactly '([^']*)'$`, h.stderrIs)
	sc.Step(`^stdout line (\d+) matches '([^']*)'$`, h.stdoutLineMatches)
	sc.Step(`^stderr is one line ending with '([^']*)'$`, h.stderrLineEndsWith)
	sc.Step(`^stdout JSON path "([^"]*)" is "([^"]*)"$`, h.stdoutPathIs)
	sc.Step(`^stdout JSON path "([^"]*)" has (\d+) items$`, h.stdoutPathHasItems)
}

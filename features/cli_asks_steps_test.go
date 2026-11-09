package features

import (
	"fmt"
	"os"
	"strings"

	"github.com/cucumber/godog"
)

// textFileHolds writes text to a temp file a client command line names as
// <file>; a `\n` in text is a line break.
func (h *harness) textFileHolds(text string) error {
	f, err := os.CreateTemp("", "board-godog-text-")
	if err != nil {
		return err
	}
	defer func() { _ = f.Close() }()
	if _, err := f.WriteString(strings.ReplaceAll(text, `\n`, "\n")); err != nil {
		return err
	}
	h.textFile = f.Name()
	return nil
}

// standardInputHolds sets what the next client command reads from standard input.
func (h *harness) standardInputHolds(text string) error {
	if text == "" {
		return fmt.Errorf("the standard input must hold some text")
	}
	h.stdin = strings.ReplaceAll(text, `\n`, "\n")
	return nil
}

// stderrHasLines checks stderr is exactly n lines.
func (h *harness) stderrHasLines(n int) error {
	if got := strings.Count(h.stderr, "\n"); got != n || !strings.HasSuffix(h.stderr, "\n") {
		return fmt.Errorf("stderr %q has %d lines, want %d", h.stderr, got, n)
	}
	return nil
}

func registerCLIAskSteps(sc *godog.ScenarioContext, h *harness) {
	sc.Step(`^a text file holds "([^"]*)"$`, h.textFileHolds)
	sc.Step(`^stderr has (\d+) lines?$`, h.stderrHasLines)
	sc.Step(`^the standard input holds "([^"]*)"$`, h.standardInputHolds)
}

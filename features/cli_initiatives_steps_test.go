package features

import (
	"context"
	"fmt"
	"regexp"
	"strings"

	"github.com/cucumber/godog"
)

// splitQuoted splits a command line into words; a word may be wrapped in
// double quotes to hold spaces.
func splitQuoted(line string) []string {
	var words []string
	var cur strings.Builder
	inQuote, started := false, false
	for _, r := range line {
		switch {
		case r == '"':
			inQuote, started = !inQuote, true
		case r == ' ' && !inQuote:
			if started {
				words, cur, started = append(words, cur.String()), strings.Builder{}, false
			}
		default:
			cur.WriteRune(r)
			started = true
		}
	}
	if started {
		words = append(words, cur.String())
	}
	return words
}

// runClient runs a client command against the running server with the
// remembered key. A nil actor leaves BOARD_ACTOR unset; a nil wayBack leaves
// BOARD_WAY_BACK unset.
func (h *harness) runClient(ctx context.Context, line string, actor, wayBack *string) error {
	if err := h.requireKey(); err != nil {
		return err
	}
	words := splitQuoted(line)
	if len(words) == 0 || words[0] != "board" {
		return fmt.Errorf("command %q must start with board", line)
	}
	extra := []string{"BOARD_URL=http://" + h.addr, "BOARD_API_KEY=" + h.key}
	if actor != nil {
		extra = append(extra, "BOARD_ACTOR="+*actor)
	}
	if wayBack != nil {
		extra = append(extra, "BOARD_WAY_BACK="+*wayBack)
	}
	return h.runArgs(ctx, words[1:], extra...)
}

func (h *harness) runClientAsActor(ctx context.Context, line, actor string) error {
	return h.runClient(ctx, line, &actor, nil)
}

func (h *harness) runClientWithWayBack(ctx context.Context, line, actor, wayBack string) error {
	return h.runClient(ctx, line, &actor, &wayBack)
}

func (h *harness) runClientWithNoActor(ctx context.Context, line string) error {
	return h.runClient(ctx, line, nil, nil)
}

func (h *harness) stdoutIs(doc *godog.DocString) error {
	if want := strings.TrimSpace(doc.Content) + "\n"; h.stdout != want {
		return fmt.Errorf("stdout %q, want %q", h.stdout, want)
	}
	return nil
}

func (h *harness) stdoutLineMatches(n int, pattern string) error {
	lines := strings.Split(strings.TrimSuffix(h.stdout, "\n"), "\n")
	if n < 1 || n > len(lines) {
		return fmt.Errorf("stdout %q has %d lines, want a line %d", h.stdout, len(lines), n)
	}
	re, err := regexp.Compile(pattern)
	if err != nil {
		return err
	}
	if !re.MatchString(lines[n-1]) {
		return fmt.Errorf("stdout line %d %q does not match %q", n, lines[n-1], pattern)
	}
	return nil
}

func (h *harness) stderrIs(want string) error {
	if h.stderr != want+"\n" {
		return fmt.Errorf("stderr %q, want %q", h.stderr, want+"\n")
	}
	return nil
}

func (h *harness) outputLacksKey() error {
	if h.key == "" {
		return fmt.Errorf("no key was remembered")
	}
	if strings.Contains(h.stdout+h.stderr, h.key) {
		return fmt.Errorf("the output holds the key: stdout %q stderr %q", h.stdout, h.stderr)
	}
	return nil
}

func registerCLIInitiativeSteps(sc *godog.ScenarioContext, h *harness) {
	sc.Step(`^I run the client '([^']*)' as actor "([^"]*)"$`, h.runClientAsActor)
	sc.Step(`^I run the client '([^']*)' as actor "([^"]*)" with way back "([^"]*)"$`, h.runClientWithWayBack)
	sc.Step(`^I run the client '([^']*)' with no actor$`, h.runClientWithNoActor)
	sc.Step(`^stdout is:$`, h.stdoutIs)
	sc.Step(`^stdout line (\d+) matches "([^"]*)"$`, h.stdoutLineMatches)
	sc.Step(`^stderr is exactly "([^"]*)"$`, h.stderrIs)
	sc.Step(`^neither stdout nor stderr contains the remembered key$`, h.outputLacksKey)
}

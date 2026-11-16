package features

import (
	"context"
	"fmt"
	"net/http"
	"strings"

	"github.com/cucumber/godog"

	"github.com/n-orlov/village-hall/internal/cli"
)

// stdoutNamesOnlyKnownCommands parses the `board …` commands and --flags out of
// what the binary printed and resolves them against the CLI's command tree.
func (h *harness) stdoutNamesOnlyKnownCommands() error {
	if problems := cli.UnknownReferences(h.stdout, cli.Commands()); len(problems) != 0 {
		return fmt.Errorf("stdout names what this build does not have: %s", strings.Join(problems, "; "))
	}
	return nil
}

// stdoutNamesCommands checks that the text names at least n commands, so the
// resolution above is not vacuous.
func (h *harness) stdoutNamesCommands(n int) error {
	if got := strings.Count(h.stdout, "`vh "); got < n {
		return fmt.Errorf("stdout names %d commands in backticks, want at least %d", got, n)
	}
	return nil
}

func (h *harness) getKeyedAs(ctx context.Context, path, actor string) error {
	if err := h.requireKey(); err != nil {
		return err
	}
	return h.sendKeyed(ctx, http.MethodGet, path, h.key, actor, nil)
}

func (h *harness) putProtocolSeen(ctx context.Context, version int, actor string) error {
	if err := h.requireKey(); err != nil {
		return err
	}
	return h.sendKeyed(ctx, http.MethodPut, "/api/v1/actors/me/protocol-seen", h.key, actor, map[string]int{"version": version})
}

func (h *harness) protocolSeenIs(ctx context.Context, handle string, want int) error {
	got, err := h.scalar(ctx, `SELECT COALESCE(CAST(protocol_seen AS TEXT), 'null') FROM actors WHERE handle = ?`, handle)
	if err != nil {
		return fmt.Errorf("read protocol_seen of %q: %w", handle, err)
	}
	if got != fmt.Sprint(want) {
		return fmt.Errorf("protocol_seen of %q is %s, want %d", handle, got, want)
	}
	return nil
}

func (h *harness) noActorRow(ctx context.Context, handle string) error {
	got, err := h.scalar(ctx, `SELECT count(*) FROM actors WHERE handle = ?`, handle)
	if err != nil {
		return err
	}
	if got != "0" {
		return fmt.Errorf("%s rows for actor %q, want none", got, handle)
	}
	return nil
}

// responseVersionIsStdout checks the response's version against what the last
// command printed, which the scenario makes `vh version`.
func (h *harness) responseVersionIsStdout() error {
	doc, err := h.bodyMap()
	if err != nil {
		return err
	}
	got, _ := doc["version"].(string)
	if got == "" || got != strings.TrimSpace(h.stdout) {
		return fmt.Errorf("meta version %q, want the binary's own version %q", got, strings.TrimSpace(h.stdout))
	}
	return nil
}

// stdoutNamesSpan checks stdout carries the exact backticked span, so a command
// the protocol must teach cannot be dropped or reworded unnoticed.
func (h *harness) stdoutNamesSpan(span string) error {
	if !strings.Contains(h.stdout, "`"+span+"`") {
		return fmt.Errorf("stdout does not name `%s`", span)
	}
	return nil
}

func registerHelpAgentSteps(sc *godog.ScenarioContext, h *harness) {
	sc.Step(`^every board command and flag the stdout text names exists in the command tree$`, h.stdoutNamesOnlyKnownCommands)
	sc.Step(`^the stdout text names at least (\d+) commands$`, h.stdoutNamesCommands)
	sc.Step(`^the stdout text names the command '([^']*)'$`, h.stdoutNamesSpan)
	sc.Step(`^I request GET "([^"]*)" with the remembered key as actor "([^"]*)"$`, h.getKeyedAs)
	sc.Step(`^I record protocol version (-?\d+) as read as actor "([^"]*)" through the API$`, h.putProtocolSeen)
	sc.Step(`^the response version is the version the last command printed$`, h.responseVersionIsStdout)
	sc.Step(`^the protocol_seen of the actor "([^"]*)" is (\d+)$`, h.protocolSeenIs)
	sc.Step(`^there is no actor row for "([^"]*)"$`, h.noActorRow)
}

package features

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"

	"github.com/cucumber/godog"

	"github.com/n-orlov/village-hall/internal/cli"
)

// startCounts reads the tasks, leases and events counts as one string.
const startCountsSQL = `SELECT (SELECT count(*) FROM tasks) || '/' || (SELECT count(*) FROM leases) || '/' || (SELECT count(*) FROM events)`

func (h *harness) startWithBody(ctx context.Context, body, actor string) error {
	return h.sendTask(ctx, http.MethodPost, "/api/v1/start", body, actor)
}

func (h *harness) noteStartCounts(ctx context.Context) error {
	got, err := h.scalar(ctx, startCountsSQL)
	h.api.startCounts = got
	return err
}

// startCountsChanged checks how far the tasks, leases and events counts moved
// since noteStartCounts.
func (h *harness) startCountsChanged(ctx context.Context, tasks, leases, events int) error {
	var t0, l0, e0 int
	if _, err := fmt.Sscanf(h.api.startCounts, "%d/%d/%d", &t0, &l0, &e0); err != nil {
		return fmt.Errorf("no counts were noted (%q): %w", h.api.startCounts, err)
	}
	want := fmt.Sprintf("%d/%d/%d", t0+tasks, l0+leases, e0+events)
	got, err := h.scalar(ctx, startCountsSQL)
	if err != nil {
		return err
	}
	if got != want {
		return fmt.Errorf("tasks/leases/events are %s, want %s (noted %s)", got, want, h.api.startCounts)
	}
	return nil
}

// protocolIsClients checks the protocol in the response against the version the
// client binary embeds, which is what the server reports.
func (h *harness) protocolIsClients() error {
	return h.pathIs("protocol", fmt.Sprint(cli.ProtocolVersion()))
}

// stubStartBoard starts a server whose POST /api/v1/start answers one lease with
// the given protocol numbers, standing for a server newer than this binary.
func (h *harness) stubStartBoard(server, seen int) error {
	body := fmt.Sprintf(`{"lease":{"ref":"L1","task":"aa-1","actor":"claude@laptop","intent":"x","state":"live","resources":[],`+
		`"renewed_at":"2026-03-04T12:00:00Z","expires_at":"2026-03-04T14:00:00Z"},"conflicts":[],"task_created":false,`+
		`"now":{"actor":"claude@laptop","my_leases":[],"others_leases":[],"conflicts":[],"my_asks":[],"notes":[],"notes_more":0,`+
		`"attention":{"open_asks":0,"done_not_closed":0,"expired_leases":0,"overdue_watches":0,"conflicts":0}},"protocol":%d,"protocol_seen":%d}`, server, seen)
	h.api.stubBoard = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(body))
	}))
	return nil
}

// runClientAtStub runs a client command against the stub the scenario started.
func (h *harness) runClientAtStub(ctx context.Context, line, actor string) error {
	if h.api.stubBoard == nil {
		return fmt.Errorf("no stub board was started")
	}
	return h.runClientAtAddress(ctx, line, actor, h.api.stubBoard.URL)
}

// stdoutHasLinesStartingWith counts the stdout lines that start with prefix.
func (h *harness) stdoutHasLinesStartingWith(n int, prefix string) error {
	got := 0
	for _, line := range strings.Split(h.stdout, "\n") {
		if strings.HasPrefix(line, prefix) {
			got++
		}
	}
	if got != n {
		return fmt.Errorf("stdout has %d lines starting with %q, want %d: %q", got, prefix, n, h.stdout)
	}
	return nil
}

// lastStdoutLineStarts checks the last line of stdout, so a hint is known to
// come after the summary.
func (h *harness) lastStdoutLineStarts(prefix string) error {
	lines := strings.Split(strings.TrimSuffix(h.stdout, "\n"), "\n")
	if last := lines[len(lines)-1]; !strings.HasPrefix(last, prefix) {
		return fmt.Errorf("the last stdout line %q does not start with %q", last, prefix)
	}
	return nil
}

func registerStartSteps(sc *godog.ScenarioContext, h *harness) {
	sc.Step(`^a stub board answers vh start with protocol (\d+) and protocol_seen (\d+)$`, h.stubStartBoard)
	sc.Step(`^I run the client '([^']*)' as actor "([^"]*)" against the stub board$`, h.runClientAtStub)
	sc.Step(`^stdout has (\d+) lines? starting with "([^"]*)"$`, h.stdoutHasLinesStartingWith)
	sc.Step(`^the last stdout line starts with "([^"]*)"$`, h.lastStdoutLineStarts)
	sc.Step(`^I start with body "([^"]*)" as actor "([^"]*)"$`, h.startWithBody)
	sc.Step(`^I note the counts of tasks, leases and events$`, h.noteStartCounts)
	sc.Step(`^the counts of tasks, leases and events changed by (-?\d+), (-?\d+) and (-?\d+)$`, h.startCountsChanged)
	sc.Step(`^the response path "protocol" is the protocol version the client embeds$`, h.protocolIsClients)
}

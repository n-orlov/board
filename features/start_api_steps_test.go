package features

import (
	"context"
	"fmt"
	"net/http"

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

func registerStartSteps(sc *godog.ScenarioContext, h *harness) {
	sc.Step(`^I start with body "([^"]*)" as actor "([^"]*)"$`, h.startWithBody)
	sc.Step(`^I note the counts of tasks, leases and events$`, h.noteStartCounts)
	sc.Step(`^the counts of tasks, leases and events changed by (-?\d+), (-?\d+) and (-?\d+)$`, h.startCountsChanged)
	sc.Step(`^the response path "protocol" is the protocol version the client embeds$`, h.protocolIsClients)
}

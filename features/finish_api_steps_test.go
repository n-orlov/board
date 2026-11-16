package features

import (
	"context"
	"fmt"
	"net/http"

	"github.com/cucumber/godog"
)

// finishCountsSQL reads the events, notes and live leases counts as one string.
const finishCountsSQL = `SELECT (SELECT count(*) FROM events) || '/' || (SELECT count(*) FROM notes) || '/' ||
	(SELECT count(*) FROM leases WHERE state = 'live')`

func (h *harness) finishWithBody(ctx context.Context, body, actor string) error {
	return h.sendTask(ctx, http.MethodPost, "/api/v1/finish", body, actor)
}

func (h *harness) noteFinishCounts(ctx context.Context) error {
	got, err := h.scalar(ctx, finishCountsSQL)
	h.api.finishCounts = got
	return err
}

// finishCountsChanged checks how far the events, notes and live leases counts
// moved since noteFinishCounts.
func (h *harness) finishCountsChanged(ctx context.Context, events, notes, live int) error {
	var e0, n0, l0 int
	if _, err := fmt.Sscanf(h.api.finishCounts, "%d/%d/%d", &e0, &n0, &l0); err != nil {
		return fmt.Errorf("no counts were noted (%q): %w", h.api.finishCounts, err)
	}
	want := fmt.Sprintf("%d/%d/%d", e0+events, n0+notes, l0+live)
	got, err := h.scalar(ctx, finishCountsSQL)
	if err != nil {
		return err
	}
	if got != want {
		return fmt.Errorf("events/notes/live leases are %s, want %s (noted %s)", got, want, h.api.finishCounts)
	}
	return nil
}

func registerFinishSteps(sc *godog.ScenarioContext, h *harness) {
	sc.Step(`^I finish with body "([^"]*)" as actor "([^"]*)"$`, h.finishWithBody)
	sc.Step(`^I note the counts of events, notes and live leases$`, h.noteFinishCounts)
	sc.Step(`^the counts of events, notes and live leases changed by (-?\d+), (-?\d+) and (-?\d+)$`, h.finishCountsChanged)
}

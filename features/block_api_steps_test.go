package features

import (
	"context"
	"fmt"

	"github.com/cucumber/godog"
)

// blockCountsSQL reads the asks, events and live leases counts as one string.
const blockCountsSQL = `SELECT (SELECT count(*) FROM asks) || '/' || (SELECT count(*) FROM events) || '/' ||
	(SELECT count(*) FROM leases WHERE state = 'live')`

func (h *harness) noteBlockCounts(ctx context.Context) error {
	got, err := h.scalar(ctx, blockCountsSQL)
	h.api.blockCounts = got
	return err
}

// blockCountsChanged checks how far the asks, events and live leases counts
// moved since noteBlockCounts.
func (h *harness) blockCountsChanged(ctx context.Context, asks, events, live int) error {
	var a0, e0, l0 int
	if _, err := fmt.Sscanf(h.api.blockCounts, "%d/%d/%d", &a0, &e0, &l0); err != nil {
		return fmt.Errorf("no counts were noted (%q): %w", h.api.blockCounts, err)
	}
	want := fmt.Sprintf("%d/%d/%d", a0+asks, e0+events, l0+live)
	got, err := h.scalar(ctx, blockCountsSQL)
	if err != nil {
		return err
	}
	if got != want {
		return fmt.Errorf("asks/events/live leases are %s, want %s (noted %s)", got, want, h.api.blockCounts)
	}
	return nil
}

// leaseOutcomeIs checks the outcome the lease row holds.
func (h *harness) leaseOutcomeIs(ctx context.Context, ref, want string) error {
	n, err := leaseNumber(ref)
	if err != nil {
		return err
	}
	got, err := h.scalar(ctx, `SELECT outcome FROM leases WHERE n = ?`, n)
	if err != nil {
		return err
	}
	if got != want {
		return fmt.Errorf("lease %s has the outcome %q in the database, want %q", ref, got, want)
	}
	return nil
}

func registerBlockSteps(sc *godog.ScenarioContext, h *harness) {
	sc.Step(`^I note the counts of asks, events and live leases$`, h.noteBlockCounts)
	sc.Step(`^the counts of asks, events and live leases changed by (-?\d+), (-?\d+) and (-?\d+)$`, h.blockCountsChanged)
	sc.Step(`^the lease "([^"]*)" has the outcome "([^"]*)" in the database$`, h.leaseOutcomeIs)
}

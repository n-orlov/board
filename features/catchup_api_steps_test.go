package features

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/cucumber/godog"
)

// catchupCountsSQL reads the events and cursors counts as one string.
const catchupCountsSQL = `SELECT (SELECT count(*) FROM events) || '/' || (SELECT count(*) FROM cursors)`

func (h *harness) catchUp(ctx context.Context, actor string) error {
	return h.sendTask(ctx, http.MethodPost, "/api/v1/catchup", "", actor)
}

func (h *harness) noteCatchupCounts(ctx context.Context) error {
	got, err := h.scalar(ctx, catchupCountsSQL)
	h.api.catchupCounts = got
	return err
}

// catchupCountsChanged checks how far the events and cursors counts moved since
// noteCatchupCounts.
func (h *harness) catchupCountsChanged(ctx context.Context, events, cursors int) error {
	var e0, c0 int
	if _, err := fmt.Sscanf(h.api.catchupCounts, "%d/%d", &e0, &c0); err != nil {
		return fmt.Errorf("no counts were noted (%q): %w", h.api.catchupCounts, err)
	}
	want := fmt.Sprintf("%d/%d", e0+events, c0+cursors)
	got, err := h.scalar(ctx, catchupCountsSQL)
	if err != nil {
		return err
	}
	if got != want {
		return fmt.Errorf("events/cursors are %s, want %s (noted %s)", got, want, h.api.catchupCounts)
	}
	return nil
}

func (h *harness) catchupEventCount(ctx context.Context, want int, actor string) error {
	got, err := h.scalar(ctx, `SELECT count(*) FROM events e JOIN actors a ON a.id = e.actor_id WHERE e.type = 'catchup' AND a.handle = ?`, actor)
	if err != nil {
		return err
	}
	if got != fmt.Sprint(want) {
		return fmt.Errorf("%s has %s catchup events, want %d", actor, got, want)
	}
	return nil
}

// catchupEventSays checks the summary and the counts the newest catchup event
// of the actor holds.
func (h *harness) catchupEventSays(ctx context.Context, actor, summary string, notes, answers int) error {
	got, err := h.scalar(ctx, `SELECT e.summary || '|' || e.data FROM events e JOIN actors a ON a.id = e.actor_id
		WHERE e.type = 'catchup' AND a.handle = ? ORDER BY e.n DESC LIMIT 1`, actor)
	if err != nil {
		return err
	}
	var s string
	var data struct {
		Notes   int `json:"notes"`
		Answers int `json:"answers"`
	}
	for i := range got {
		if got[i] == '|' {
			s = got[:i]
			if err := json.Unmarshal([]byte(got[i+1:]), &data); err != nil {
				return fmt.Errorf("catchup event data %q: %w", got[i+1:], err)
			}
			break
		}
	}
	if s != summary || data.Notes != notes || data.Answers != answers {
		return fmt.Errorf("the newest catchup event of %s is %q with %d notes and %d answers, want %q with %d and %d",
			actor, s, data.Notes, data.Answers, summary, notes, answers)
	}
	return nil
}

func registerCatchupSteps(sc *godog.ScenarioContext, h *harness) {
	sc.Step(`^I catch up as actor "([^"]*)"$`, h.catchUp)
	sc.Step(`^I note the counts of events and cursors$`, h.noteCatchupCounts)
	sc.Step(`^the counts of events and cursors changed by (-?\d+) and (-?\d+)$`, h.catchupCountsChanged)
	sc.Step(`^exactly (\d+) catchup events? exists? for the actor "([^"]*)"$`, h.catchupEventCount)
	sc.Step(`^the newest catchup event of "([^"]*)" says "([^"]*)" with (\d+) notes and (\d+) answers$`, h.catchupEventSays)
}

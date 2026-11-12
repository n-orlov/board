package features

import (
	"context"
	"net/http"
	"net/url"
	"time"

	"github.com/cucumber/godog"
)

// getActors reads GET /api/v1/actors with the query, which may be empty.
func (h *harness) getActors(ctx context.Context, query, actor string) error {
	p := "/api/v1/actors"
	if query != "" {
		p += "?" + query
	}
	return h.sendTask(ctx, http.MethodGet, p, "", actor)
}

// getActor reads GET /api/v1/actors/{handle}.
func (h *harness) getActor(ctx context.Context, handle, actor string) error {
	return h.sendTask(ctx, http.MethodGet, "/api/v1/actors/"+url.PathEscape(handle), "", actor)
}

// lastSeen moves last_seen_at of the actors a condition selects to a number of
// minutes ago; exactly one row must change.
func (h *harness) lastSeen(ctx context.Context, what, where string, minutes int, args ...any) error {
	at := time.Now().Add(-time.Duration(minutes) * time.Minute).UnixMilli()
	return h.updateOne(ctx, what, `UPDATE actors SET last_seen_at = ? WHERE `+where, append([]any{at}, args...)...)
}

func (h *harness) actorSeen(ctx context.Context, handle string, minutes int) error {
	return h.lastSeen(ctx, handle, `handle = ?`, minutes, handle)
}

func (h *harness) operatorSeen(ctx context.Context, minutes int) error {
	return h.lastSeen(ctx, "the operator", `kind = 'operator'`, minutes)
}

func registerActorSteps(sc *godog.ScenarioContext, h *harness) {
	sc.Step(`^I request the actors with "([^"]*)" as actor "([^"]*)"$`, h.getActors)
	sc.Step(`^I request the actor "([^"]*)" as actor "([^"]*)"$`, h.getActor)
	sc.Step(`^the actor "([^"]*)" was last seen (\d+) minutes ago$`, h.actorSeen)
	sc.Step(`^the operator actor was last seen (\d+) minutes ago$`, h.operatorSeen)
	sc.Step(`^the response list "([^"]*)" has ([a-z_]+) "([^"]*)"$`, h.timelineListIs)
}

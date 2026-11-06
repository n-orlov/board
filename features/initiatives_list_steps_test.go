package features

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"time"

	"github.com/cucumber/godog"
)

// listInitiativesWith sends GET /api/v1/initiatives with the query string.
func (h *harness) listInitiativesWith(ctx context.Context, query, actor string) error {
	p := "/api/v1/initiatives"
	if query != "" {
		p += "?" + query
	}
	return h.sendTask(ctx, http.MethodGet, p, "", actor)
}

// nextInitiativesPage lists the page after the last response's next_after.
func (h *harness) nextInitiativesPage(ctx context.Context, query, actor string) error {
	after, err := h.nextAfter()
	if err != nil {
		return err
	}
	return h.listInitiativesWith(ctx, query+"&after="+url.QueryEscape(after), actor)
}

// initiativeClosedDaysAgo backdates the closed_at of an initiative through the database handle.
func (h *harness) initiativeClosedDaysAgo(ctx context.Context, slug string, days int) error {
	at := time.Now().Add(-time.Duration(days) * 24 * time.Hour).UnixMilli()
	rows, err := h.exec(ctx, `UPDATE initiatives SET closed_at = ? WHERE slug = ? AND status = 'closed'`, at, slug)
	if err != nil {
		return err
	}
	if rows != 1 {
		return fmt.Errorf("%d closed initiatives backdated for %s, want 1", rows, slug)
	}
	return nil
}

func registerInitiativeListSteps(sc *godog.ScenarioContext, h *harness) {
	sc.Step(`^I list the initiatives with "([^"]*)" as actor "([^"]*)"$`, h.listInitiativesWith)
	sc.Step(`^I list the initiatives as actor "([^"]*)"$`, func(ctx context.Context, actor string) error {
		return h.listInitiativesWith(ctx, "", actor)
	})
	sc.Step(`^I request the next initiatives page with "([^"]*)" as actor "([^"]*)"$`, h.nextInitiativesPage)
	sc.Step(`^the initiative "([^"]*)" was closed (\d+) days ago$`, h.initiativeClosedDaysAgo)
}

package features

import (
	"context"
	"net/http"
	"net/url"

	"github.com/cucumber/godog"
)

func (h *harness) initiativeVerb(ctx context.Context, slug, verb, body, actor string) error {
	return h.sendTask(ctx, http.MethodPost, "/api/v1/initiatives/"+url.PathEscape(slug)+"/"+verb, body, actor)
}

func registerInitiativeCloseSteps(sc *godog.ScenarioContext, h *harness) {
	sc.Step(`^I close the initiative "([^"]*)" without a body as actor "([^"]*)"$`, func(ctx context.Context, slug, actor string) error {
		return h.initiativeVerb(ctx, slug, "close", "", actor)
	})
	sc.Step(`^I close the initiative "([^"]*)" with body "([^"]*)" as actor "([^"]*)"$`, func(ctx context.Context, slug, body, actor string) error {
		return h.initiativeVerb(ctx, slug, "close", body, actor)
	})
	sc.Step(`^I reopen the initiative "([^"]*)" as actor "([^"]*)"$`, func(ctx context.Context, slug, actor string) error {
		return h.initiativeVerb(ctx, slug, "reopen", "", actor)
	})
}

package features

import (
	"context"
	"net/http"
	"net/url"

	"github.com/cucumber/godog"
)

func (h *harness) depPath(ref, blockedBy string) string {
	return "/api/v1/tasks/" + url.PathEscape(ref) + "/deps/" + url.PathEscape(blockedBy)
}

func (h *harness) addDep(ctx context.Context, ref, blockedBy, actor string) error {
	return h.sendTask(ctx, http.MethodPost, h.depPath(ref, blockedBy), "", actor)
}

func (h *harness) removeDep(ctx context.Context, ref, blockedBy, actor string) error {
	return h.sendTask(ctx, http.MethodDelete, h.depPath(ref, blockedBy), "", actor)
}

func registerTaskDepSteps(sc *godog.ScenarioContext, h *harness) {
	sc.Step(`^I make the task "([^"]*)" blocked by "([^"]*)" as actor "([^"]*)"$`, h.addDep)
	sc.Step(`^I remove the dependency of the task "([^"]*)" on "([^"]*)" as actor "([^"]*)"$`, h.removeDep)
}

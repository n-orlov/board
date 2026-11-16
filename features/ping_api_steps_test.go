package features

import (
	"context"
	"fmt"
	"net/http"

	"github.com/cucumber/godog"
)

func (h *harness) pingWithBody(ctx context.Context, body, actor string) error {
	return h.sendTask(ctx, http.MethodPost, "/api/v1/ping", body, actor)
}

// milestoneSummaryIs checks the summary the newest milestone event of the task holds.
func (h *harness) milestoneSummaryIs(ctx context.Context, ref, want string) error {
	data, err := h.latestEventData(ctx, "milestone", ref)
	if err != nil {
		return err
	}
	if data["summary"] != want {
		return fmt.Errorf("the milestone on %s has the summary %v, want %q", ref, data["summary"], want)
	}
	return nil
}

func registerPingSteps(sc *godog.ScenarioContext, h *harness) {
	sc.Step(`^I ping with body "([^"]*)" as actor "([^"]*)"$`, h.pingWithBody)
	sc.Step(`^the task "([^"]*)" has a milestone event with the summary "([^"]*)"$`, h.milestoneSummaryIs)
}

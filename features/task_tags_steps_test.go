package features

import (
	"context"
	"fmt"
	"net/http"
	"net/url"

	"github.com/cucumber/godog"
)

// taskTagRowsAre checks how many rows task_tags holds.
func (h *harness) taskTagRowsAre(ctx context.Context, want int) error {
	got, err := h.scalar(ctx, `SELECT count(*) FROM task_tags`)
	if err != nil {
		return err
	}
	if got != fmt.Sprint(want) {
		return fmt.Errorf("task_tags holds %s rows, want %d", got, want)
	}
	return nil
}

func tagPath(ref, tag string) string {
	return "/api/v1/tasks/" + url.PathEscape(ref) + "/tags/" + url.PathEscape(tag)
}

func (h *harness) addTag(ctx context.Context, tag, ref, actor string) error {
	return h.sendTask(ctx, http.MethodPut, tagPath(ref, tag), "", actor)
}

func (h *harness) removeTag(ctx context.Context, tag, ref, actor string) error {
	return h.sendTask(ctx, http.MethodDelete, tagPath(ref, tag), "", actor)
}

// taskTagEventIs checks the newest task_updated event of a task for the tag it
// names and the change it records, and that it names tags as what changed.
func (h *harness) taskTagEventIs(ctx context.Context, ref, tag, change string) error {
	data, err := h.latestEventData(ctx, "task_updated", ref)
	if err != nil {
		return err
	}
	if got := fieldText(data["changed"]); got != "tags" {
		return fmt.Errorf("data.changed is %q, want tags", got)
	}
	if got := fieldText(data["tag"]); got != tag {
		return fmt.Errorf("data.tag is %q, want %q", got, tag)
	}
	if got := fieldText(data["change"]); got != change {
		return fmt.Errorf("data.change is %q, want %q", got, change)
	}
	return nil
}

func registerTaskTagSteps(sc *godog.ScenarioContext, h *harness) {
	sc.Step(`^I add the tag "([^"]*)" to the task "([^"]*)" as actor "([^"]*)"$`, h.addTag)
	sc.Step(`^I remove the tag "([^"]*)" from the task "([^"]*)" as actor "([^"]*)"$`, h.removeTag)
	sc.Step(`^the latest task_updated event of "([^"]*)" names the tag "([^"]*)" as "(added|removed)"$`, h.taskTagEventIs)
	sc.Step(`^the task_tags table holds (\d+) rows?$`, h.taskTagRowsAre)
}

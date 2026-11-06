package features

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/cucumber/godog"
)

// listTasks sends GET /api/v1/tasks with the query string.
func (h *harness) listTasks(ctx context.Context, query, actor string) error {
	p := "/api/v1/tasks"
	if query != "" {
		p += "?" + query
	}
	return h.sendTask(ctx, http.MethodGet, p, "", actor)
}

// nextTasksPage lists the page after the last response's next_after.
func (h *harness) nextTasksPage(ctx context.Context, query, actor string) error {
	after, err := h.nextAfter()
	if err != nil {
		return err
	}
	return h.listTasks(ctx, query+"&after="+url.QueryEscape(after), actor)
}

// closedDaysAgo backdates the closed_at of a task through the database handle.
func (h *harness) closedDaysAgo(ctx context.Context, ref string, days int) error {
	at := time.Now().Add(-time.Duration(days) * 24 * time.Hour).UnixMilli()
	dash := strings.LastIndex(ref, "-")
	if dash < 0 {
		return fmt.Errorf("%q is not a task ref", ref)
	}
	slug, seq := ref[:dash], ref[dash+1:]
	rows, err := h.exec(ctx, `UPDATE tasks SET closed_at = ? WHERE seq = ? AND initiative_id = (SELECT id FROM initiatives WHERE slug = ?)`, at, seq, slug)
	if err != nil {
		return err
	}
	if rows != 1 {
		return fmt.Errorf("%d tasks backdated for %s, want 1", rows, ref)
	}
	return nil
}

func registerTaskListSteps(sc *godog.ScenarioContext, h *harness) {
	sc.Step(`^I list the tasks with "([^"]*)" as actor "([^"]*)"$`, h.listTasks)
	sc.Step(`^I list the tasks as actor "([^"]*)"$`, func(ctx context.Context, actor string) error { return h.listTasks(ctx, "", actor) })
	sc.Step(`^I request the next tasks page with "([^"]*)" as actor "([^"]*)"$`, h.nextTasksPage)
	sc.Step(`^the task "([^"]*)" was closed (\d+) days ago$`, h.closedDaysAgo)
}

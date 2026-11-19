package features

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/cucumber/godog"
)

// getNowBoard reads GET /api/v1/attention/now with the query, which may be empty.
func (h *harness) getNowBoard(ctx context.Context, query, actor string) error {
	p := "/api/v1/attention/now"
	if query != "" {
		p += "?" + query
	}
	return h.sendTask(ctx, http.MethodGet, p, "", actor)
}

// taskEventsAge sets the time of every event on the task ref (such as aa-1) to
// the given hours ago, through the database handle: no sleep, the rows say how
// long ago the task was touched.
func (h *harness) taskEventsAge(ctx context.Context, ref string, hours int) error {
	slug, seq, ok := strings.Cut(ref, "-")
	if !ok {
		return fmt.Errorf("task ref %q has no dash", ref)
	}
	at := time.Now().Add(-time.Duration(hours) * time.Hour).UnixMilli()
	rows, err := h.exec(ctx, `UPDATE events SET at = ? WHERE task_id = (SELECT t.id FROM tasks t
		JOIN initiatives i ON i.id = t.initiative_id WHERE i.slug = ? AND t.seq = ?)`, at, slug, seq)
	if err != nil {
		return err
	}
	if rows == 0 {
		return fmt.Errorf("no events of %s were backdated", ref)
	}
	return nil
}

// everyEventAge sets the time of every event on the board to the given hours ago.
func (h *harness) everyEventAge(ctx context.Context, hours int) error {
	at := time.Now().Add(-time.Duration(hours) * time.Hour).UnixMilli()
	_, err := h.exec(ctx, `UPDATE events SET at = ?`, at)
	return err
}

// nowTasksAre checks the refs of the tasks of one part of the Now answer, in order.
func (h *harness) nowTasksAre(part, want string) error {
	got, _, err := h.responsePath(part)
	if err != nil {
		return err
	}
	list, ok := got.([]any)
	if !ok {
		return fmt.Errorf("%q is %v, want a list (response %s)", part, got, h.api.body)
	}
	refs := make([]string, len(list))
	for i, e := range list {
		item, _ := e.(map[string]any)
		task, _ := item["task"].(map[string]any)
		refs[i] = fmt.Sprint(task["task"])
	}
	if text := strings.Join(refs, ","); text != strings.Join(splitList(want), ",") {
		return fmt.Errorf("%s holds the tasks %q, want %q (response %s)", part, text, want, h.api.body)
	}
	return nil
}

func registerNowBoardSteps(sc *godog.ScenarioContext, h *harness) {
	sc.Step(`^I request the now board with "([^"]*)" as actor "([^"]*)"$`, h.getNowBoard)
	sc.Step(`^the events of the task "([^"]*)" are (\d+) hours old$`, h.taskEventsAge)
	sc.Step(`^every event on the board is (\d+) hours old$`, h.everyEventAge)
	sc.Step(`^the tasks in "(in_flight|recent)" are "([^"]*)"$`, h.nowTasksAre)
}

package features

import (
	"context"
	"fmt"

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

func registerTaskTagSteps(sc *godog.ScenarioContext, h *harness) {
	sc.Step(`^the task_tags table holds (\d+) rows?$`, h.taskTagRowsAre)
}

package features

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/cucumber/godog"
)

// tasksExist creates n tasks in the initiative through the API, titled Task 1 to Task <n>.
func (h *harness) tasksExist(ctx context.Context, n int, initiative, actor string) error {
	for i := 1; i <= n; i++ {
		if err := h.createTask(ctx, initiative, fmt.Sprintf("Task %d", i), actor); err != nil {
			return err
		}
		if h.status != http.StatusCreated {
			return fmt.Errorf("creating task %d answered %d: %s", i, h.status, h.api.body)
		}
	}
	return nil
}

// milestonesRecorded records n milestones, m1 to m<n>, on the task through the API.
func (h *harness) milestonesRecorded(ctx context.Context, n int, ref, actor string) error {
	for i := 1; i <= n; i++ {
		body, _ := json.Marshal(map[string]any{"summary": fmt.Sprintf("m%d", i)})
		if err := h.recordMilestone(ctx, ref, string(body), actor); err != nil {
			return err
		}
		if h.status != http.StatusOK {
			return fmt.Errorf("milestone %d answered %d: %s", i, h.status, h.api.body)
		}
	}
	return nil
}

func (h *harness) stdoutHasTheLine(want string) error {
	for _, line := range strings.Split(h.stdout, "\n") {
		if line == want {
			return nil
		}
	}
	return fmt.Errorf("stdout has no line %q: %q", want, h.stdout)
}

func registerCLITaskSteps(sc *godog.ScenarioContext, h *harness) {
	sc.Step(`^(\d+) tasks exist in the initiative "([^"]*)" as actor "([^"]*)"$`, h.tasksExist)
	sc.Step(`^(\d+) milestones are recorded on the task "([^"]*)" as actor "([^"]*)"$`, h.milestonesRecorded)
	sc.Step(`^stdout has the line "([^"]*)"$`, h.stdoutHasTheLine)
}

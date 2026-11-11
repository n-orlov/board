package features

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"regexp"
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

// stdoutHasNoLineStartingWith checks no line of stdout starts with the prefix.
func (h *harness) stdoutHasNoLineStartingWith(prefix string) error {
	for _, line := range strings.Split(h.stdout, "\n") {
		if strings.HasPrefix(line, prefix) {
			return fmt.Errorf("stdout has the line %q, which starts with %q", line, prefix)
		}
	}
	return nil
}

// eventRefPattern is the ref an event line starts with.
var eventRefPattern = regexp.MustCompile(`^(E[0-9]+)  `)

// rememberEventRef keeps the E<n> ref that starts line n of stdout; a later
// client command line may name it as <event>.
func (h *harness) rememberEventRef(n int) error {
	lines := strings.Split(strings.TrimSuffix(h.stdout, "\n"), "\n")
	if n < 1 || n > len(lines) {
		return fmt.Errorf("stdout %q has %d lines, want a line %d", h.stdout, len(lines), n)
	}
	m := eventRefPattern.FindStringSubmatch(lines[n-1])
	if m == nil {
		return fmt.Errorf("stdout line %d %q does not start with an event ref", n, lines[n-1])
	}
	h.eventRef = m[1]
	return nil
}

// stdoutJSONArrayRefs checks stdout is one JSON array whose members' ref
// fields are the comma-separated refs, in order.
func (h *harness) stdoutJSONArrayRefs(want string) error {
	var items []struct {
		Ref string `json:"ref"`
	}
	if err := json.Unmarshal([]byte(h.stdout), &items); err != nil || strings.Count(h.stdout, "\n") != 1 {
		return fmt.Errorf("stdout %q is not one JSON array line", h.stdout)
	}
	refs := make([]string, len(items))
	for i, it := range items {
		refs[i] = it.Ref
	}
	if got := strings.Join(refs, ","); got != want {
		return fmt.Errorf("stdout refs %q, want %q", got, want)
	}
	return nil
}

func registerCLITaskSteps(sc *godog.ScenarioContext, h *harness) {
	sc.Step(`^(\d+) tasks exist in the initiative "([^"]*)" as actor "([^"]*)"$`, h.tasksExist)
	sc.Step(`^(\d+) milestones are recorded on the task "([^"]*)" as actor "([^"]*)"$`, h.milestonesRecorded)
	sc.Step(`^the event ref on line (\d+) of stdout is remembered$`, h.rememberEventRef)
	sc.Step(`^stdout is a JSON array whose refs are "([^"]*)"$`, h.stdoutJSONArrayRefs)
	sc.Step(`^stdout has the line "([^"]*)"$`, h.stdoutHasTheLine)
	sc.Step(`^stdout has the line '([^']*)'$`, h.stdoutHasTheLine)
	sc.Step(`^stdout has no line starting with "([^"]*)"$`, h.stdoutHasNoLineStartingWith)
}

package features

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"sort"
	"strings"

	"github.com/cucumber/godog"
)

// checkIn asks GET /api/v1/now, with the query when there is one.
func (h *harness) checkIn(ctx context.Context, query, actor string) error {
	p := "/api/v1/now"
	if query != "" {
		p += "?" + query
	}
	return h.sendTask(ctx, http.MethodGet, p, "", actor)
}

// membersAre checks the response is an object with exactly these members.
func (h *harness) membersAre(want string) error {
	var doc map[string]json.RawMessage
	if err := json.Unmarshal(h.api.body, &doc); err != nil {
		return fmt.Errorf("response %q: %w", h.api.body, err)
	}
	got := make([]string, 0, len(doc))
	for k := range doc {
		got = append(got, k)
	}
	sort.Strings(got)
	wanted := splitList(want)
	sort.Strings(wanted)
	if strings.Join(got, ",") != strings.Join(wanted, ",") {
		return fmt.Errorf("the response members are %q, want %q", strings.Join(got, ","), strings.Join(wanted, ","))
	}
	return nil
}

func registerNowSteps(sc *godog.ScenarioContext, h *harness) {
	sc.Step(`^I check in with "([^"]*)" as actor "([^"]*)"$`, h.checkIn)
	sc.Step(`^the response members are exactly "([^"]*)"$`, h.membersAre)
}

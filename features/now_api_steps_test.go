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

// checkInAttentionIs checks the attention member of the check-in: the five
// section counts and the total, in that order.
func (h *harness) checkInAttentionIs(want string) error {
	names := []string{"open_asks", "done_not_closed", "expired_leases", "overdue_watches", "conflicts", "total"}
	wanted := splitList(want)
	if len(wanted) != len(names) {
		return fmt.Errorf("%q names %d counts, want %d", want, len(wanted), len(names))
	}
	got := make([]string, len(names))
	for i, name := range names {
		v, ok, err := h.responsePath("attention." + name)
		if err != nil {
			return err
		}
		if !ok {
			return fmt.Errorf("the response %s has no attention.%s", h.api.body, name)
		}
		got[i] = fieldText(v)
	}
	if strings.Join(got, ",") != strings.Join(wanted, ",") {
		return fmt.Errorf("the check-in counts are %s, want %s (response %s)", strings.Join(got, ","), strings.Join(wanted, ","), h.api.body)
	}
	return nil
}

// postBroadcasts posts n broadcast notes as the actor.
func (h *harness) postBroadcasts(ctx context.Context, n int, actor string) error {
	for i := 1; i <= n; i++ {
		if err := h.postNote(ctx, fmt.Sprintf(`{"summary":"broadcast %d"}`, i), actor); err != nil {
			return err
		}
		if h.status != http.StatusCreated {
			return fmt.Errorf("posting broadcast %d answered %d: %s", i, h.status, h.api.body)
		}
	}
	return nil
}

func registerNowSteps(sc *godog.ScenarioContext, h *harness) {
	sc.Step(`^I check in with "([^"]*)" as actor "([^"]*)"$`, h.checkIn)
	sc.Step(`^the check-in attention counts are "([^"]*)"$`, h.checkInAttentionIs)
	sc.Step(`^(\d+) broadcast notes are posted as actor "([^"]*)"$`, h.postBroadcasts)
	sc.Step(`^the response members are exactly "([^"]*)"$`, h.membersAre)
}

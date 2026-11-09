package features

import (
	"context"
	"fmt"
	"net/http"
	"strings"

	"github.com/cucumber/godog"
)

// getAttention reads GET /api/v1/attention with the query, which may be empty.
func (h *harness) getAttention(ctx context.Context, query, actor string) error {
	p := "/api/v1/attention"
	if query != "" {
		p += "?" + query
	}
	return h.sendTask(ctx, http.MethodGet, p, "", actor)
}

// attentionCountsAre checks the six numbers of counts, in the order open_asks,
// done_not_closed, expired_leases, overdue_watches, conflicts, total.
func (h *harness) attentionCountsAre(want string) error {
	names := []string{"open_asks", "done_not_closed", "expired_leases", "overdue_watches", "conflicts", "total"}
	wanted := splitList(want)
	if len(wanted) != len(names) {
		return fmt.Errorf("%q names %d counts, want %d", want, len(wanted), len(names))
	}
	var got []string
	for _, name := range names {
		v, ok, err := h.responsePath("counts." + name)
		if err != nil {
			return err
		}
		if !ok {
			return fmt.Errorf("the response %s has no counts.%s", h.api.body, name)
		}
		got = append(got, fieldText(v))
	}
	if strings.Join(got, ",") != strings.Join(wanted, ",") {
		return fmt.Errorf("the counts are %s, want %s (response %s)", strings.Join(got, ","), strings.Join(wanted, ","), h.api.body)
	}
	return nil
}

func registerAttentionSteps(sc *godog.ScenarioContext, h *harness) {
	sc.Step(`^I request the attention with "([^"]*)" as actor "([^"]*)"$`, h.getAttention)
	sc.Step(`^the attention counts are "([^"]*)"$`, h.attentionCountsAre)
}

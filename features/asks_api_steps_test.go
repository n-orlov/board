package features

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/cucumber/godog"
)

func (h *harness) askPath(ref, sub string) string {
	p := "/api/v1/asks"
	if ref != "" {
		p += "/" + url.PathEscape(ref)
	}
	if sub != "" {
		p += "/" + sub
	}
	return p
}

// rememberAsk keeps the id of the ask a 201 answered by its ref.
func (h *harness) rememberAsk() error {
	if h.status != http.StatusCreated {
		return nil
	}
	var doc struct{ ID, Ref string }
	if err := json.Unmarshal(h.api.body, &doc); err != nil {
		return fmt.Errorf("response %q: %w", h.api.body, err)
	}
	if h.api.ids == nil {
		h.api.ids = map[string]string{}
	}
	h.api.ids["ask:"+doc.Ref] = doc.ID
	return nil
}

func (h *harness) raiseAsk(ctx context.Context, body, actor string) error {
	if err := h.sendTask(ctx, http.MethodPost, h.askPath("", ""), body, actor); err != nil {
		return err
	}
	return h.rememberAsk()
}

func (h *harness) raiseAskWithoutBody(ctx context.Context, actor string) error {
	return h.raiseAsk(ctx, "", actor)
}

func (h *harness) getAsk(ctx context.Context, ref, actor string) error {
	return h.sendTask(ctx, http.MethodGet, h.askPath(ref, ""), "", actor)
}

func (h *harness) getAskByID(ctx context.Context, ref, how, actor string) error {
	id, ok := h.api.ids["ask:"+ref]
	if !ok {
		return fmt.Errorf("no ask %q was raised", ref)
	}
	if how == "lowercase " {
		id = strings.ToLower(id)
	}
	return h.getAsk(ctx, id, actor)
}

func (h *harness) listAsks(ctx context.Context, query, actor string) error {
	p := h.askPath("", "")
	if query != "" {
		p += "?" + query
	}
	return h.sendTask(ctx, http.MethodGet, p, "", actor)
}

func (h *harness) nextAsksPage(ctx context.Context, query, actor string) error {
	after, err := h.nextAfter()
	if err != nil {
		return err
	}
	return h.listAsks(ctx, query+"&after="+url.QueryEscape(after), actor)
}

func (h *harness) answerAsk(ctx context.Context, ref, body, actor string) error {
	return h.sendTask(ctx, http.MethodPost, h.askPath(ref, "answer"), body, actor)
}

func (h *harness) answerAskWithoutBody(ctx context.Context, ref, actor string) error {
	return h.answerAsk(ctx, ref, "", actor)
}

func (h *harness) withdrawAsk(ctx context.Context, ref, body, actor string) error {
	return h.sendTask(ctx, http.MethodPost, h.askPath(ref, "withdraw"), body, actor)
}

func (h *harness) withdrawAskWithoutBody(ctx context.Context, ref, actor string) error {
	return h.withdrawAsk(ctx, ref, "", actor)
}

func askNumber(ref string) (int64, error) {
	n, err := strconv.ParseInt(strings.TrimPrefix(ref, "A"), 10, 64)
	if err != nil {
		return 0, fmt.Errorf("ask ref %q: %w", ref, err)
	}
	return n, nil
}

const askCounts = `SELECT (SELECT count(*) FROM asks) || '/' || (SELECT count(*) FROM events) || '/' || (SELECT count(*) FROM actors)`

func (h *harness) noteAskCounts(ctx context.Context) error {
	got, err := h.scalar(ctx, askCounts)
	h.api.askCounts = got
	return err
}

func (h *harness) askCountsUnchanged(ctx context.Context) error {
	got, err := h.scalar(ctx, askCounts)
	if err != nil {
		return err
	}
	if h.api.askCounts == "" || got != h.api.askCounts {
		return fmt.Errorf("asks/events/actors are %s, were %q", got, h.api.askCounts)
	}
	return nil
}

func (h *harness) askCount(ctx context.Context, want int) error {
	got, err := h.scalar(ctx, `SELECT count(*) FROM asks`)
	if err != nil {
		return err
	}
	if got != fmt.Sprint(want) {
		return fmt.Errorf("%s asks in the database, want %d", got, want)
	}
	return nil
}

func (h *harness) askStateIs(ctx context.Context, ref, want string) error {
	n, err := askNumber(ref)
	if err != nil {
		return err
	}
	got, err := h.scalar(ctx, `SELECT state FROM asks WHERE n = ?`, n)
	if err != nil {
		return err
	}
	if got != want {
		return fmt.Errorf("ask %s is %s in the database, want %s", ref, got, want)
	}
	return nil
}

func (h *harness) askEventCount(ctx context.Context, want int, typ, ref string) error {
	n, err := askNumber(ref)
	if err != nil {
		return err
	}
	got, err := h.scalar(ctx, `SELECT count(*) FROM events e JOIN asks k ON k.id = e.ref_id WHERE e.type = ? AND k.n = ?`, typ, n)
	if err != nil {
		return err
	}
	if got != fmt.Sprint(want) {
		return fmt.Errorf("%s %s events for %s, want %d", got, typ, ref, want)
	}
	return nil
}

// askEventScope checks the task and initiative slug the latest event of a type
// on the ask carries, each "" for none.
func (h *harness) askEventScope(ctx context.Context, typ, ref, task, slug string) error {
	n, err := askNumber(ref)
	if err != nil {
		return err
	}
	got, err := h.scalar(ctx, `SELECT COALESCE(i.slug || '-' || t.seq, '') || '|' || COALESCE(ei.slug, '') FROM events e
		JOIN asks k ON k.id = e.ref_id LEFT JOIN tasks t ON t.id = e.task_id LEFT JOIN initiatives i ON i.id = t.initiative_id
		LEFT JOIN initiatives ei ON ei.id = e.initiative_id WHERE e.type = ? AND k.n = ? ORDER BY e.n DESC LIMIT 1`, typ, n)
	if err != nil {
		return fmt.Errorf("read the %s event of %s: %w", typ, ref, err)
	}
	if want := task + "|" + slug; got != want {
		return fmt.Errorf("the %s event of %s is on %q, want %q", typ, ref, got, want)
	}
	return nil
}

func (h *harness) askEventPrincipal(ctx context.Context, typ, ref, principal, actor string) error {
	n, err := askNumber(ref)
	if err != nil {
		return err
	}
	got, err := h.scalar(ctx, `SELECT e.principal || '|' || a.handle FROM events e JOIN asks k ON k.id = e.ref_id
		JOIN actors a ON a.id = e.actor_id WHERE e.type = ? AND k.n = ? ORDER BY e.n DESC LIMIT 1`, typ, n)
	if err != nil {
		return fmt.Errorf("read the %s event of %s: %w", typ, ref, err)
	}
	if want := principal + "|" + actor; got != want {
		return fmt.Errorf("the %s event of %s has principal and actor %q, want %q", typ, ref, got, want)
	}
	return nil
}

func (h *harness) askEventOnBehalf(ctx context.Context, typ, ref, want string) error {
	n, err := askNumber(ref)
	if err != nil {
		return err
	}
	got, err := h.scalar(ctx, `SELECT COALESCE(ob.handle, '') FROM events e JOIN asks k ON k.id = e.ref_id
		LEFT JOIN actors ob ON ob.id = e.on_behalf_of WHERE e.type = ? AND k.n = ? ORDER BY e.n DESC LIMIT 1`, typ, n)
	if err != nil {
		return fmt.Errorf("read the %s event of %s: %w", typ, ref, err)
	}
	if got != want {
		return fmt.Errorf("the %s event of %s is on behalf of %q, want %q", typ, ref, got, want)
	}
	return nil
}

func registerAskSteps(sc *godog.ScenarioContext, h *harness) {
	sc.Step(`^I raise an ask with body "([^"]*)" as actor "([^"]*)"$`, h.raiseAsk)
	sc.Step(`^I raise an ask without a body as actor "([^"]*)"$`, h.raiseAskWithoutBody)
	sc.Step(`^I request the ask "([^"]*)" as actor "([^"]*)"$`, h.getAsk)
	sc.Step(`^I request the ask "([^"]*)" by its (lowercase )?id as actor "([^"]*)"$`, h.getAskByID)
	sc.Step(`^I list the asks with "([^"]*)" as actor "([^"]*)"$`, h.listAsks)
	sc.Step(`^I request the next asks page with "([^"]*)" as actor "([^"]*)"$`, h.nextAsksPage)
	sc.Step(`^I answer the ask "([^"]*)" with body "([^"]*)" as actor "([^"]*)"$`, h.answerAsk)
	sc.Step(`^I answer the ask "([^"]*)" without a body as actor "([^"]*)"$`, h.answerAskWithoutBody)
	sc.Step(`^I withdraw the ask "([^"]*)" with body "([^"]*)" as actor "([^"]*)"$`, h.withdrawAsk)
	sc.Step(`^I withdraw the ask "([^"]*)" without a body as actor "([^"]*)"$`, h.withdrawAskWithoutBody)
	sc.Step(`^I note the counts of asks, events and actors$`, h.noteAskCounts)
	sc.Step(`^the counts of asks, events and actors are unchanged$`, h.askCountsUnchanged)
	sc.Step(`^there are (\d+) asks in the database$`, h.askCount)
	sc.Step(`^the ask "([^"]*)" is "([^"]*)" in the database$`, h.askStateIs)
	sc.Step(`^exactly (\d+) (ask_[a-z]+) events? exists? for the ask "([^"]*)"$`, h.askEventCount)
	sc.Step(`^the latest (ask_[a-z]+) event of the ask "([^"]*)" is on the task "([^"]*)" and the initiative "([^"]*)"$`, h.askEventScope)
	sc.Step(`^the latest (ask_[a-z]+) event of the ask "([^"]*)" has principal "([^"]*)" and actor "([^"]*)"$`, h.askEventPrincipal)
	sc.Step(`^the latest (ask_[a-z]+) event of the ask "([^"]*)" is on behalf of "([^"]*)"$`, h.askEventOnBehalf)
}

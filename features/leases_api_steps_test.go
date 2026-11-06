package features

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/cucumber/godog"
)

func (h *harness) leasePath(ref, sub string) string {
	p := "/api/v1/leases"
	if ref != "" {
		p += "/" + url.PathEscape(ref)
	}
	if sub != "" {
		p += "/" + sub
	}
	return p
}

func (h *harness) startLease(ctx context.Context, body, actor string) error {
	return h.sendTask(ctx, http.MethodPost, h.leasePath("", ""), body, actor)
}

func (h *harness) startLeaseWithoutBody(ctx context.Context, actor string) error {
	return h.startLease(ctx, "", actor)
}

func (h *harness) renewLease(ctx context.Context, ref, body, actor string) error {
	return h.sendTask(ctx, http.MethodPost, h.leasePath(ref, "renew"), body, actor)
}

func (h *harness) renewLeasePlain(ctx context.Context, ref, actor string) error {
	return h.renewLease(ctx, ref, "", actor)
}

func (h *harness) endLease(ctx context.Context, ref, body, actor string) error {
	return h.sendTask(ctx, http.MethodPost, h.leasePath(ref, "end"), body, actor)
}

func (h *harness) endLeasePlain(ctx context.Context, ref, actor string) error {
	return h.endLease(ctx, ref, "", actor)
}

func (h *harness) ackLease(ctx context.Context, ref, actor string) error {
	return h.sendTask(ctx, http.MethodPost, h.leasePath(ref, "ack"), "", actor)
}

func (h *harness) getLease(ctx context.Context, ref, actor string) error {
	return h.sendTask(ctx, http.MethodGet, h.leasePath(ref, ""), "", actor)
}

func (h *harness) listLeases(ctx context.Context, query, actor string) error {
	p := h.leasePath("", "")
	if query != "" {
		p += "?" + query
	}
	return h.sendTask(ctx, http.MethodGet, p, "", actor)
}

func (h *harness) nextLeasesPage(ctx context.Context, query, actor string) error {
	after, err := h.nextAfter()
	if err != nil {
		return err
	}
	return h.listLeases(ctx, query+"&after="+url.QueryEscape(after), actor)
}

// exec runs a write on the scenario's database.
func (h *harness) exec(ctx context.Context, query string, args ...any) (int64, error) {
	db, err := h.openDB()
	if err != nil {
		return 0, err
	}
	defer db.Close()
	if h.leg == "postgres" {
		for i := range args {
			query = strings.Replace(query, "?", fmt.Sprintf("$%d", i+1), 1)
		}
	}
	res, err := db.ExecContext(ctx, query, args...)
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}

func leaseNumber(ref string) (int64, error) {
	n, err := strconv.ParseInt(strings.TrimPrefix(ref, "L"), 10, 64)
	if err != nil {
		return 0, fmt.Errorf("lease ref %q: %w", ref, err)
	}
	return n, nil
}

// expireLease backdates expires_at of the lease: no sleep, the row says the
// time has run out.
func (h *harness) expireLease(ctx context.Context, ref string, seconds int) error {
	n, err := leaseNumber(ref)
	if err != nil {
		return err
	}
	at := time.Now().Add(-time.Duration(seconds) * time.Second).UnixMilli()
	rows, err := h.exec(ctx, `UPDATE leases SET expires_at = ? WHERE n = ?`, at, n)
	if err != nil {
		return err
	}
	if rows != 1 {
		return fmt.Errorf("%d leases backdated for %s, want 1", rows, ref)
	}
	return nil
}

func (h *harness) leaseCount(ctx context.Context, want int) error {
	got, err := h.scalar(ctx, `SELECT count(*) FROM leases`)
	if err != nil {
		return err
	}
	if got != fmt.Sprint(want) {
		return fmt.Errorf("%s leases in the database, want %d", got, want)
	}
	return nil
}

func (h *harness) leaseStateIs(ctx context.Context, ref, want string) error {
	n, err := leaseNumber(ref)
	if err != nil {
		return err
	}
	got, err := h.scalar(ctx, `SELECT state FROM leases WHERE n = ?`, n)
	if err != nil {
		return err
	}
	if got != want {
		return fmt.Errorf("lease %s is %s in the database, want %s", ref, got, want)
	}
	return nil
}

// pathIsTimePlus checks a time in the response is the request time plus the
// seconds, to the millisecond the database keeps.
func (h *harness) pathIsTimePlus(path string, seconds int) error {
	got, ok, err := h.responsePath(path)
	if err != nil {
		return err
	}
	if !ok {
		return fmt.Errorf("no %s in %s", path, h.api.body)
	}
	at, err := time.Parse(time.RFC3339Nano, fmt.Sprint(got))
	if err != nil {
		return err
	}
	d := time.Duration(seconds) * time.Second
	if at.Before(h.api.sentAt.Add(d).Truncate(time.Millisecond)) || at.After(h.api.answeredAt.Add(d)) {
		return fmt.Errorf("%s %s is not between the request time %s and %s plus %s", path, at, h.api.sentAt, h.api.answeredAt, d)
	}
	return nil
}

// expiredAtIsStored checks the response's expired_at is the expires_at the
// lease row holds.
func (h *harness) expiredAtIsStored(ctx context.Context, ref string) error {
	return h.pathIsStoredExpiry(ctx, "expired_at", ref)
}

// pathIsStoredExpiry checks the time at path is the expires_at stored for the lease.
func (h *harness) pathIsStoredExpiry(ctx context.Context, path, ref string) error {
	n, err := leaseNumber(ref)
	if err != nil {
		return err
	}
	stored, err := h.scalar(ctx, `SELECT expires_at FROM leases WHERE n = ?`, n)
	if err != nil {
		return err
	}
	ms, err := strconv.ParseInt(stored, 10, 64)
	if err != nil {
		return err
	}
	got, ok, err := h.responsePath(path)
	if err != nil {
		return err
	}
	if !ok {
		return fmt.Errorf("no %s in %s", path, h.api.body)
	}
	at, err := time.Parse(time.RFC3339Nano, fmt.Sprint(got))
	if err != nil {
		return err
	}
	if at.UnixMilli() != ms {
		return fmt.Errorf("%s %s is not the stored expires_at %d", path, at, ms)
	}
	return nil
}

func (h *harness) leaseEventCount(ctx context.Context, want int, typ, ref string) error {
	n, err := leaseNumber(ref)
	if err != nil {
		return err
	}
	got, err := h.scalar(ctx, `SELECT count(*) FROM events e JOIN leases l ON l.id = e.ref_id WHERE e.type = ? AND l.n = ?`, typ, n)
	if err != nil {
		return err
	}
	if got != fmt.Sprint(want) {
		return fmt.Errorf("%s %s events for %s, want %d", got, typ, ref, want)
	}
	return nil
}

// leaseStateWithin polls the lease row for up to the seconds until it holds
// the state, for the background sweeper, which writes on its own schedule.
func (h *harness) leaseStateWithin(ctx context.Context, seconds int, ref, want string) error {
	deadline := time.Now().Add(time.Duration(seconds) * time.Second)
	for {
		err := h.leaseStateIs(ctx, ref, want)
		if err == nil || time.Now().After(deadline) {
			return err
		}
		time.Sleep(50 * time.Millisecond)
	}
}

func registerLeaseSteps(sc *godog.ScenarioContext, h *harness) {
	sc.Step(`^I start a lease with body "([^"]*)" as actor "([^"]*)"$`, h.startLease)
	sc.Step(`^I start a lease without a body as actor "([^"]*)"$`, h.startLeaseWithoutBody)
	sc.Step(`^I renew the lease "([^"]*)" with body "([^"]*)" as actor "([^"]*)"$`, h.renewLease)
	sc.Step(`^I renew the lease "([^"]*)" without a body as actor "([^"]*)"$`, h.renewLeasePlain)
	sc.Step(`^I end the lease "([^"]*)" with body "([^"]*)" as actor "([^"]*)"$`, h.endLease)
	sc.Step(`^I end the lease "([^"]*)" without a body as actor "([^"]*)"$`, h.endLeasePlain)
	sc.Step(`^I acknowledge the lease "([^"]*)" as actor "([^"]*)"$`, h.ackLease)
	sc.Step(`^I request the lease "([^"]*)" as actor "([^"]*)"$`, h.getLease)
	sc.Step(`^I list the leases with "([^"]*)" as actor "([^"]*)"$`, h.listLeases)
	sc.Step(`^I request the next leases page with "([^"]*)" as actor "([^"]*)"$`, h.nextLeasesPage)
	sc.Step(`^the lease "([^"]*)" ran out (\d+) seconds ago$`, h.expireLease)
	sc.Step(`^within (\d+) seconds the lease "([^"]*)" is "([^"]*)" in the database$`, h.leaseStateWithin)
	sc.Step(`^there are (\d+) leases in the database$`, h.leaseCount)
	sc.Step(`^the lease "([^"]*)" is "([^"]*)" in the database$`, h.leaseStateIs)
	sc.Step(`^the response path "([^"]*)" is the request time plus (\d+) seconds$`, h.pathIsTimePlus)
	sc.Step(`^the response expired_at is the expiry stored for the lease "([^"]*)"$`, h.expiredAtIsStored)
	sc.Step(`^the response path "([^"]*)" is the expiry stored for the lease "([^"]*)"$`, h.pathIsStoredExpiry)
	sc.Step(`^exactly (\d+) ([a-z_]+) events? exists? for the lease "([^"]*)"$`, h.leaseEventCount)
}

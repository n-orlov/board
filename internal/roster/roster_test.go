package roster_test

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"github.com/n-orlov/village-hall/internal/asks"
	"github.com/n-orlov/village-hall/internal/audit"
	"github.com/n-orlov/village-hall/internal/initiatives"
	"github.com/n-orlov/village-hall/internal/leases"
	"github.com/n-orlov/village-hall/internal/roster"
	"github.com/n-orlov/village-hall/internal/store"
	"github.com/n-orlov/village-hall/internal/tasks"
)

var ctx = context.Background()

type env struct {
	st *store.Store
	r  roster.Store
	tk tasks.Store
	ls leases.Store
	ak asks.Store
}

func newEnv(t *testing.T) *env {
	t.Helper()
	st, err := store.Open("sqlite://" + filepath.Join(t.TempDir(), "b.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	if err := st.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	e := &env{st: st, tk: tasks.Store{Store: st}, ls: leases.Store{Store: st}, ak: asks.Store{Store: st}}
	e.r = roster.Store{Store: st, Leases: &e.ls, Asks: &e.ak}
	if _, err := (initiatives.Store{Store: st}).Create(ctx, audit.Actor{Handle: "setup@host"}, "key:k", initiatives.New{Slug: "aa", Title: "A"}); err != nil {
		t.Fatal(err)
	}
	return e
}

func (e *env) task(t *testing.T, title string) string {
	t.Helper()
	got, err := e.tk.Create(ctx, audit.Actor{Handle: "setup@host"}, "key:k", tasks.New{Initiative: "aa", Title: title})
	if err != nil {
		t.Fatal(err)
	}
	return got.Ref
}

func (e *env) lease(t *testing.T, handle, task string, resources ...string) string {
	t.Helper()
	got, err := e.ls.Start(ctx, audit.Actor{Handle: handle}, "key:k", leases.Start{Task: task, Intent: "work on " + task, Resources: resources})
	if err != nil {
		t.Fatal(err)
	}
	return got.Lease.Ref
}

func (e *env) ask(t *testing.T, handle, task, question string) asks.Ask {
	t.Helper()
	k, err := e.ak.Raise(ctx, audit.Actor{Handle: handle}, "key:k", asks.New{Task: task, Question: question})
	if err != nil {
		t.Fatal(err)
	}
	return k
}

func (e *env) seen(t *testing.T, handle string, at time.Time) {
	t.Helper()
	if _, err := e.st.Writer().ExecContext(ctx, e.st.Dialect().Rebind(`UPDATE actors SET last_seen_at = ? WHERE handle = ?`), store.EncodeTime(at), handle); err != nil {
		t.Fatal(err)
	}
}

func handles(list []roster.Actor) []string {
	out := []string{}
	for _, a := range list {
		out = append(out, a.Handle)
	}
	return out
}

func TestListOrdersByLastSeenNewestFirstAndTiesByHandle(t *testing.T) {
	e := newEnv(t)
	base := time.Date(2026, 11, 12, 12, 0, 0, 0, time.UTC)
	e.lease(t, "mid@host", e.task(t, "one"))
	e.lease(t, "new@host", e.task(t, "two"))
	e.lease(t, "old@host", e.task(t, "three"))
	e.lease(t, "also-mid@host", e.task(t, "four"))
	e.seen(t, "setup@host", base.Add(-time.Hour))
	e.seen(t, "old@host", base.Add(-10*time.Minute))
	e.seen(t, "mid@host", base.Add(-5*time.Minute))
	e.seen(t, "also-mid@host", base.Add(-5*time.Minute))
	e.seen(t, "new@host", base)
	got, err := e.r.List(ctx, false)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"new@host", "also-mid@host", "mid@host", "old@host", "setup@host"}
	if !reflect.DeepEqual(handles(got), want) {
		t.Errorf("order = %v, want %v", handles(got), want)
	}
	if !got[0].LastSeen.Equal(base) {
		t.Errorf("last seen = %v, want %v", got[0].LastSeen, base)
	}
}

func TestAnActorCarriesItsLiveLeasesAndOpenAsksOnly(t *testing.T) {
	e := newEnv(t)
	one, two, three := e.task(t, "one"), e.task(t, "two"), e.task(t, "three")
	first := e.lease(t, "claude@host", one, "repo:b", "repo:a")
	e.lease(t, "claude@host", two)
	ended := e.lease(t, "claude@host", three)
	if _, err := e.ls.End(ctx, audit.Actor{Handle: "claude@host"}, "key:k", ended, ""); err != nil {
		t.Fatal(err)
	}
	open := e.ask(t, "claude@host", one, "Which one?")
	gone := e.ask(t, "claude@host", two, "Never mind?")
	if _, err := e.ak.Withdraw(ctx, audit.Actor{Handle: "claude@host"}, "key:k", gone.Ref, "no longer"); err != nil {
		t.Fatal(err)
	}
	e.ask(t, "other@host", one, "Not mine")
	a, err := e.r.Get(ctx, "claude@host")
	if err != nil {
		t.Fatal(err)
	}
	if len(a.Leases) != 2 || a.Leases[0].Ref != first || a.Leases[0].Task != one || !reflect.DeepEqual(a.Leases[0].Resources, []string{"repo:a", "repo:b"}) {
		t.Errorf("leases = %+v, want the two live ones, oldest first, the first on %s with sorted resources", a.Leases, one)
	}
	if len(a.Asks) != 1 || a.Asks[0].Ref != open.Ref || a.Asks[0].Question != "Which one?" {
		t.Errorf("asks = %+v, want only the open ask of this actor", a.Asks)
	}
	if a.Kind != "agent" || a.WayBack != "" || !a.Alive() {
		t.Errorf("actor = %+v", a)
	}
}

func TestAliveKeepsOnlyActorsWithALiveLeaseAndExpiresThoseThatRanOut(t *testing.T) {
	e := newEnv(t)
	live := e.lease(t, "live@host", e.task(t, "one"))
	e.lease(t, "late@host", e.task(t, "two"))
	e.ask(t, "asker@host", "", "Only asking")
	if _, err := e.st.Writer().ExecContext(ctx, e.st.Dialect().Rebind(`UPDATE leases SET expires_at = ? WHERE n = 2`), store.EncodeTime(time.Now().Add(-time.Minute))); err != nil {
		t.Fatal(err)
	}
	got, err := e.r.List(ctx, true)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(handles(got), []string{"live@host"}) || got[0].Leases[0].Ref != live {
		t.Errorf("alive = %v, want only live@host", handles(got))
	}
	all, err := e.r.List(ctx, false)
	if err != nil {
		t.Fatal(err)
	}
	if len(all) != 4 {
		t.Errorf("all = %v, want setup, live, late and asker", handles(all))
	}
	late, err := e.r.Get(ctx, "late@host")
	if err != nil || len(late.Leases) != 0 || late.Alive() {
		t.Errorf("late = %+v, %v, want no live lease", late, err)
	}
}

func TestEveryPageOfLeasesAndAsksIsRead(t *testing.T) {
	e := newEnv(t)
	e.r.Page = 2
	for i := 0; i < 5; i++ {
		task := e.task(t, fmt.Sprint("t", i))
		e.lease(t, "a@host", task)
		e.ask(t, "a@host", task, fmt.Sprint("q", i))
		e.lease(t, fmt.Sprint("b", i, "@host"), e.task(t, fmt.Sprint("u", i)))
	}
	a, err := e.r.Get(ctx, "a@host")
	if err != nil {
		t.Fatal(err)
	}
	if len(a.Leases) != 5 || len(a.Asks) != 5 {
		t.Errorf("a holds %d leases and %d asks, want 5 and 5", len(a.Leases), len(a.Asks))
	}
	all, err := e.r.List(ctx, true)
	if err != nil {
		t.Fatal(err)
	}
	if len(all) != 6 {
		t.Errorf("alive = %v, want a and b0..b4", handles(all))
	}
	if n := len(all[len(all)-1].Leases) + len(all[0].Leases); n < 2 {
		t.Errorf("leases of the first and last = %d", n)
	}
}

func TestGetAnUnknownHandleIsNotFound(t *testing.T) {
	e := newEnv(t)
	_, err := e.r.Get(ctx, "nobody@host")
	if !errors.Is(err, roster.ErrNotFound) {
		t.Errorf("err = %v, want ErrNotFound", err)
	}
}

func TestTheWayBackAndKindAreRead(t *testing.T) {
	e := newEnv(t)
	if _, err := e.st.Writer().ExecContext(ctx, e.st.Dialect().Rebind(`UPDATE actors SET way_back = ? WHERE handle = ?`), "tmux attach", "setup@host"); err != nil {
		t.Fatal(err)
	}
	a, err := e.r.Get(ctx, "setup@host")
	if err != nil || a.WayBack != "tmux attach" || a.Handle != "setup@host" {
		t.Errorf("actor = %+v, %v", a, err)
	}
}

func TestAReadFailureIsReturned(t *testing.T) {
	e := newEnv(t)
	if err := e.st.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := e.r.List(ctx, false); err == nil {
		t.Error("List on a closed database returned no error")
	}
	if _, err := e.r.Get(ctx, "setup@host"); err == nil || errors.Is(err, roster.ErrNotFound) {
		t.Errorf("Get on a closed database = %v, want a read error that is not ErrNotFound", err)
	}
}

func TestAFailureReadingLeasesOrAsksIsReturned(t *testing.T) {
	for _, table := range []string{"leases", "asks"} {
		t.Run(table, func(t *testing.T) {
			e := newEnv(t)
			if _, err := e.st.Writer().ExecContext(ctx, `ALTER TABLE `+table+` RENAME TO `+table+`_gone`); err != nil {
				t.Fatal(err)
			}
			if _, err := e.r.List(ctx, false); err == nil {
				t.Errorf("List without the %s table returned no error", table)
			}
		})
	}
}

func TestAnActorShowsTheOriginItsLatestKeyedWriteLeft(t *testing.T) {
	e := newEnv(t)
	op := audit.Actor{Handle: "alice-op", Operator: true}
	if _, err := (initiatives.Store{Store: e.st}).Create(ctx, op, "oidc:a@example.com", initiatives.New{Slug: "bb", Title: "B"}); err != nil {
		t.Fatal(err)
	}
	if _, err := (initiatives.Store{Store: e.st}).Create(audit.WithOrigin(ctx, "alice-op"), audit.Actor{Handle: "claude@host"}, "key:k", initiatives.New{Slug: "cc", Title: "C"}); err != nil {
		t.Fatal(err)
	}
	got := map[string]string{}
	list, err := e.r.List(ctx, false)
	if err != nil {
		t.Fatal(err)
	}
	for _, a := range list {
		got[a.Handle] = a.Origin
	}
	want := map[string]string{"setup@host": "", "alice-op": "", "claude@host": "alice-op"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("origins = %v, want %v", got, want)
	}
	one, err := e.r.Get(ctx, "claude@host")
	if err != nil || one.Origin != "alice-op" {
		t.Errorf("Get = %+v, %v, want origin alice-op", one, err)
	}
}

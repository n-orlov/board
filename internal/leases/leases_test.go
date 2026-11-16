package leases_test

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/n-orlov/village-hall/internal/audit"
	"github.com/n-orlov/village-hall/internal/domain"
	"github.com/n-orlov/village-hall/internal/initiatives"
	"github.com/n-orlov/village-hall/internal/leases"
	"github.com/n-orlov/village-hall/internal/store"
	"github.com/n-orlov/village-hall/internal/tasks"
)

var (
	t0  = time.Date(2026, 11, 5, 12, 0, 0, 0, time.UTC)
	ctx = context.Background()
	me  = audit.Actor{Handle: "claude@host"}
	bob = audit.Actor{Handle: "codex@host"}
)

// clock is a settable time source.
type clock struct{ now time.Time }

func (c *clock) Now() time.Time      { return c.now }
func (c *clock) add(d time.Duration) { c.now = c.now.Add(d) }

type env struct {
	S     leases.Store
	Tasks tasks.Store
	Clock *clock
}

// newEnv is a migrated SQLite database, an initiative "aa" with open tasks aa-1
// and aa-2, and a clock the test moves by hand.
func newEnv(t *testing.T) env {
	t.Helper()
	st, err := store.Open("sqlite://" + filepath.Join(t.TempDir(), "b.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	if err := st.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	c := &clock{now: t0}
	if _, err := (initiatives.Store{Store: st, Now: c.Now}).Create(ctx, me, "key:x", initiatives.New{Slug: "aa", Title: "I"}); err != nil {
		t.Fatal(err)
	}
	e := env{S: leases.Store{Store: st, Now: c.Now}, Tasks: tasks.Store{Store: st, Now: c.Now}, Clock: c}
	for i := 0; i < 2; i++ {
		c.add(time.Second)
		if _, err := e.Tasks.Create(ctx, me, "key:x", tasks.New{Initiative: "aa", Title: "t"}); err != nil {
			t.Fatal(err)
		}
	}
	return e
}

func (e env) start(t *testing.T, a audit.Actor, task string, ttl *int64, res ...string) leases.Result {
	t.Helper()
	e.Clock.add(time.Second)
	r, err := e.S.Start(ctx, a, "key:"+a.Handle, leases.Start{Task: task, Intent: "work on " + task, TTL: ttl, Resources: res})
	if err != nil {
		t.Fatalf("start %s: %v", task, err)
	}
	return r
}

func secs(v int64) *int64 { return &v }

type event struct {
	Type, TaskID, RefID, Summary, Principal, Actor string
	Data                                           map[string]any
}

func (e env) events(t *testing.T, typ string) []event {
	t.Helper()
	rows, err := e.S.Store.Reader().QueryContext(ctx, `SELECT e.type, COALESCE(e.task_id, ''), COALESCE(e.ref_id, ''), e.summary, e.principal, a.handle, e.data
		FROM events e JOIN actors a ON a.id = e.actor_id WHERE e.type = ? ORDER BY e.n`, typ)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = rows.Close() }()
	var out []event
	for rows.Next() {
		var ev event
		var data string
		if err := rows.Scan(&ev.Type, &ev.TaskID, &ev.RefID, &ev.Summary, &ev.Principal, &ev.Actor, &data); err != nil {
			t.Fatal(err)
		}
		if err := json.Unmarshal([]byte(data), &ev.Data); err != nil {
			t.Fatal(err)
		}
		out = append(out, ev)
	}
	return out
}

func TestStartWritesALiveLeaseNumberedLAndALeaseStartedEvent(t *testing.T) {
	e := newEnv(t)
	r := e.start(t, me, "aa-1", nil, "repo:x", "host:y", "repo:x")
	l := r.Lease
	if l.Ref != "L1" || l.Task != "aa-1" || l.Actor != "claude@host" || l.State != "live" || l.Intent != "work on aa-1" {
		t.Errorf("lease = %+v", l)
	}
	if l.TTLSeconds != 7200 || !l.ExpiresAt.Equal(l.StartedAt.Add(2*time.Hour)) || !l.RenewedAt.Equal(l.StartedAt) {
		t.Errorf("times = %+v", l)
	}
	if !reflect.DeepEqual(l.Resources, []string{"host:y", "repo:x"}) || r.Renewed || len(r.Conflicts) != 0 {
		t.Errorf("resources %v renewed %v conflicts %v", l.Resources, r.Renewed, r.Conflicts)
	}
	evs := e.events(t, leases.EventStarted)
	if len(evs) != 1 || evs[0].TaskID != l.TaskID || evs[0].RefID != l.ID || evs[0].Principal != "key:claude@host" ||
		evs[0].Data["ref"] != "L1" || evs[0].Data["task"] != "aa-1" || evs[0].Data["ttl_seconds"] != float64(7200) ||
		!strings.HasPrefix(evs[0].Summary, "L1 started on aa-1") {
		t.Errorf("events = %+v", evs)
	}
	if second := e.start(t, bob, "aa-1", nil); second.Lease.Ref != "L2" {
		t.Errorf("second lease ref = %s", second.Lease.Ref)
	}
}

func TestStartClampsTheTTL(t *testing.T) {
	for _, tc := range []struct {
		in   *int64
		want int64
	}{{secs(59), 60}, {secs(43201), 43200}, {nil, 7200}, {secs(60), 60}, {secs(43200), 43200}} {
		e := newEnv(t)
		r := e.start(t, me, "aa-1", tc.in)
		if r.Lease.TTLSeconds != tc.want || !r.Lease.ExpiresAt.Equal(r.Lease.StartedAt.Add(time.Duration(tc.want)*time.Second)) {
			t.Errorf("ttl %v -> %d, expires %v", tc.in, r.Lease.TTLSeconds, r.Lease.ExpiresAt)
		}
	}
}

func TestStartBySameActorOnSameTaskRenewsThatLease(t *testing.T) {
	e := newEnv(t)
	first := e.start(t, me, "aa-1", secs(600), "r1")
	e.Clock.add(5 * time.Minute)
	r, err := e.S.Start(ctx, me, "key:c", leases.Start{Task: "AA-1", Intent: "new intent", TTL: secs(1200)})
	if err != nil {
		t.Fatal(err)
	}
	l := r.Lease
	if !r.Renewed || l.Ref != first.Lease.Ref || l.Intent != "new intent" || l.TTLSeconds != 1200 {
		t.Errorf("renewed start = %+v", r)
	}
	if !l.ExpiresAt.Equal(e.Clock.now.Add(20*time.Minute)) || !l.StartedAt.Equal(first.Lease.StartedAt) || !l.RenewedAt.Equal(e.Clock.now) {
		t.Errorf("times = %+v", l)
	}
	if !reflect.DeepEqual(l.Resources, []string{"r1"}) {
		t.Errorf("resources left out of a renewing start must be kept, got %v", l.Resources)
	}
	if n := len(e.events(t, leases.EventStarted)); n != 1 {
		t.Errorf("lease_started events = %d, want 1", n)
	}
	if ev := e.events(t, leases.EventRenewed); len(ev) != 1 || ev[0].Data["ref"] != "L1" || ev[0].RefID != l.ID {
		t.Errorf("lease_renewed events = %+v", ev)
	}
	again, err := e.S.Start(ctx, me, "key:c", leases.Start{Task: "aa-1", Intent: "x", Resources: []string{"r2"}})
	if err != nil || !reflect.DeepEqual(again.Lease.Resources, []string{"r2"}) {
		t.Errorf("a renewing start with resources replaces them: %+v, %v", again.Lease.Resources, err)
	}
}

func TestStartByAnotherActorOrOnAnotherTaskIsANewLease(t *testing.T) {
	e := newEnv(t)
	e.start(t, me, "aa-1", nil)
	if r := e.start(t, bob, "aa-1", nil); r.Renewed || r.Lease.Ref != "L2" {
		t.Errorf("other actor: %+v", r)
	}
	if r := e.start(t, me, "aa-2", nil); r.Renewed || r.Lease.Ref != "L3" {
		t.Errorf("other task: %+v", r)
	}
}

func TestStartAfterTheLeaseExpiredIsANewLease(t *testing.T) {
	e := newEnv(t)
	first := e.start(t, me, "aa-1", secs(60))
	e.Clock.add(2 * time.Minute)
	second := e.start(t, me, "aa-1", nil)
	if second.Renewed || second.Lease.Ref == first.Lease.Ref {
		t.Errorf("start after expiry = %+v", second)
	}
	if got, _ := e.S.Get(ctx, first.Lease.Ref); got.State != "expired" {
		t.Errorf("first lease state = %s", got.State)
	}
}

func TestStartOnAClosedTaskIsRefusedAndWritesNothing(t *testing.T) {
	e := newEnv(t)
	if _, err := e.Tasks.Close(ctx, me, "key:x", "aa-1", "dropped"); err != nil {
		t.Fatal(err)
	}
	_, err := e.S.Start(ctx, bob, "key:b", leases.Start{Task: "aa-1", Intent: "x"})
	var ce leases.ConflictError
	if !errors.Is(err, leases.ErrConflict) || !errors.As(err, &ce) || !strings.Contains(err.Error(), "aa-1") {
		t.Fatalf("err = %v", err)
	}
	var n int
	if err := e.S.Store.Reader().QueryRow(`SELECT COUNT(*) FROM leases`).Scan(&n); err != nil || n != 0 {
		t.Errorf("leases = %d, %v", n, err)
	}
	if err := e.S.Store.Reader().QueryRow(`SELECT COUNT(*) FROM actors WHERE handle = 'codex@host'`).Scan(&n); err != nil || n != 0 {
		t.Errorf("a refused start recorded the actor: %d", n)
	}
}

func TestStartRefusesWhatIsInvalidOrUnknown(t *testing.T) {
	e := newEnv(t)
	for name, in := range map[string]leases.Start{
		"empty intent":   {Task: "aa-1", Intent: " "},
		"long intent":    {Task: "aa-1", Intent: strings.Repeat("x", 201)},
		"empty resource": {Task: "aa-1", Intent: "x", Resources: []string{""}},
		"long resource":  {Task: "aa-1", Intent: "x", Resources: []string{strings.Repeat("x", 101)}},
	} {
		if _, err := e.S.Start(ctx, me, "key:x", in); !errors.Is(err, leases.ErrInvalid) {
			t.Errorf("%s: err = %v, want ErrInvalid", name, err)
		}
	}
	many := make([]string, 21)
	for i := range many {
		many[i] = strings.Repeat("r", i+1)
	}
	if _, err := e.S.Start(ctx, me, "key:x", leases.Start{Task: "aa-1", Intent: "x", Resources: many}); !errors.Is(err, leases.ErrInvalid) {
		t.Errorf("21 resources: %v", err)
	}
	for _, task := range []string{"aa-9", "nope", "zz-1"} {
		if _, err := e.S.Start(ctx, me, "key:x", leases.Start{Task: task, Intent: "x"}); !errors.Is(err, leases.ErrNotFound) {
			t.Errorf("task %q: err = %v, want ErrNotFound", task, err)
		}
	}
}

func TestStartReturnsTheConflictsOfOtherLiveLeases(t *testing.T) {
	e := newEnv(t)
	e.start(t, bob, "aa-2", nil, "repo:a", "host:z")
	r := e.start(t, me, "aa-1", nil, "repo:a", "other")
	if len(r.Conflicts) != 1 {
		t.Fatalf("conflicts = %+v", r.Conflicts)
	}
	c := r.Conflicts[0]
	if c.Lease != "L1" || c.Holder != "codex@host" || c.Task != "aa-2" || c.Intent != "work on aa-2" ||
		!reflect.DeepEqual(c.Resources, []string{"repo:a"}) || c.ExpiresAt.IsZero() {
		t.Errorf("conflict = %+v", c)
	}
}

func TestAnExpiredOrEndedLeaseDoesNotConflict(t *testing.T) {
	e := newEnv(t)
	gone := e.start(t, bob, "aa-2", secs(60), "res")
	e.Clock.add(2 * time.Minute)
	ended := e.start(t, bob, "aa-1", nil, "res")
	if _, err := e.S.End(ctx, bob, "key:b", ended.Lease.Ref, ""); err != nil {
		t.Fatal(err)
	}
	if r := e.start(t, me, "aa-1", nil, "res"); len(r.Conflicts) != 0 {
		t.Errorf("conflicts with %s and %s = %+v", gone.Lease.Ref, ended.Lease.Ref, r.Conflicts)
	}
}

func TestRenewPushesExpiresAtAndKeepsTheTTLUnlessGiven(t *testing.T) {
	e := newEnv(t)
	l := e.start(t, me, "aa-1", secs(600), "r").Lease
	e.Clock.add(5 * time.Minute)
	r, err := e.S.Renew(ctx, me, "key:c", "l1", leases.Renew{})
	if err != nil {
		t.Fatal(err)
	}
	if !r.Lease.ExpiresAt.Equal(e.Clock.now.Add(10*time.Minute)) || !r.Lease.RenewedAt.Equal(e.Clock.now) || r.Lease.TTLSeconds != 600 ||
		!r.Lease.StartedAt.Equal(l.StartedAt) || r.Lease.Intent != l.Intent || !reflect.DeepEqual(r.Lease.Resources, []string{"r"}) {
		t.Errorf("renewed lease = %+v", r.Lease)
	}
	e.Clock.add(time.Minute)
	intent, res := "other", []string{"a", "b", "a"}
	r, err = e.S.Renew(ctx, me, "key:c", l.ID, leases.Renew{TTL: secs(10), Intent: &intent, Resources: &res})
	if err != nil {
		t.Fatal(err)
	}
	if r.Lease.TTLSeconds != 60 || r.Lease.Intent != "other" || !reflect.DeepEqual(r.Lease.Resources, []string{"a", "b"}) ||
		!r.Lease.ExpiresAt.Equal(e.Clock.now.Add(time.Minute)) {
		t.Errorf("renewed lease = %+v", r.Lease)
	}
	evs := e.events(t, leases.EventRenewed)
	if len(evs) != 2 || evs[1].Data["intent"] != "other" || evs[1].Data["ttl_seconds"] != float64(60) {
		t.Errorf("events = %+v", evs)
	}
}

func TestRenewReturnsConflictsAndRefusesInvalidInput(t *testing.T) {
	e := newEnv(t)
	e.start(t, bob, "aa-2", nil, "shared")
	l := e.start(t, me, "aa-1", nil).Lease
	res := []string{"shared"}
	r, err := e.S.Renew(ctx, me, "key:c", l.Ref, leases.Renew{Resources: &res})
	if err != nil || len(r.Conflicts) != 1 || r.Conflicts[0].Holder != "codex@host" {
		t.Fatalf("renew conflicts = %+v, %v", r.Conflicts, err)
	}
	bad := ""
	if _, err := e.S.Renew(ctx, me, "key:c", l.Ref, leases.Renew{Intent: &bad}); !errors.Is(err, leases.ErrInvalid) {
		t.Errorf("empty intent: %v", err)
	}
	empty := []string{""}
	if _, err := e.S.Renew(ctx, me, "key:c", l.Ref, leases.Renew{Resources: &empty}); !errors.Is(err, leases.ErrInvalid) {
		t.Errorf("empty resource: %v", err)
	}
	for _, ref := range []string{"L99", "nope", ""} {
		if _, err := e.S.Renew(ctx, me, "key:c", ref, leases.Renew{}); !errors.Is(err, leases.ErrNotFound) {
			t.Errorf("ref %q: %v", ref, err)
		}
	}
}

func TestRenewOfAnExpiredLeaseIsGoneCarryingTheExpiryTime(t *testing.T) {
	e := newEnv(t)
	l := e.start(t, me, "aa-1", secs(60)).Lease
	e.Clock.add(61 * time.Second)
	for i := 0; i < 2; i++ { // a refused renewal never marks it, so both attempts see the same thing
		_, err := e.S.Renew(ctx, me, "key:c", l.Ref, leases.Renew{})
		var gone leases.GoneError
		if !errors.Is(err, leases.ErrGone) || !errors.As(err, &gone) || !gone.ExpiredAt.Equal(l.ExpiresAt) || gone.Ref != "L1" {
			t.Fatalf("attempt %d: err = %v", i, err)
		}
	}
	if got, _ := e.S.Get(ctx, l.Ref); got.State != "live" || !got.ExpiresAt.Equal(l.ExpiresAt) {
		t.Errorf("lease after a refused renewal = %+v", got)
	}
	if n := len(e.events(t, leases.EventRenewed)) + len(e.events(t, leases.EventExpired)); n != 0 {
		t.Errorf("lease_renewed and lease_expired events = %d", n)
	}
	if got, _ := e.S.Current(ctx, l.Ref); got.State != "expired" || len(e.events(t, leases.EventExpired)) != 1 {
		t.Errorf("lease after a read = %+v", got)
	}
}

func TestRenewAtTheExpiryInstantIsStillLive(t *testing.T) {
	e := newEnv(t)
	l := e.start(t, me, "aa-1", secs(60)).Lease
	e.Clock.now = l.ExpiresAt
	if _, err := e.S.Renew(ctx, me, "key:c", l.Ref, leases.Renew{}); err != nil {
		t.Fatalf("renew at expires_at: %v", err)
	}
}

func TestRenewOfAnEndedLeaseIsAConflict(t *testing.T) {
	e := newEnv(t)
	l := e.start(t, me, "aa-1", nil).Lease
	if _, err := e.S.End(ctx, me, "key:c", l.Ref, ""); err != nil {
		t.Fatal(err)
	}
	_, err := e.S.Renew(ctx, me, "key:c", l.Ref, leases.Renew{})
	if !errors.Is(err, leases.ErrConflict) || errors.Is(err, leases.ErrGone) || !strings.Contains(err.Error(), "ended") {
		t.Fatalf("err = %v", err)
	}
}

func TestEndByTheHolderSetsNoEndedBy(t *testing.T) {
	e := newEnv(t)
	l := e.start(t, me, "aa-1", nil).Lease
	e.Clock.add(time.Minute)
	got, err := e.S.End(ctx, me, "key:c", l.Ref, "  all done ")
	if err != nil {
		t.Fatal(err)
	}
	if got.State != "ended" || got.EndedBy != "" || got.Outcome != "all done" || got.EndedAt == nil || !got.EndedAt.Equal(e.Clock.now) {
		t.Errorf("ended lease = %+v", got)
	}
	evs := e.events(t, leases.EventEnded)
	if len(evs) != 1 || evs[0].Data["outcome"] != "all done" || evs[0].Data["ref"] != "L1" || evs[0].Actor != "claude@host" {
		t.Fatalf("events = %+v", evs)
	}
	if _, has := evs[0].Data["ended_by"]; has {
		t.Errorf("a holder's end carries ended_by: %v", evs[0].Data)
	}
}

func TestEndByAnotherActorSetsEndedBy(t *testing.T) {
	e := newEnv(t)
	l := e.start(t, me, "aa-1", nil).Lease
	got, err := e.S.End(ctx, bob, "key:b", l.Ref, "")
	if err != nil {
		t.Fatal(err)
	}
	if got.State != "ended" || got.EndedBy != "codex@host" || got.Actor != "claude@host" || got.Outcome != "" {
		t.Errorf("ended lease = %+v", got)
	}
	evs := e.events(t, leases.EventEnded)
	if len(evs) != 1 || evs[0].Data["ended_by"] != "codex@host" || evs[0].Actor != "codex@host" {
		t.Errorf("events = %+v", evs)
	}
	if _, has := evs[0].Data["outcome"]; has {
		t.Errorf("an empty outcome was recorded: %v", evs[0].Data)
	}
}

func TestEndTwiceAndEndOfAnExpiredLeaseAreConflicts(t *testing.T) {
	e := newEnv(t)
	l := e.start(t, me, "aa-1", nil).Lease
	if _, err := e.S.End(ctx, me, "key:c", l.Ref, ""); err != nil {
		t.Fatal(err)
	}
	if _, err := e.S.End(ctx, me, "key:c", l.Ref, ""); !errors.Is(err, leases.ErrConflict) || !strings.Contains(err.Error(), "ended") {
		t.Errorf("second end: %v", err)
	}
	short := e.start(t, me, "aa-2", secs(60)).Lease
	e.Clock.add(2 * time.Minute)
	if _, err := e.S.End(ctx, me, "key:c", short.Ref, ""); !errors.Is(err, leases.ErrConflict) || !strings.Contains(err.Error(), "expired") {
		t.Errorf("end of expired: %v", err)
	}
	if n := len(e.events(t, leases.EventEnded)); n != 1 {
		t.Errorf("lease_ended events = %d", n)
	}
	if _, err := e.S.End(ctx, me, "key:c", "L77", ""); !errors.Is(err, leases.ErrNotFound) {
		t.Errorf("unknown: %v", err)
	}
	if _, err := e.S.End(ctx, me, "key:c", "L1", strings.Repeat("x", 201)); !errors.Is(err, leases.ErrInvalid) {
		t.Errorf("long outcome: %v", err)
	}
}

func TestAckOfAnExpiredLeaseSetsAckedAtAndAckedBy(t *testing.T) {
	e := newEnv(t)
	l := e.start(t, me, "aa-1", secs(60)).Lease
	e.Clock.add(2 * time.Minute)
	op := audit.Actor{Handle: "op@host", Operator: true}
	got, err := e.S.Ack(ctx, op, "oidc:op@example.com", l.Ref)
	if err != nil {
		t.Fatal(err)
	}
	if got.State != "expired" || got.AckedBy != "op@host" || got.AckedAt == nil || !got.AckedAt.Equal(e.Clock.now) {
		t.Errorf("acked lease = %+v", got)
	}
	evs := e.events(t, leases.EventAcked)
	if len(evs) != 1 || evs[0].Data["ref"] != "L1" || evs[0].RefID != l.ID {
		t.Errorf("events = %+v", evs)
	}
	e.Clock.add(time.Minute)
	again, err := e.S.Ack(ctx, bob, "key:b", l.Ref)
	if err != nil || again.AckedBy != "op@host" || !again.AckedAt.Equal(got.AckedAt.UTC()) || len(e.events(t, leases.EventAcked)) != 1 {
		t.Errorf("second ack = %+v, %v", again, err)
	}
}

func TestAckOfALiveOrEndedLeaseIsAConflict(t *testing.T) {
	e := newEnv(t)
	live := e.start(t, me, "aa-1", nil).Lease
	if _, err := e.S.Ack(ctx, me, "key:c", live.Ref); !errors.Is(err, leases.ErrConflict) || !strings.Contains(err.Error(), "live") {
		t.Errorf("ack live: %v", err)
	}
	done := e.start(t, me, "aa-2", nil).Lease
	if _, err := e.S.End(ctx, me, "key:c", done.Ref, ""); err != nil {
		t.Fatal(err)
	}
	if _, err := e.S.Ack(ctx, me, "key:c", done.Ref); !errors.Is(err, leases.ErrConflict) || !strings.Contains(err.Error(), "ended") {
		t.Errorf("ack ended: %v", err)
	}
	if got, _ := e.S.Get(ctx, live.Ref); got.AckedAt != nil || got.AckedBy != "" {
		t.Errorf("a refused ack wrote: %+v", got)
	}
	if _, err := e.S.Ack(ctx, me, "key:c", "L99"); !errors.Is(err, leases.ErrNotFound) {
		t.Errorf("unknown: %v", err)
	}
	if n := len(e.events(t, leases.EventAcked)); n != 0 {
		t.Errorf("lease_acked events = %d", n)
	}
}

func TestExpireDueMarksLeasesPastExpiryOnceAndWritesOneEventEach(t *testing.T) {
	e := newEnv(t)
	a := e.start(t, me, "aa-1", secs(60)).Lease
	b := e.start(t, bob, "aa-1", secs(600)).Lease
	if n, err := e.S.ExpireDue(ctx); err != nil || n != 0 {
		t.Fatalf("nothing due: %d, %v", n, err)
	}
	e.Clock.add(2 * time.Minute)
	if n, err := e.S.ExpireDue(ctx); err != nil || n != 1 {
		t.Fatalf("one due: %d, %v", n, err)
	}
	if n, err := e.S.ExpireDue(ctx); err != nil || n != 0 {
		t.Fatalf("second sweep: %d, %v", n, err)
	}
	got, _ := e.S.Get(ctx, a.Ref)
	other, _ := e.S.Get(ctx, b.Ref)
	if got.State != "expired" || other.State != "live" {
		t.Errorf("states = %s, %s", got.State, other.State)
	}
	evs := e.events(t, leases.EventExpired)
	if len(evs) != 1 || evs[0].RefID != a.ID || evs[0].Data["ref"] != "L1" || evs[0].Principal != "system:expiry" ||
		evs[0].Data["expired_at"] != a.ExpiresAt.Format(time.RFC3339Nano) || evs[0].TaskID != a.TaskID {
		t.Errorf("events = %+v", evs)
	}
}

func TestClosingATaskEndsItsLiveLeasesAndTheyCanNoLongerBeRenewedOrEnded(t *testing.T) {
	e := newEnv(t)
	l := e.start(t, me, "aa-1", nil).Lease
	b := e.start(t, bob, "aa-1", nil).Lease
	other := e.start(t, bob, "aa-2", nil).Lease
	if _, err := e.Tasks.Close(ctx, me, "key:x", "aa-1", "dropped"); err != nil {
		t.Fatal(err)
	}
	got, _ := e.S.Get(ctx, l.Ref)
	theirs, _ := e.S.Get(ctx, b.Ref)
	if got.State != "ended" || got.Outcome != "ended: task closed by claude@host" || got.EndedBy != "claude@host" || got.EndedAt == nil {
		t.Errorf("own lease = %+v", got)
	}
	if theirs.State != "ended" || theirs.Outcome != "ended: task closed by claude@host" || theirs.EndedBy != "claude@host" {
		t.Errorf("other lease = %+v", theirs)
	}
	if left, _ := e.S.Get(ctx, other.Ref); left.State != "live" {
		t.Errorf("a lease on another task = %+v, want live", left)
	}
	if _, err := e.S.Renew(ctx, me, "key:c", l.Ref, leases.Renew{}); !errors.Is(err, leases.ErrConflict) {
		t.Errorf("renew on a closed task: %v", err)
	}
	if _, err := e.S.End(ctx, me, "key:c", l.Ref, ""); !errors.Is(err, leases.ErrConflict) {
		t.Errorf("end on a closed task: %v", err)
	}
	evs := e.events(t, leases.EventEnded)
	if len(evs) != 2 || evs[0].Data["ended_by"] != "claude@host" || evs[1].Data["ended_by"] != "claude@host" || evs[1].RefID != b.ID {
		t.Errorf("events = %+v", evs)
	}
}

func TestDoneEndsTheCallersLeaseAndRefusesOrForcesPastAnothersLease(t *testing.T) {
	e := newEnv(t)
	mine := e.start(t, me, "aa-1", nil).Lease
	theirs := e.start(t, bob, "aa-1", nil).Lease
	_, err := e.Tasks.Done(ctx, me, "key:x", "aa-1", tasks.DoneOptions{})
	var held tasks.LeaseHeldError
	if !errors.As(err, &held) || len(held.Holders) != 1 || held.Holders[0].Ref != theirs.Ref {
		t.Fatalf("err = %v, want the done refused naming %s", err, theirs.Ref)
	}
	if got, _ := e.S.Get(ctx, mine.Ref); got.State != "live" || len(e.events(t, leases.EventEnded)) != 0 {
		t.Errorf("a refused done changed the caller's lease: %+v", got)
	}
	if _, err := e.Tasks.Done(ctx, me, "key:x", "aa-1", tasks.DoneOptions{Outcome: "shipped", Force: true}); err != nil {
		t.Fatal(err)
	}
	a, _ := e.S.Get(ctx, mine.Ref)
	b, _ := e.S.Get(ctx, theirs.Ref)
	if a.State != "ended" || a.Outcome != "ended: task marked done by claude@host" || a.EndedBy != "claude@host" {
		t.Errorf("own lease = %+v", a)
	}
	if b.State != "ended" || b.Outcome != "ended: task marked done by claude@host" || b.EndedBy != "claude@host" {
		t.Errorf("other lease = %+v", b)
	}
}

func TestDoneByTheHolderEndsItsLeaseWithTheDefaultOutcome(t *testing.T) {
	e := newEnv(t)
	l := e.start(t, me, "aa-1", nil).Lease
	if _, err := e.Tasks.Done(ctx, me, "key:x", "aa-1", tasks.DoneOptions{}); err != nil {
		t.Fatal(err)
	}
	if got, _ := e.S.Get(ctx, l.Ref); got.State != "ended" || got.Outcome != "done" {
		t.Errorf("lease = %+v", got)
	}
}

func TestDoneOnATaskWhoseOtherLeaseRanOutExpiresItInsteadOfRefusing(t *testing.T) {
	e := newEnv(t)
	l := e.start(t, bob, "aa-1", secs(60)).Lease
	e.Clock.add(2 * time.Minute)
	if _, err := e.Tasks.Done(ctx, me, "key:x", "aa-1", tasks.DoneOptions{}); err != nil {
		t.Fatal(err)
	}
	if got, _ := e.S.Get(ctx, l.Ref); got.State != "expired" || len(e.events(t, leases.EventExpired)) != 1 || len(e.events(t, leases.EventEnded)) != 0 {
		t.Errorf("lease = %+v", got)
	}
}

func TestStartOnADoneTaskIsRefusedAndWritesNothing(t *testing.T) {
	e := newEnv(t)
	if _, err := e.Tasks.Done(ctx, me, "key:x", "aa-1", tasks.DoneOptions{}); err != nil {
		t.Fatal(err)
	}
	_, err := e.S.Start(ctx, bob, "key:b", leases.Start{Task: "aa-1", Intent: "x"})
	var ce leases.ConflictError
	if !errors.Is(err, leases.ErrConflict) || !errors.As(err, &ce) || !strings.Contains(ce.Msg, "aa-1") || !strings.Contains(ce.Msg, "done") ||
		ce.Next != "Run vh task reopen aa-1 first, or start the lease on an open task." {
		t.Fatalf("err = %v, next %q", err, ce.Next)
	}
	var n int
	if err := e.S.Store.Reader().QueryRow(`SELECT COUNT(*) FROM leases`).Scan(&n); err != nil || n != 0 {
		t.Errorf("leases = %d, %v", n, err)
	}
	if err := e.S.Store.Reader().QueryRow(`SELECT COUNT(*) FROM actors WHERE handle = 'codex@host'`).Scan(&n); err != nil || n != 0 {
		t.Errorf("a refused start recorded the actor: %d", n)
	}
}

// liveLeasesOnSettledTasks counts the live leases whose task is done or closed:
// the invariant a lease write and a done or close must never leave broken.
func (e env) liveLeasesOnSettledTasks(t *testing.T) int {
	t.Helper()
	var n int
	if err := e.S.Store.Reader().QueryRow(`SELECT COUNT(*) FROM leases l JOIN tasks t ON t.id = l.task_id
		WHERE l.state = 'live' AND t.status IN ('done', 'closed')`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

func TestRenewAfterTheTaskIsDoneIsRefusedAndLeavesNoLiveLease(t *testing.T) {
	e := newEnv(t)
	l := e.start(t, me, "aa-1", nil).Lease
	if _, err := e.Tasks.Done(ctx, me, "key:x", "aa-1", tasks.DoneOptions{}); err != nil {
		t.Fatal(err)
	}
	e.Clock.add(time.Second)
	if _, err := e.S.Renew(ctx, me, "key:r", l.Ref, leases.Renew{}); !errors.Is(err, leases.ErrConflict) {
		t.Errorf("renew after done: %v, want a conflict", err)
	}
	if got, _ := e.S.Get(ctx, l.Ref); got.State != "ended" {
		t.Errorf("lease = %+v, want ended", got)
	}
	if n := e.liveLeasesOnSettledTasks(t); n != 0 {
		t.Errorf("%d live leases on a done task", n)
	}
}

func TestEveryOrderOfStartDoneAndCloseLeavesNoLiveLeaseOnASettledTask(t *testing.T) {
	e := newEnv(t)
	// aa-1: another actor's lease, forced done, then a start by each actor
	e.start(t, bob, "aa-1", nil)
	if _, err := e.Tasks.Done(ctx, me, "key:x", "aa-1", tasks.DoneOptions{Force: true}); err != nil {
		t.Fatal(err)
	}
	for _, a := range []audit.Actor{me, bob} {
		if _, err := e.S.Start(ctx, a, "key:"+a.Handle, leases.Start{Task: "aa-1", Intent: "again"}); !errors.Is(err, leases.ErrConflict) {
			t.Errorf("%s start on a done task: %v, want a conflict", a.Handle, err)
		}
	}
	// aa-2: the same two starts after a close
	e.start(t, me, "aa-2", nil)
	if _, err := e.Tasks.Close(ctx, me, "key:y", "aa-2", "dropped"); err != nil {
		t.Fatal(err)
	}
	for _, a := range []audit.Actor{me, bob} {
		if _, err := e.S.Start(ctx, a, "key:z"+a.Handle, leases.Start{Task: "aa-2", Intent: "again"}); !errors.Is(err, leases.ErrConflict) {
			t.Errorf("%s start on a closed task: %v, want a conflict", a.Handle, err)
		}
	}
	if n := e.liveLeasesOnSettledTasks(t); n != 0 {
		t.Errorf("%d live leases on done or closed tasks", n)
	}
}

func TestReopeningADoneTaskLetsALeaseStartAgain(t *testing.T) {
	e := newEnv(t)
	if _, err := e.Tasks.Done(ctx, me, "key:x", "aa-1", tasks.DoneOptions{}); err != nil {
		t.Fatal(err)
	}
	if _, err := e.Tasks.Reopen(ctx, me, "key:y", "aa-1"); err != nil {
		t.Fatal(err)
	}
	l := e.start(t, bob, "aa-1", nil).Lease
	if l.State != "live" || e.liveLeasesOnSettledTasks(t) != 0 {
		t.Errorf("lease = %+v", l)
	}
}

func TestGetReadsByRefAndUlidInEitherCase(t *testing.T) {
	e := newEnv(t)
	l := e.start(t, me, "aa-1", nil).Lease
	for _, ref := range []string{"L1", "l1", l.ID, strings.ToLower(l.ID)} {
		if got, err := e.S.Get(ctx, ref); err != nil || got.ID != l.ID {
			t.Errorf("Get(%q) = %v, %v", ref, got.Ref, err)
		}
	}
	for _, ref := range []string{"L2", "L0", "x", "", "01ARZ3NDEKTSV4RRFFQ69G5FAV"} {
		if _, err := e.S.Get(ctx, ref); !errors.Is(err, leases.ErrNotFound) {
			t.Errorf("Get(%q) = %v", ref, err)
		}
	}
}

func TestSameActorOnTwoTasksSharingAResourceConflicts(t *testing.T) {
	e := newEnv(t)
	e.start(t, me, "aa-1", nil, "shared")
	r := e.start(t, me, "aa-2", nil, "shared")
	if len(r.Conflicts) != 1 || r.Conflicts[0].Holder != "claude@host" || r.Conflicts[0].Task != "aa-1" {
		t.Errorf("conflicts = %+v", r.Conflicts)
	}
	_ = domain.LeaseLive
}

func TestErrorTextsNameTheLeaseAndTheProblem(t *testing.T) {
	g := leases.GoneError{Ref: "L3", ExpiredAt: t0}
	if got := g.Error(); got != "lease L3 expired at 2026-11-05T12:00:00Z" {
		t.Errorf("GoneError = %q", got)
	}
	if got := (leases.InvalidError{Msg: "bad", Next: "fix"}).Error(); got != "bad" {
		t.Errorf("InvalidError = %q", got)
	}
}

// snapshot is every lease, actor and event row a refused write must leave alone.
func (e env) snapshot(t *testing.T) string {
	t.Helper()
	var out strings.Builder
	for _, q := range []string{
		`SELECT n || '|' || state || '|' || expires_at || '|' || renewed_at || '|' || COALESCE(ended_at, '') || '|' || COALESCE(acked_at, '') FROM leases ORDER BY n`,
		`SELECT id || '|' || handle FROM actors ORDER BY id`,
		`SELECT n || '|' || type || '|' || summary FROM events ORDER BY n`,
	} {
		rows, err := e.S.Store.Reader().QueryContext(ctx, q)
		if err != nil {
			t.Fatal(err)
		}
		for rows.Next() {
			var line string
			if err := rows.Scan(&line); err != nil {
				t.Fatal(err)
			}
			out.WriteString(line + "\n")
		}
		_ = rows.Close()
	}
	return out.String()
}

func TestRefusedWritesChangeNothingWhileAnUnrelatedLeaseIsDue(t *testing.T) {
	e := newEnv(t)
	due := e.start(t, me, "aa-1", secs(60)).Lease
	ended := e.start(t, me, "aa-2", nil).Lease
	if _, err := e.S.End(ctx, me, "key:c", ended.Ref, ""); err != nil {
		t.Fatal(err)
	}
	live := e.start(t, bob, "aa-2", nil).Lease
	e.Clock.add(2 * time.Minute)
	if _, err := e.Tasks.Close(ctx, me, "key:x", "aa-2", "dropped"); err != nil {
		t.Fatal(err)
	}
	stranger := audit.Actor{Handle: "new@host"}
	refusals := map[string]func() error{
		"start closed": func() error {
			_, err := e.S.Start(ctx, stranger, "key:n", leases.Start{Task: "aa-2", Intent: "x"})
			return err
		},
		"start unknown": func() error {
			_, err := e.S.Start(ctx, stranger, "key:n", leases.Start{Task: "aa-9", Intent: "x"})
			return err
		},
		"renew ended":   func() error { _, err := e.S.Renew(ctx, stranger, "key:n", ended.Ref, leases.Renew{}); return err },
		"renew unknown": func() error { _, err := e.S.Renew(ctx, stranger, "key:n", "L99", leases.Renew{}); return err },
		"renew the due": func() error { _, err := e.S.Renew(ctx, stranger, "key:n", due.Ref, leases.Renew{}); return err },
		"end ended":     func() error { _, err := e.S.End(ctx, stranger, "key:n", ended.Ref, ""); return err },
		"end the due":   func() error { _, err := e.S.End(ctx, stranger, "key:n", due.Ref, ""); return err },
		"end unknown":   func() error { _, err := e.S.End(ctx, stranger, "key:n", "L99", ""); return err },
		"ack ended":     func() error { _, err := e.S.Ack(ctx, stranger, "key:n", ended.Ref); return err },
		"ack unknown":   func() error { _, err := e.S.Ack(ctx, stranger, "key:n", "L99"); return err },
		"ack live":      func() error { _, err := e.S.Ack(ctx, stranger, "key:n", live.Ref); return err },
		"end by outcome": func() error {
			_, err := e.S.End(ctx, stranger, "key:n", live.Ref, strings.Repeat("x", 201))
			return err
		},
	}
	before := e.snapshot(t)
	for name, refuse := range refusals {
		if err := refuse(); err == nil {
			t.Errorf("%s: want a refusal", name)
		}
		if after := e.snapshot(t); after != before {
			t.Errorf("%s changed the database:\n%s\nwas:\n%s", name, after, before)
		}
	}
}

func TestEverySuccessfulLeaseWriteCommitsTheExpiryOfADueLeaseOnce(t *testing.T) {
	writes := map[string]func(e env, held leases.Lease) error{
		"start": func(e env, _ leases.Lease) error {
			_, err := e.S.Start(ctx, bob, "key:b", leases.Start{Task: "aa-2", Intent: "x"})
			return err
		},
		"renew": func(e env, held leases.Lease) error {
			_, err := e.S.Renew(ctx, me, "key:c", held.Ref, leases.Renew{})
			return err
		},
		"end": func(e env, held leases.Lease) error { _, err := e.S.End(ctx, me, "key:c", held.Ref, ""); return err },
	}
	for name, write := range writes {
		e := newEnv(t)
		due := e.start(t, me, "aa-1", secs(60)).Lease
		held := e.start(t, me, "aa-2", nil).Lease
		e.Clock.add(2 * time.Minute)
		if err := write(e, held); err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if got, _ := e.S.Get(ctx, due.Ref); got.State != "expired" {
			t.Errorf("%s: due lease is %s", name, got.State)
		}
		if evs := e.events(t, leases.EventExpired); len(evs) != 1 || evs[0].RefID != due.ID {
			t.Errorf("%s: lease_expired events = %+v", name, evs)
		}
	}
}

func notifications(t *testing.T, e env) (kinds []string) {
	t.Helper()
	rs, err := e.S.Store.Reader().QueryContext(ctx, `SELECT n.kind FROM notifications n JOIN events ev ON ev.id = n.event_id ORDER BY ev.n`)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = rs.Close() }()
	for rs.Next() {
		var k string
		if err := rs.Scan(&k); err != nil {
			t.Fatal(err)
		}
		kinds = append(kinds, k)
	}
	return kinds
}

func TestExpiryEnqueuesAStrandedNotificationOnlyForTheNewestLeaseOfATask(t *testing.T) {
	e := newEnv(t)
	e.S.Store.SetNotify(store.NotifyKinds)
	e.start(t, me, "aa-1", secs(60))
	e.start(t, bob, "aa-1", secs(60))
	e.start(t, me, "aa-2", secs(60))
	e.Clock.add(2 * time.Minute)
	if n, err := e.S.ExpireDue(ctx); err != nil || n != 3 {
		t.Fatalf("ExpireDue = %d, %v", n, err)
	}
	if got := notifications(t, e); !reflect.DeepEqual(got, []string{"stranded", "stranded"}) {
		t.Fatalf("notifications = %q, want one for aa-1's newest lease and one for aa-2's", got)
	}
}

func TestExpiryEnqueuesNothingWhenAlertsAreOff(t *testing.T) {
	e := newEnv(t)
	e.start(t, me, "aa-1", secs(60))
	e.Clock.add(2 * time.Minute)
	if _, err := e.S.ExpireDue(ctx); err != nil {
		t.Fatal(err)
	}
	if got := notifications(t, e); len(got) != 0 {
		t.Fatalf("notifications = %q", got)
	}
}

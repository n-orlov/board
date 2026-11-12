package notify_test

import (
	"errors"
	"reflect"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/n-orlov/board/internal/domain"
	"github.com/n-orlov/board/internal/notify"
	"github.com/n-orlov/board/internal/store"
)

type seeded struct {
	id, kind, state, lastErr string
	created                  time.Time
	attempts                 int
}

// seedOutbox writes five rows: p1 (pending, +1s), s2 (sent, +2s), f3 (failed, +3s), p4 (pending, +3s, the same
// instant as f3) and s5 (sent, +4s), each on its own event, and returns them oldest first.
func seedOutbox(t *testing.T, e env) []seeded {
	t.Helper()
	rows := []seeded{
		{kind: "stranded", state: "pending", created: t0.Add(1 * time.Second)},
		{kind: "asking", state: "sent", attempts: 1, created: t0.Add(2 * time.Second)},
		{kind: "done", state: "failed", attempts: 6, lastErr: "http 500", created: t0.Add(3 * time.Second)},
		{kind: "asking", state: "pending", attempts: 2, lastErr: "timeout", created: t0.Add(3 * time.Second)},
		{kind: "done", state: "sent", attempts: 3, lastErr: "network error", created: t0.Add(4 * time.Second)},
	}
	for i := range rows {
		id, err := domain.NewULID(rows[i].created)
		if err != nil {
			t.Fatal(err)
		}
		rows[i].id = id
		at := store.EncodeTime(rows[i].created)
		var sent, lastErr any
		if rows[i].state == "sent" {
			sent = at + 500
		}
		if rows[i].lastErr != "" {
			lastErr = rows[i].lastErr
		}
		_, err = e.st.Writer().ExecContext(ctx, e.st.Dialect().Rebind(`INSERT INTO notifications
			(id, kind, event_id, payload, state, attempts, next_attempt_at, last_error, created_at, sent_at)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`),
			id, rows[i].kind, e.event(t, "task_created", "aa-1", ""), `{"kind":"`+rows[i].kind+`","text":"row `+id+`"}`,
			rows[i].state, rows[i].attempts, at+60000, lastErr, at, sent)
		if err != nil {
			t.Fatal(err)
		}
	}
	return rows
}

// newestFirst is the ids of rows in the order a list answers: newest by creation time, ties by id, both descending.
func newestFirst(rows []seeded, keep func(seeded) bool) []string {
	var kept []seeded
	for _, r := range rows {
		if keep(r) {
			kept = append(kept, r)
		}
	}
	sort.Slice(kept, func(i, j int) bool {
		if !kept[i].created.Equal(kept[j].created) {
			return kept[i].created.After(kept[j].created)
		}
		return kept[i].id > kept[j].id
	})
	ids := []string{}
	for _, r := range kept {
		ids = append(ids, r.id)
	}
	return ids
}

func idsOf(p notify.Page) []string {
	ids := []string{}
	for _, n := range p.Items {
		ids = append(ids, n.ID)
	}
	return ids
}

func TestListIsNewestFirstAndFiltersByState(t *testing.T) {
	e := newEnv(t, nil)
	rows := seedOutbox(t, e)
	s := notify.Store{Store: e.st}
	for _, state := range []string{"", "pending", "sent", "failed"} {
		var q notify.Query
		if state != "" {
			q.State = &state
		}
		p, err := s.List(ctx, q)
		want := newestFirst(rows, func(r seeded) bool { return state == "" || r.state == state })
		if err != nil || !reflect.DeepEqual(idsOf(p), want) || p.NextAfter != "" {
			t.Errorf("state %q = %v (next %q, %v), want %v", state, idsOf(p), p.NextAfter, err, want)
		}
	}
	// a state with no rows is an empty list, not null
	p, err := (notify.Store{Store: newEnv(t, nil).st}).List(ctx, notify.Query{State: ptr("failed")})
	if err != nil || p.Items == nil || len(p.Items) != 0 {
		t.Errorf("empty outbox = %#v, %v", p, err)
	}
}

func TestListCarriesTheFieldsOfEachRow(t *testing.T) {
	e := newEnv(t, nil)
	rows := seedOutbox(t, e)
	p, err := (notify.Store{Store: e.st}).List(ctx, notify.Query{Limit: 1000})
	if err != nil {
		t.Fatal(err)
	}
	byID := map[string]notify.Notification{}
	for _, n := range p.Items {
		byID[n.ID] = n
	}
	pending := byID[rows[3].id]
	if pending.Kind != "asking" || pending.State != "pending" || pending.Attempts != 2 || pending.LastError != "timeout" ||
		!pending.NextAttemptAt.Equal(rows[3].created.Add(time.Minute)) || !pending.SentAt.IsZero() ||
		!pending.CreatedAt.Equal(rows[3].created) || !strings.HasPrefix(pending.Event, "E") ||
		pending.Payload["kind"] != "asking" || pending.Payload["text"] != "row "+rows[3].id {
		t.Errorf("pending row = %+v", pending)
	}
	sent := byID[rows[1].id]
	if sent.State != "sent" || !sent.NextAttemptAt.IsZero() || !sent.SentAt.Equal(rows[1].created.Add(500*time.Millisecond)) || sent.LastError != "" {
		t.Errorf("sent row = %+v", sent)
	}
	failed := byID[rows[2].id]
	if failed.State != "failed" || !failed.NextAttemptAt.IsZero() || !failed.SentAt.IsZero() || failed.LastError != "http 500" || failed.Attempts != 6 {
		t.Errorf("failed row = %+v", failed)
	}
	refs := map[string]bool{}
	for _, n := range p.Items {
		refs[n.Event] = true
	}
	if len(refs) != 5 {
		t.Errorf("each row names its own event: %v", refs)
	}
}

func TestListPagesWithLimitAndAfter(t *testing.T) {
	e := newEnv(t, nil)
	rows := seedOutbox(t, e)
	s := notify.Store{Store: e.st}
	all := newestFirst(rows, func(seeded) bool { return true })
	var got []string
	after := ""
	for page := 0; page < 3; page++ {
		p, err := s.List(ctx, notify.Query{Limit: 2, After: after})
		if err != nil {
			t.Fatal(err)
		}
		got = append(got, idsOf(p)...)
		wantNext := page < 2
		if (p.NextAfter != "") != wantNext || (wantNext && p.NextAfter != p.Items[1].ID) {
			t.Fatalf("page %d next_after = %q", page, p.NextAfter)
		}
		after = p.NextAfter
	}
	if !reflect.DeepEqual(got, all) {
		t.Errorf("pages = %v, want %v", got, all)
	}
	// a limit equal to the number left is the last page
	p, err := s.List(ctx, notify.Query{Limit: 3, After: all[1]})
	if err != nil || !reflect.DeepEqual(idsOf(p), all[2:]) || p.NextAfter != "" {
		t.Errorf("last page = %v next %q, %v", idsOf(p), p.NextAfter, err)
	}
	// paging keeps the state filter, and a lowercase cursor reads as its ULID
	pend := newestFirst(rows, func(r seeded) bool { return r.state == "pending" })
	p, err = s.List(ctx, notify.Query{State: ptr("pending"), After: strings.ToLower(pend[0])})
	if err != nil || !reflect.DeepEqual(idsOf(p), pend[1:]) {
		t.Errorf("pending after the first = %v, %v", idsOf(p), err)
	}
}

func TestListRefusesWhatItCannotRead(t *testing.T) {
	e := newEnv(t, nil)
	rows := seedOutbox(t, e)
	s := notify.Store{Store: e.st}
	unknown, _ := domain.NewULID(t0)
	for name, q := range map[string]notify.Query{
		"unknown state": {State: ptr("queued")}, "state case": {State: ptr("Sent")}, "empty state": {State: ptr("")}, "limit above": {Limit: 1001}, "limit below": {Limit: -1},
		"after not a ULID": {After: "N1"}, "after unknown": {After: unknown},
	} {
		_, err := s.List(ctx, q)
		var bad notify.InvalidError
		if !errors.Is(err, notify.ErrInvalid) || !errors.As(err, &bad) || bad.Msg == "" || bad.Next == "" || bad.Error() != bad.Msg {
			t.Errorf("%s: err = %v", name, err)
		}
	}
	if _, err := s.List(ctx, notify.Query{After: rows[0].id, Limit: 1000}); err != nil {
		t.Errorf("a known cursor: %v", err)
	}
}

func TestListFailsOnAReadErrorAndOnAPayloadThatIsNotJSON(t *testing.T) {
	e := newEnv(t, nil)
	rows := seedOutbox(t, e)
	s := notify.Store{Store: e.st}
	if _, err := e.st.Writer().ExecContext(ctx, `UPDATE notifications SET payload = 'not json' WHERE id = ?`, rows[0].id); err != nil {
		t.Fatal(err)
	}
	if _, err := s.List(ctx, notify.Query{}); err == nil || errors.Is(err, notify.ErrInvalid) {
		t.Errorf("a payload that is not JSON: %v", err)
	}
	if _, err := e.st.Writer().ExecContext(ctx, `DROP TABLE notifications`); err != nil {
		t.Fatal(err)
	}
	for _, q := range []notify.Query{{}, {After: rows[1].id}} {
		if _, err := s.List(ctx, q); err == nil || errors.Is(err, notify.ErrInvalid) {
			t.Errorf("a missing table (%+v): %v", q, err)
		}
	}
}

func ptr(s string) *string { return &s }

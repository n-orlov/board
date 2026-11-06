package initiatives

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/n-orlov/board/internal/audit"
	"github.com/n-orlov/board/internal/domain"
	"github.com/n-orlov/board/internal/store"
)

var t0 = time.Date(2026, 11, 5, 12, 0, 0, 0, time.UTC)

// newStore is an Initiatives store over a migrated SQLite database whose clock
// advances one second per call, so ids sort in creation order.
func newStore(t *testing.T) Store {
	t.Helper()
	st, err := store.Open("sqlite://" + filepath.Join(t.TempDir(), "b.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	if err := st.Migrate(context.Background()); err != nil {
		t.Fatal(err)
	}
	n := 0
	return Store{Store: st, Now: func() time.Time { n++; return t0.Add(time.Duration(n) * time.Second) }}
}

var who = audit.Actor{Handle: "claude@host"}

func mk(t *testing.T, s Store, slug string, tags ...string) Initiative {
	t.Helper()
	i, err := s.Create(context.Background(), who, "key:caller", New{Slug: slug, Title: "T " + slug, Tags: tags})
	if err != nil {
		t.Fatalf("create %s: %v", slug, err)
	}
	return i
}

func count(t *testing.T, s Store, table string) int {
	t.Helper()
	var n int
	if err := s.Store.Reader().QueryRowContext(context.Background(), "SELECT count(*) FROM "+table).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

func TestCreateStoresTheInitiativeItsTagsAndOneEvent(t *testing.T) {
	s := newStore(t)
	got, err := s.Create(context.Background(), who, "key:caller", New{
		Slug: "phone-onboard", Title: "  Phone  ", Description: "d", Tags: []string{"Work", "home"}, TargetStart: "2026-03-01", TargetEnd: "2026-04-01",
	})
	if err != nil {
		t.Fatal(err)
	}
	want := Initiative{
		ID: got.ID, Slug: "phone-onboard", Title: "Phone", Description: "d", Status: "open", Tags: []string{"home", "work"},
		TargetStart: "2026-03-01", TargetEnd: "2026-04-01", CreatedBy: "claude@host", CreatedAt: t0.Add(time.Second),
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("created = %+v\nwant      %+v", got, want)
	}
	var principal, typ, initiative, ref, data string
	err = s.Store.Reader().QueryRowContext(context.Background(),
		`SELECT principal, type, initiative_id, ref_id, data FROM events`).Scan(&principal, &typ, &initiative, &ref, &data)
	if err != nil || principal != "key:caller" || typ != EventCreated || initiative != got.ID || ref != got.ID {
		t.Fatalf("event %q %q %q %q (%v)", principal, typ, initiative, ref, err)
	}
	if data != `{"slug":"phone-onboard","tags":["home","work"],"title":"Phone"}` {
		t.Errorf("event data = %s", data)
	}
	byID, err := s.Get(context.Background(), got.ID)
	if err != nil || !reflect.DeepEqual(byID, got) {
		t.Errorf("Get by ULID = %+v, %v", byID, err)
	}
}

func TestCreateRefusalsLeaveNothingBehind(t *testing.T) {
	s := newStore(t)
	mk(t, s, "taken")
	ctx := context.Background()
	if _, err := s.Create(ctx, audit.Actor{Handle: "other"}, "key:x", New{Slug: "taken", Title: "t"}); !errors.Is(err, ErrExists) {
		t.Fatalf("duplicate = %v, want ErrExists", err)
	}
	for _, in := range []New{
		{Slug: "x", Title: "t"},
		{Slug: "okay", Title: ""},
		{Slug: "okay", Title: "t", Description: string(make([]byte, domain.MaxDescription+1))},
		{Slug: "okay", Title: "t", Tags: []string{"no way"}},
		{Slug: "okay", Title: "t", TargetStart: "2026-02-01", TargetEnd: "2026-01-01"},
	} {
		_, err := s.Create(ctx, audit.Actor{Handle: "other"}, "key:x", in)
		var bad InvalidError
		if !errors.Is(err, ErrInvalid) || !errors.As(err, &bad) || bad.Msg == "" || bad.Next == "" {
			t.Errorf("%+v: %v", in, err)
		}
	}
	if n := count(t, s, "initiatives"); n != 1 {
		t.Errorf("initiatives = %d, want 1", n)
	}
	if n := count(t, s, "events"); n != 1 {
		t.Errorf("events = %d, want 1", n)
	}
	if n := count(t, s, "actors"); n != 1 {
		t.Errorf("actors = %d, want 1 (a refused create upserts none)", n)
	}
	if n := count(t, s, "initiative_tags"); n != 0 {
		t.Errorf("tags = %d", n)
	}
}

func TestGetUnknownIsNotFound(t *testing.T) {
	s := newStore(t)
	for _, ref := range []string{"nope", "01HZZZZZZZZZZZZZZZZZZZZZZZ"} {
		if _, err := s.Get(context.Background(), ref); !errors.Is(err, ErrNotFound) {
			t.Errorf("Get(%q) = %v", ref, err)
		}
	}
}

func slugs(p Page) []string {
	out := []string{}
	for _, i := range p.Items {
		out = append(out, i.Slug)
	}
	return out
}

func TestListPaginationBoundary(t *testing.T) {
	s := newStore(t)
	ctx := context.Background()
	var all []Initiative
	for i := 1; i <= 5; i++ {
		all = append(all, mk(t, s, fmt.Sprintf("init-%d", i)))
	}
	// A limit equal to the number of rows is the last page: no next cursor.
	p, err := s.List(ctx, Query{Limit: 5})
	if err != nil || len(p.Items) != 5 || p.NextAfter != "" {
		t.Fatalf("limit = count: %v %q %v", slugs(p), p.NextAfter, err)
	}
	// One less leaves a next cursor naming the last item shown.
	p, err = s.List(ctx, Query{Limit: 4})
	if err != nil || len(p.Items) != 4 || p.NextAfter != all[3].ID {
		t.Fatalf("limit = count-1: %v %q %v", slugs(p), p.NextAfter, err)
	}
	p, err = s.List(ctx, Query{Limit: 4, After: domain.Cursor{Ref: p.NextAfter}})
	if err != nil || !reflect.DeepEqual(slugs(p), []string{"init-5"}) || p.NextAfter != "" {
		t.Fatalf("second page: %v %q %v", slugs(p), p.NextAfter, err)
	}
	// A slug cursor is the same as the id of that initiative.
	p, err = s.List(ctx, Query{Limit: 2, After: domain.Cursor{Ref: "init-2"}})
	if err != nil || !reflect.DeepEqual(slugs(p), []string{"init-3", "init-4"}) || p.NextAfter != all[3].ID {
		t.Fatalf("after slug: %v %q %v", slugs(p), p.NextAfter, err)
	}
	// After the last one there is nothing, and the default size applies.
	p, err = s.List(ctx, Query{After: domain.Cursor{Ref: "init-5"}})
	if err != nil || p.Items == nil || len(p.Items) != 0 || p.NextAfter != "" {
		t.Fatalf("after last: %#v %v", p, err)
	}
	if p, err = s.List(ctx, Query{}); err != nil || len(p.Items) != 5 {
		t.Fatalf("default: %v %v", slugs(p), err)
	}
}

func TestListFiltersByEveryTagAndReturnsEachInitiativesTags(t *testing.T) {
	s := newStore(t)
	ctx := context.Background()
	mk(t, s, "aa", "work", "x")
	mk(t, s, "bb", "work")
	mk(t, s, "cc")
	p, err := s.List(ctx, Query{Tags: []string{"Work"}})
	if err != nil || !reflect.DeepEqual(slugs(p), []string{"aa", "bb"}) {
		t.Fatalf("work: %v %v", slugs(p), err)
	}
	if !reflect.DeepEqual(p.Items[0].Tags, []string{"work", "x"}) || !reflect.DeepEqual(p.Items[1].Tags, []string{"work"}) {
		t.Errorf("tags = %v %v", p.Items[0].Tags, p.Items[1].Tags)
	}
	p, err = s.List(ctx, Query{Tags: []string{"work", "x", "work"}})
	if err != nil || !reflect.DeepEqual(slugs(p), []string{"aa"}) {
		t.Fatalf("work+x: %v %v", slugs(p), err)
	}
	if p, err = s.List(ctx, Query{Tags: []string{"absent"}}); err != nil || len(p.Items) != 0 {
		t.Fatalf("absent: %v %v", slugs(p), err)
	}
	all, err := s.List(ctx, Query{})
	if err != nil || all.Items[2].Tags == nil || len(all.Items[2].Tags) != 0 {
		t.Fatalf("untagged = %#v %v", all.Items[2].Tags, err)
	}
	p, err = s.List(ctx, Query{Tags: []string{"work"}, After: domain.Cursor{Ref: "aa"}, Limit: 1})
	if err != nil || !reflect.DeepEqual(slugs(p), []string{"bb"}) || p.NextAfter != "" {
		t.Fatalf("work after aa: %v %q %v", slugs(p), p.NextAfter, err)
	}
}

func TestListRefusesBadQueries(t *testing.T) {
	s := newStore(t)
	ctx := context.Background()
	mk(t, s, "aa")
	if _, err := s.List(ctx, Query{After: domain.Cursor{Ref: "missing"}}); !errors.Is(err, ErrBadCursor) {
		t.Errorf("unknown slug cursor = %v", err)
	}
	for _, q := range []Query{{Limit: -1}, {Limit: domain.MaxPageSize + 1}, {Tags: []string{"no way"}}} {
		var bad InvalidError
		if _, err := s.List(ctx, q); !errors.Is(err, ErrInvalid) || !errors.As(err, &bad) || bad.Next == "" {
			t.Errorf("%+v: %v", q, err)
		}
	}
	if p, err := s.List(ctx, Query{Limit: domain.MaxPageSize}); err != nil || len(p.Items) != 1 {
		t.Errorf("the maximum limit = %v %v", slugs(p), err)
	}
}

func TestNowDefaultsToTheWallClock(t *testing.T) {
	s := newStore(t)
	s.Now = nil
	before := time.Now().Add(-time.Second)
	i := mk(t, s, "aa")
	if i.CreatedAt.Before(before) || i.CreatedAt.After(time.Now().Add(time.Second)) || i.CreatedAt.Location() != time.UTC {
		t.Errorf("created_at = %v", i.CreatedAt)
	}
}

func TestSlugsThatReadAsULIDsStayReferences(t *testing.T) {
	s := newStore(t)
	ctx := context.Background()
	digits := "01234567890123456789012345"
	lowerULID := "01arz3ndektsv4rrffq69g5fav"
	d := mk(t, s, digits)
	l := mk(t, s, lowerULID)
	z := mk(t, s, "zz-last")
	if d.Slug != digits || l.Slug != lowerULID {
		t.Fatalf("create answered %q and %q", d.Slug, l.Slug)
	}
	for _, tc := range []struct {
		ref  string
		want Initiative
	}{
		{digits, d}, {d.ID, d}, {lowerULID, l}, {l.ID, l}, {strings.ToUpper(lowerULID[:10]) + lowerULID[10:], l},
	} {
		got, err := s.Get(ctx, tc.ref)
		if err != nil || got.ID != tc.want.ID {
			t.Errorf("Get(%q) = %q, %v; want %q", tc.ref, got.ID, err, tc.want.ID)
		}
	}
	p, err := s.List(ctx, Query{After: domain.Cursor{Ref: digits}})
	if err != nil || !reflect.DeepEqual(slugs(p), []string{lowerULID, "zz-last"}) {
		t.Fatalf("after %s: %v %v", digits, slugs(p), err)
	}
	p, err = s.List(ctx, Query{After: domain.Cursor{Ref: lowerULID}})
	if err != nil || !reflect.DeepEqual(slugs(p), []string{"zz-last"}) {
		t.Fatalf("after %s: %v %v", lowerULID, slugs(p), err)
	}
	_ = z
}

func TestReferencesAreCaseInsensitive(t *testing.T) {
	s := newStore(t)
	ctx := context.Background()
	a := mk(t, s, "phone-onboard")
	mk(t, s, "next-one")
	for _, ref := range []string{"Phone-Onboard", "PHONE-ONBOARD", a.ID, strings.ToLower(a.ID)} {
		if got, err := s.Get(ctx, ref); err != nil || got.ID != a.ID {
			t.Errorf("Get(%q) = %q, %v", ref, got.ID, err)
		}
		p, err := s.List(ctx, Query{After: domain.Cursor{Ref: ref}})
		if err != nil || !reflect.DeepEqual(slugs(p), []string{"next-one"}) {
			t.Errorf("after %q: %v %v", ref, slugs(p), err)
		}
	}
}

func TestAnExistingSlugBeatsAnotherRowsLowercaseID(t *testing.T) {
	s := newStore(t)
	ctx := context.Background()
	a := mk(t, s, "aa")
	// A slug spelled like another initiative's id in lowercase names its own row
	// when the text is given as is, and the id row when given in capitals.
	b := mk(t, s, strings.ToLower(a.ID))
	if got, err := s.Get(ctx, strings.ToLower(a.ID)); err != nil || got.ID != b.ID {
		t.Errorf("lowercase text = %q, %v; want the slug row %q", got.ID, err, b.ID)
	}
	if got, err := s.Get(ctx, a.ID); err != nil || got.ID != a.ID {
		t.Errorf("capitals = %q, %v; want the id row %q", got.ID, err, a.ID)
	}
}

func TestAKeysetULIDThatNamesNoRowStillPages(t *testing.T) {
	s := newStore(t)
	mk(t, s, "aa")
	p, err := s.List(context.Background(), Query{After: domain.Cursor{Ref: "00000000000000000000000000"}})
	if err != nil || len(p.Items) != 1 {
		t.Fatalf("%v %v", slugs(p), err)
	}
}

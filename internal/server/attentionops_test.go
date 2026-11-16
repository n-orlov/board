package server

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"path/filepath"
	"testing"
	"time"

	"github.com/n-orlov/village-hall/internal/attention"
	"github.com/n-orlov/village-hall/internal/store"
)

func TestAttentionAnswers503WithoutAStore(t *testing.T) {
	t.Parallel()
	got, _ := attentionOps{}.GetAttention(context.Background(), GetAttentionRequestObject{})
	if p, ok := got.(problemResponse); !ok || p.status != http.StatusServiceUnavailable {
		t.Errorf("attention without a store = %#v, want a 503 problem", got)
	}
}

func TestAttentionFieldsAreFullOrCounts(t *testing.T) {
	t.Parallel()
	for in, want := range map[string][2]bool{"": {false, true}, "full": {false, true}, "counts": {true, true}, "summary": {false, false}, "COUNTS": {false, false}} {
		counts, ok := attentionCountsOnly(&in)
		if counts != want[0] || ok != want[1] {
			t.Errorf("fields=%q = %v, %v; want %v", in, counts, ok, want)
		}
	}
	if counts, ok := attentionCountsOnly(nil); counts || !ok {
		t.Errorf("no fields = %v, %v; want the full answer", counts, ok)
	}
	got, _ := attentionOps{attention: &attention.Store{}}.GetAttention(context.Background(),
		GetAttentionRequestObject{Params: GetAttentionParams{Fields: ptr("nothing")}})
	if p, ok := got.(problemResponse); !ok || p.status != http.StatusBadRequest || p.next == "" {
		t.Errorf("fields=nothing = %#v, want a 400 with a next", got)
	}
}

func TestAttentionFailureMapsEachErrorToItsStatus(t *testing.T) {
	t.Parallel()
	for name, tc := range map[string]struct {
		err    error
		status int
	}{
		"invalid":    {attention.InvalidError{Msg: "bad tag", Next: "fix it"}, http.StatusBadRequest},
		"initiative": {fmt.Errorf("%w: zz", attention.ErrNotFound), http.StatusNotFound},
	} {
		p, ok := attentionFailure(tc.err)
		if !ok || p.status != tc.status || p.next == "" {
			t.Errorf("%s = %+v, %v; want status %d with a next", name, p, ok, tc.status)
		}
	}
	if _, ok := attentionFailure(errors.New("disk")); ok {
		t.Error("a server error was mapped to a problem")
	}
}

func TestAttentionQueryCarriesBothFilters(t *testing.T) {
	t.Parallel()
	q := attentionQuery(GetAttentionParams{Initiative: &[]string{"aa", "bb"}, Tag: &[]string{"web"}})
	if len(q.Initiatives) != 2 || q.Initiatives[1] != "bb" || len(q.Tags) != 1 || q.Tags[0] != "web" {
		t.Errorf("query = %+v", q)
	}
	if q := attentionQuery(GetAttentionParams{}); q.Initiatives != nil || q.Tags != nil {
		t.Errorf("an empty query = %+v, want no filters", q)
	}
}

func TestAttentionDocumentCarriesEveryField(t *testing.T) {
	t.Parallel()
	at := time.Date(2026, 11, 9, 10, 0, 0, 0, time.UTC)
	who := attention.Actor{Handle: "claude@laptop", WayBack: "ssh a"}
	got := apiAttention(attention.Result{
		OpenAsks: []attention.OpenAsk{{Ref: "A1", Question: "q", Task: "aa-1", Asker: who, Mode: "poll", AskerWaiting: true, Blocking: true,
			CreatedAt: at, Age: 90*time.Second + 400*time.Millisecond}},
		DoneNotClosed: []attention.DoneTask{{Task: "aa-1", Title: "t", DoneBy: who, DoneAt: at,
			LastLease: &attention.LastLease{Lease: "L1", Holder: who, Outcome: "ok", EndedAt: at}}, {Task: "aa-2"}},
		ExpiredLeases: []attention.ExpiredLease{{Lease: "L2", Holder: who, Task: "aa-1", Title: "t", Intent: "i", RenewedAt: at, ExpiredAt: at}},
		OverdueWatches: []attention.OverdueWatch{{Task: "aa-3", Title: "w", Owner: who, NextCheckAt: at,
			LastCheck: &attention.LastCheck{Summary: "s", By: who, At: at}}, {Task: "aa-4"}},
		Conflicts: []attention.Conflict{{Resource: "db:x", Tasks: []string{"aa-1", "aa-2"},
			Holders: []attention.Holder{{Lease: "L1", Actor: who, Task: "aa-1", Intent: "i", ExpiresAt: at}}}},
		Counts: attention.Counts{OpenAsks: 1, DoneNotClosed: 2, ExpiredLeases: 1, OverdueWatches: 2, Conflicts: 1, Total: 7},
	})
	ask := (*got.OpenAsks)[0]
	if ask.AgeSeconds != 90 || ask.Mode != "poll" || !ask.AskerWaiting || !ask.Blocking || text(ask.Task) != "aa-1" || ask.Asker.Handle != who.Handle || text(ask.Asker.WayBack) != "ssh a" {
		t.Errorf("ask = %+v", ask)
	}
	done := *got.DoneNotClosed
	if done[0].LastLease == nil || text(done[0].LastLease.Outcome) != "ok" || done[0].LastLease.Holder.Handle != who.Handle || done[1].LastLease != nil {
		t.Errorf("done = %+v", done)
	}
	watches := *got.OverdueWatches
	if watches[0].LastCheck == nil || watches[0].LastCheck.Summary != "s" || watches[1].LastCheck != nil {
		t.Errorf("watches = %+v", watches)
	}
	if c := (*got.Conflicts)[0]; c.Resource != "db:x" || len(c.Holders) != 1 || len(c.Tasks) != 2 || c.Holders[0].Actor.Handle != who.Handle {
		t.Errorf("conflict = %+v", c)
	}
	if l := (*got.ExpiredLeases)[0]; l.Lease != "L2" || l.Intent != "i" || l.Holder.Handle != who.Handle {
		t.Errorf("expired lease = %+v", l)
	}
	if got.Counts.Total != 7 || got.Counts.DoneNotClosed != 2 || got.Counts.OverdueWatches != 2 {
		t.Errorf("counts = %+v", got.Counts)
	}
}

func TestAttentionSectionsAreEmptyListsNotNull(t *testing.T) {
	t.Parallel()
	got := apiAttention(attention.Result{})
	for name, section := range map[string]bool{"open": got.OpenAsks != nil, "done": got.DoneNotClosed != nil, "expired": got.ExpiredLeases != nil,
		"watches": got.OverdueWatches != nil, "conflicts": got.Conflicts != nil} {
		if !section {
			t.Errorf("%s section is nil, want an empty list", name)
		}
	}
}

func attentionOpsOnEmptyBoard(t *testing.T) attentionOps {
	t.Helper()
	st, err := store.Open("sqlite://" + filepath.Join(t.TempDir(), "b.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	if err := st.Migrate(context.Background()); err != nil {
		t.Fatal(err)
	}
	return attentionOps{attention: &attention.Store{Store: st}}
}

func TestGetAttentionOnAnEmptyBoardAnswersFullAndCounts(t *testing.T) {
	t.Parallel()
	o := attentionOpsOnEmptyBoard(t)
	for name, fields := range map[string]*string{"full": nil, "counts": ptr("counts")} {
		got, err := o.GetAttention(context.Background(), GetAttentionRequestObject{Params: GetAttentionParams{Fields: fields}})
		doc, ok := got.(GetAttention200JSONResponse)
		if err != nil || !ok || Attention(doc).Counts != (AttentionCounts{}) {
			t.Fatalf("%s = %#v, %v; want a 200 with zero counts", name, got, err)
		}
		if (Attention(doc).OpenAsks == nil) != (name == "counts") {
			t.Errorf("%s: sections present = %v", name, Attention(doc).OpenAsks != nil)
		}
	}
}

func TestGetAttentionRefusesABadTagAndAnUnknownInitiative(t *testing.T) {
	t.Parallel()
	o := attentionOpsOnEmptyBoard(t)
	for name, tc := range map[string]struct {
		params GetAttentionParams
		status int
	}{
		"tag":        {GetAttentionParams{Tag: &[]string{"bad tag"}}, http.StatusBadRequest},
		"initiative": {GetAttentionParams{Initiative: &[]string{"zz"}}, http.StatusNotFound},
	} {
		got, err := o.GetAttention(context.Background(), GetAttentionRequestObject{Params: tc.params})
		if p, ok := got.(problemResponse); err != nil || !ok || p.status != tc.status || p.next == "" {
			t.Errorf("%s = %#v, %v; want a %d problem with a next", name, got, err, tc.status)
		}
	}
}

func TestGetAttentionReturnsAServerErrorAsIs(t *testing.T) {
	t.Parallel()
	st, err := store.Open("sqlite://" + filepath.Join(t.TempDir(), "b.db"))
	if err != nil {
		t.Fatal(err)
	}
	_ = st.Close()
	got, err := attentionOps{attention: &attention.Store{Store: st}}.GetAttention(context.Background(), GetAttentionRequestObject{})
	if err == nil || got != nil {
		t.Errorf("a closed store = %#v, %v; want the error", got, err)
	}
}

func TestAttentionAskModeAcceptsOnlyTheDeclaredValues(t *testing.T) {
	t.Parallel()
	if !AttentionAskModePoll.Valid() || !AttentionAskModeStop.Valid() || AttentionAskMode("x").Valid() {
		t.Error("the attention ask mode enum does not accept exactly poll and stop")
	}
}

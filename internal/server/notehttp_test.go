package server

import (
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/n-orlov/board/internal/asks"
	"github.com/n-orlov/board/internal/attention"
	"github.com/n-orlov/board/internal/cursors"
	"github.com/n-orlov/board/internal/notes"
	"github.com/n-orlov/board/internal/store"
)

// boardRig serves notes, cursors, attention and asks over one real database,
// so the handlers are exercised through the generated wrappers and the strict
// handler, not only as methods.
func boardRig(t *testing.T, dbs databases) *rig {
	t.Helper()
	st := openStore(t, dbs.New(t))
	if err := st.Migrate(t.Context()); err != nil {
		t.Fatal(err)
	}
	cfg := streamSettings{poll: time.Hour, heartbeat: time.Minute, extra: func(s *store.Store) []Option {
		return []Option{
			WithNotes(&notes.Store{Store: s}), WithCursors(&cursors.Store{Store: s}),
			WithAttention(&attention.Store{Store: s}), WithAsks(&asks.Store{Store: s}),
		}
	}}
	return startRig(t, st, newToken(t, st), cfg)
}

// call sends one request and returns its status and decoded JSON object.
func (r *rig) call(method, path, body string) (int, map[string]any) {
	r.t.Helper()
	resp := r.do(method, path, body, r.token)
	defer func() { _ = resp.Body.Close() }()
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		r.t.Fatal(err)
	}
	var doc map[string]any
	if err := json.Unmarshal(raw, &doc); err != nil {
		r.t.Fatalf("%s %s = %d with a body that is not JSON: %q", method, path, resp.StatusCode, raw)
	}
	return resp.StatusCode, doc
}

func (r *rig) want(method, path, body string, status int) map[string]any {
	r.t.Helper()
	got, doc := r.call(method, path, body)
	if got != status {
		r.t.Fatalf("%s %s = %d %v, want %d", method, path, got, doc, status)
	}
	return doc
}

func itemSummaries(doc map[string]any) []string {
	var out []string
	for _, it := range doc["items"].([]any) {
		out = append(out, it.(map[string]any)["summary"].(string))
	}
	return out
}

func TestNotesPostGetAndListThroughTheServer(t *testing.T) {
	forEachLeg(t, func(t *testing.T, _ string, dbs databases) {
		r := boardRig(t, dbs)
		posted := r.want(http.MethodPost, "/api/v1/notes", `{"summary":"first","body":"the long text"}`, http.StatusCreated)
		ref, _ := posted["ref"].(string)
		if ref != "N1" || posted["body"] != "the long text" {
			t.Fatalf("posted note = %v, want ref N1 with its body", posted)
		}
		r.want(http.MethodPost, "/api/v1/notes", `{"summary":"second"}`, http.StatusCreated)
		r.want(http.MethodPost, "/api/v1/notes", ``, http.StatusBadRequest)
		if got := r.want(http.MethodGet, "/api/v1/notes/"+ref, "", http.StatusOK); got["summary"] != "first" || got["body"] != "the long text" {
			t.Errorf("GET note = %v", got)
		}
		r.want(http.MethodGet, "/api/v1/notes/N99", "", http.StatusNotFound)

		list := r.want(http.MethodGet, "/api/v1/notes", "", http.StatusOK)
		if got := itemSummaries(list); strings.Join(got, ",") != "first,second" {
			t.Errorf("listed summaries = %v, want first,second", got)
		}
		if first := list["items"].([]any)[0].(map[string]any); first["body"] != nil {
			t.Errorf("a summary list carries the body %v", first["body"])
		}
		full := r.want(http.MethodGet, "/api/v1/notes?fields=full", "", http.StatusOK)
		if first := full["items"].([]any)[0].(map[string]any); first["body"] != "the long text" {
			t.Errorf("a full list leaves out the body: %v", first)
		}
		r.want(http.MethodGet, "/api/v1/notes?fields=nothing", "", http.StatusBadRequest)
		r.want(http.MethodGet, "/api/v1/notes?since=zzz", "", http.StatusBadRequest)
	})
}

func TestNotesAckMovesTheCursorAndSinceCursorReadsAfterIt(t *testing.T) {
	forEachLeg(t, func(t *testing.T, _ string, dbs databases) {
		r := boardRig(t, dbs)
		for _, s := range []string{"one", "two", "three"} {
			r.want(http.MethodPost, "/api/v1/notes", `{"summary":"`+s+`"}`, http.StatusCreated)
		}
		// An empty cursor reads from the start, and ack=true moves it to the last note returned.
		first := r.want(http.MethodGet, "/api/v1/notes?since=cursor&limit=2&ack=true", "", http.StatusOK)
		if got := strings.Join(itemSummaries(first), ","); got != "one,two" {
			t.Fatalf("first read = %s, want one,two", got)
		}
		rest := r.want(http.MethodGet, "/api/v1/notes?since=cursor&ack=true", "", http.StatusOK)
		if got := strings.Join(itemSummaries(rest), ","); got != "three" {
			t.Errorf("read after the ack = %s, want three", got)
		}
		empty := r.want(http.MethodGet, "/api/v1/notes?since=cursor&ack=true", "", http.StatusOK)
		if got := itemSummaries(empty); len(got) != 0 {
			t.Errorf("read after everything was acked = %v, want none", got)
		}
		// A read without ack leaves the cursor where it was.
		put := r.want(http.MethodPut, "/api/v1/cursors/notes", `{"position":"N1"}`, http.StatusOK)
		if put["stream"] != "notes" || put["ref"] != "N1" {
			t.Errorf("PUT cursor = %v, want the notes cursor at N1", put)
		}
		for i := 0; i < 2; i++ {
			got := r.want(http.MethodGet, "/api/v1/notes?since=cursor", "", http.StatusOK)
			if s := strings.Join(itemSummaries(got), ","); s != "two,three" {
				t.Errorf("read %d without ack = %s, want two,three", i, s)
			}
		}
	})
}

func TestPutCursorRefusesAMissingBodyAnUnknownStreamAndABadPosition(t *testing.T) {
	forEachLeg(t, func(t *testing.T, _ string, dbs databases) {
		r := boardRig(t, dbs)
		r.want(http.MethodPut, "/api/v1/cursors/notes", ``, http.StatusBadRequest)
		r.want(http.MethodPut, "/api/v1/cursors/notes", `{"position":"not-a-ref"}`, http.StatusBadRequest)
		r.want(http.MethodPut, "/api/v1/cursors/notes", `{"position":"N77"}`, http.StatusBadRequest)
		r.want(http.MethodPut, "/api/v1/cursors/nowhere", `{"position":"N1"}`, http.StatusNotFound)
	})
}

func TestAttentionThroughTheServerAnswersFullAndCounts(t *testing.T) {
	forEachLeg(t, func(t *testing.T, _ string, dbs databases) {
		r := boardRig(t, dbs)
		r.raiseAsk()
		full := r.want(http.MethodGet, "/api/v1/attention", "", http.StatusOK)
		if open, _ := full["open_asks"].([]any); len(open) != 1 {
			t.Errorf("attention = %v, want the one open ask", full)
		}
		counts := r.want(http.MethodGet, "/api/v1/attention?fields=counts", "", http.StatusOK)
		if c, _ := counts["counts"].(map[string]any); c["open_asks"] != float64(1) || counts["open_asks"] != nil {
			t.Errorf("attention counts = %v, want the counts without the sections", counts)
		}
		r.want(http.MethodGet, "/api/v1/attention?fields=nothing", "", http.StatusBadRequest)
	})
}

func TestAskListAndWithdrawThroughTheServer(t *testing.T) {
	forEachLeg(t, func(t *testing.T, _ string, dbs databases) {
		r := boardRig(t, dbs)
		withBody, plain := r.raiseAsk(), r.raiseAsk()
		list := r.want(http.MethodGet, "/api/v1/asks?state=open", "", http.StatusOK)
		if n := len(list["items"].([]any)); n != 2 {
			t.Fatalf("open asks = %d, want 2", n)
		}
		got := r.want(http.MethodPost, "/api/v1/asks/"+withBody+"/withdraw", `{"reason":"not needed"}`, http.StatusOK)
		if got["state"] != "withdrawn" {
			t.Errorf("withdrawn ask = %v", got)
		}
		if got := r.want(http.MethodPost, "/api/v1/asks/"+plain+"/withdraw", ``, http.StatusOK); got["state"] != "withdrawn" {
			t.Errorf("withdrawn ask without a body = %v", got)
		}
		r.want(http.MethodPost, "/api/v1/asks/"+plain+"/withdraw", ``, http.StatusConflict)
		open := r.want(http.MethodGet, "/api/v1/asks?state=open", "", http.StatusOK)
		if n := len(open["items"].([]any)); n != 0 {
			t.Errorf("open asks after both withdrawals = %d, want 0", n)
		}
		r.want(http.MethodGet, "/api/v1/asks?state=bogus", "", http.StatusBadRequest)
	})
}

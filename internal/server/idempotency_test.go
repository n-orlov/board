package server

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"regexp"
	"sort"
	"strings"
	"sync/atomic"
	"testing"
	"testing/iotest"
	"time"

	"github.com/n-orlov/village-hall/api"
	"github.com/n-orlov/village-hall/internal/auth"
	"github.com/n-orlov/village-hall/internal/idempotency"
	"github.com/n-orlov/village-hall/internal/leases"
)

// idemWorld is a key server with a second principal, "other", and helpers that
// send a request with an Idempotency-Key.
type idemWorld struct {
	*keyServer
	otherToken string
}

func newIdemWorld(t *testing.T, dbs databases) *idemWorld {
	t.Helper()
	s := newKeyServerOn(t, dbs.New(t), keyServerBuild)
	token, _, err := s.keys.Create(context.Background(), "other", "admin-cli", 0)
	if err != nil {
		t.Fatal(err)
	}
	return &idemWorld{keyServer: s, otherToken: token}
}

// call sends a request as the principal that token names; keys are the values
// of the Idempotency-Key header, none for no header.
func (w *idemWorld) call(token, method, path, body, actor string, keys ...string) *httptest.ResponseRecorder {
	w.t.Helper()
	r := httptest.NewRequest(method, path, strings.NewReader(body))
	r.Header.Set("Authorization", "Bearer "+token)
	r.Header.Set(ActorHeader, actor)
	if body != "" {
		r.Header.Set("Content-Type", "application/json")
	}
	if len(keys) > 0 {
		r.Header[IdempotencyKeyHeader] = keys
	}
	rec := httptest.NewRecorder()
	w.h.ServeHTTP(rec, r)
	return rec
}

// post sends a POST as "caller" with the one key.
func (w *idemWorld) post(path, body, actor, key string) *httptest.ResponseRecorder {
	w.t.Helper()
	return w.call(w.token, http.MethodPost, path, body, actor, key)
}

// must sends a POST without a key and requires a 2xx; it returns the document.
func (w *idemWorld) must(path, body, actor string) map[string]any {
	w.t.Helper()
	rec := w.call(w.token, http.MethodPost, path, body, actor)
	if rec.Code/100 != 2 {
		w.t.Fatalf("POST %s %s = %d %s", path, body, rec.Code, rec.Body)
	}
	var doc map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &doc)
	return doc
}

func (w *idemWorld) count(table string) int {
	w.t.Helper()
	var n int
	if err := w.st.Reader().QueryRowContext(context.Background(), "SELECT COUNT(*) FROM "+table).Scan(&n); err != nil {
		w.t.Fatal(err)
	}
	return n
}

// task makes a task of initiative aa and returns its ref.
func (w *idemWorld) newTask(title string) string {
	w.t.Helper()
	return w.must("/api/v1/tasks", fmt.Sprintf(`{"initiative":"aa","title":%q}`, title), "claude@host")["ref"].(string)
}

// newLease starts a lease for actor on a new task and returns the lease ref.
func (w *idemWorld) newLease(actor string) string {
	w.t.Helper()
	task := w.newTask("work of " + actor)
	started := w.must("/api/v1/leases", fmt.Sprintf(`{"task":%q,"intent":"mine"}`, task), actor)
	return started["lease"].(map[string]any)["ref"].(string)
}

// newAsk raises a blocking poll ask on a new task and returns the ask ref.
func (w *idemWorld) newAsk() string {
	w.t.Helper()
	return w.must("/api/v1/asks", fmt.Sprintf(`{"task":%q,"question":"Which?","mode":"poll","blocking":true}`, w.newTask("asked")), "claude@host")["ref"].(string)
}

// idemCase is how to send one POST operation, and how many events rows a
// first, successful send writes.
type idemCase struct {
	// events is the number of events rows the first send writes: one, but a ping
	// with progress writes a renewal and a milestone, and a finish a close and a lease end.
	events int
	// build makes the state the request needs and returns the request.
	build func(w *idemWorld) (path, body, actor string)
}

// idemExcluded lists the POST operations under /api/v1 that write nothing, each
// with the reason. Every POST there writes (an event at least), so none is excluded.
var idemExcluded = map[string]string{}

func fixed(path, body, actor string) func(*idemWorld) (string, string, string) {
	return func(*idemWorld) (string, string, string) { return path, body, actor }
}

// idemCases has one case for every POST operation under /api/v1.
var idemCases = map[string]idemCase{
	"POST /api/v1/keys": {1, fixed("/api/v1/keys", `{"name":"laptop"}`, "claude@host")},
	"POST /api/v1/keys/{name}/revoke": {1, func(w *idemWorld) (string, string, string) {
		w.must("/api/v1/keys", `{"name":"old"}`, "claude@host")
		return "/api/v1/keys/old/revoke", "", "claude@host"
	}},
	"POST /api/v1/initiatives": {1, fixed("/api/v1/initiatives", `{"slug":"bb","title":"B"}`, "claude@host")},
	"POST /api/v1/initiatives/{slug}/close": {1, func(w *idemWorld) (string, string, string) {
		w.must("/api/v1/initiatives", `{"slug":"cc","title":"C"}`, "claude@host")
		return "/api/v1/initiatives/cc/close", `{"resolution":"completed"}`, "claude@host"
	}},
	"POST /api/v1/initiatives/{slug}/reopen": {1, func(w *idemWorld) (string, string, string) {
		w.must("/api/v1/initiatives", `{"slug":"dd","title":"D"}`, "claude@host")
		w.must("/api/v1/initiatives/dd/close", "", "claude@host")
		return "/api/v1/initiatives/dd/reopen", "", "claude@host"
	}},
	"POST /api/v1/tasks": {1, fixed("/api/v1/tasks", `{"initiative":"aa","title":"made twice"}`, "claude@host")},
	"POST /api/v1/tasks/{ref}/done": {1, func(w *idemWorld) (string, string, string) {
		return "/api/v1/tasks/" + w.newTask("d") + "/done", `{"outcome":"shipped"}`, "claude@host"
	}},
	"POST /api/v1/tasks/{ref}/reopen": {1, func(w *idemWorld) (string, string, string) {
		ref := w.newTask("r")
		w.must("/api/v1/tasks/"+ref+"/done", "", "claude@host")
		return "/api/v1/tasks/" + ref + "/reopen", "", "claude@host"
	}},
	"POST /api/v1/tasks/{ref}/close": {1, func(w *idemWorld) (string, string, string) {
		return "/api/v1/tasks/" + w.newTask("c") + "/close", `{"resolution":"dropped"}`, "claude@host"
	}},
	"POST /api/v1/tasks/{ref}/deps/{blockedByRef}": {1, func(w *idemWorld) (string, string, string) {
		return "/api/v1/tasks/" + w.newTask("blocked") + "/deps/" + w.newTask("blocker"), "", "claude@host"
	}},
	"POST /api/v1/tasks/{ref}/checks": {1, func(w *idemWorld) (string, string, string) {
		watch := w.must("/api/v1/tasks", `{"initiative":"aa","title":"watched","kind":"watch","cadence_seconds":60}`, "claude@host")["ref"].(string)
		return "/api/v1/tasks/" + watch + "/checks", `{"summary":"still fine"}`, "claude@host"
	}},
	"POST /api/v1/tasks/{ref}/milestones": {1, func(w *idemWorld) (string, string, string) {
		return "/api/v1/tasks/" + w.newTask("m") + "/milestones", `{"summary":"halfway"}`, "claude@host"
	}},
	"POST /api/v1/leases": {1, func(w *idemWorld) (string, string, string) {
		return "/api/v1/leases", fmt.Sprintf(`{"task":%q,"intent":"mine"}`, w.newTask("l")), "claude@host"
	}},
	"POST /api/v1/leases/{ref}/renew": {1, func(w *idemWorld) (string, string, string) {
		ref := w.newLease("renewer@host")
		return "/api/v1/leases/" + ref + "/renew", `{"ttl_seconds":900}`, "renewer@host"
	}},
	"POST /api/v1/leases/{ref}/end": {1, func(w *idemWorld) (string, string, string) {
		ref := w.newLease("ender@host")
		return "/api/v1/leases/" + ref + "/end", `{"outcome":"done"}`, "ender@host"
	}},
	"POST /api/v1/leases/{ref}/ack": {1, func(w *idemWorld) (string, string, string) {
		ref := w.newLease("acker@host")
		later := func() time.Time { return time.Now().Add(48 * time.Hour) }
		if n, err := (&leases.Store{Store: w.st, Now: later}).ExpireDue(context.Background()); err != nil || n == 0 {
			w.t.Fatalf("ExpireDue = %d, %v", n, err)
		}
		return "/api/v1/leases/" + ref + "/ack", "", "acker@host"
	}},
	"POST /api/v1/asks": {1, func(w *idemWorld) (string, string, string) {
		return "/api/v1/asks", fmt.Sprintf(`{"task":%q,"question":"Which?","mode":"poll","blocking":true}`, w.newTask("a")), "claude@host"
	}},
	"POST /api/v1/asks/{ref}/answer": {1, func(w *idemWorld) (string, string, string) {
		return "/api/v1/asks/" + w.newAsk() + "/answer", `{"answer":"this one"}`, "nik@phone"
	}},
	"POST /api/v1/asks/{ref}/withdraw": {1, func(w *idemWorld) (string, string, string) {
		return "/api/v1/asks/" + w.newAsk() + "/withdraw", `{"reason":"not needed"}`, "claude@host"
	}},
	"POST /api/v1/notes": {1, fixed("/api/v1/notes", `{"summary":"hello"}`, "nik@phone")},
	"POST /api/v1/start": {1, func(w *idemWorld) (string, string, string) {
		return "/api/v1/start", fmt.Sprintf(`{"task":%q,"intent":"mine"}`, w.newTask("s")), "starter@host"
	}},
	"POST /api/v1/finish": {2, func(w *idemWorld) (string, string, string) {
		w.newLease("finisher@host")
		return "/api/v1/finish", `{"outcome":"shipped it"}`, "finisher@host"
	}},
	"POST /api/v1/ping": {2, func(w *idemWorld) (string, string, string) {
		w.newLease("pinger@host")
		return "/api/v1/ping", `{"progress":"half way"}`, "pinger@host"
	}},
	"POST /api/v1/catchup": {1, func(w *idemWorld) (string, string, string) {
		w.must("/api/v1/notes", `{"summary":"for the reader"}`, "writer@host")
		return "/api/v1/catchup", "", "reader@host"
	}},
}

// postsUnderAPI reads every `POST /api/v1/...` operation from the OpenAPI document.
func postsUnderAPI(t *testing.T) []string {
	t.Helper()
	generic, err := api.JSON()
	if err != nil {
		t.Fatal(err)
	}
	raw, err := json.Marshal(generic)
	if err != nil {
		t.Fatal(err)
	}
	var doc struct {
		Paths map[string]map[string]json.RawMessage `json:"paths"`
	}
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatal(err)
	}
	var ops []string
	for path, methods := range doc.Paths {
		if _, ok := methods["post"]; ok && strings.HasPrefix(path, "/api/v1/") {
			ops = append(ops, "POST "+path)
		}
	}
	sort.Strings(ops)
	return ops
}

func TestEveryPostUnderAPIIsCoveredOrExcludedWithAReason(t *testing.T) {
	t.Parallel()
	ops := postsUnderAPI(t)
	if len(ops) < 20 {
		t.Fatalf("the document lists %d POST operations under /api/v1: %v", len(ops), ops)
	}
	inDoc := map[string]bool{}
	for _, op := range ops {
		inDoc[op] = true
		_, covered := idemCases[op]
		reason, excluded := idemExcluded[op]
		if covered == excluded {
			t.Errorf("%s is covered = %v and excluded = %v: it must be exactly one", op, covered, excluded)
		}
		if excluded && reason == "" {
			t.Errorf("%s is excluded without a reason", op)
		}
	}
	for op := range idemCases {
		if !inDoc[op] {
			t.Errorf("idemCases names %s, which the document does not have", op)
		}
	}
	for op := range idemExcluded {
		if !inDoc[op] {
			t.Errorf("idemExcluded names %s, which the document does not have", op)
		}
	}
}

// TestTheGeneratedRouterRefusesTwoKeyValuesOnEveryPost drives the generated router
// with no authentication or idempotency step in front: the parameter the OpenAPI
// document declares on each POST is read there too, and two values are a 400
// problem document with a next, never a handled request.
func TestTheGeneratedRouterRefusesTwoKeyValuesOnEveryPost(t *testing.T) {
	t.Parallel()
	mux := http.NewServeMux()
	HandlerWithOptions(strictOperations(operations{}), StdHTTPServerOptions{BaseRouter: mux, ErrorHandlerFunc: parameterUnreadable})
	h := withProblemNext(mux)
	segment := regexp.MustCompile(`\{[^}]*\}`)
	for _, op := range postsUnderAPI(t) {
		path := segment.ReplaceAllString(strings.TrimPrefix(op, "POST "), "x")
		r := httptest.NewRequest(http.MethodPost, path, nil)
		r.Header[IdempotencyKeyHeader] = []string{"one", "two"}
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, r)
		if doc := decode(t, rec); rec.Code != http.StatusBadRequest || doc["next"] == nil || doc["next"] == "" {
			t.Errorf("%s with two key values = %d %s, want a 400 problem with a next", op, rec.Code, rec.Body)
		}
	}
}

func TestEveryWritingPostReplaysItsAnswerAndWritesOnce(t *testing.T) {
	t.Parallel()
	forEachLeg(t, func(t *testing.T, _ string, dbs databases) {
		w := newIdemWorld(t, dbs)
		w.must("/api/v1/initiatives", `{"slug":"aa","title":"A"}`, "claude@host")
		ops := make([]string, 0, len(idemCases))
		for op := range idemCases {
			ops = append(ops, op)
		}
		sort.Strings(ops)
		for i, op := range ops {
			c := idemCases[op]
			path, body, actor := c.build(w)
			key := fmt.Sprintf("scope-%d", i)
			before := w.count("events")
			first := w.post(path, body, actor, key)
			afterFirst := w.count("events")
			second := w.post(path, body, actor, key)
			if first.Code/100 != 2 {
				t.Errorf("%s: first send = %d %s, want 2xx", op, first.Code, first.Body)
				continue
			}
			if second.Code != first.Code || second.Body.String() != first.Body.String() || second.Header().Get("Content-Type") != first.Header().Get("Content-Type") {
				t.Errorf("%s: replay = %d %s (%s), want %d %s (%s)", op, second.Code, second.Body, second.Header().Get("Content-Type"),
					first.Code, first.Body, first.Header().Get("Content-Type"))
			}
			if got := first.Header().Get(ReplayedHeader); got != "" {
				t.Errorf("%s: the first answer carries %s: %s", op, ReplayedHeader, got)
			}
			if got := second.Header().Get(ReplayedHeader); got != "true" {
				t.Errorf("%s: the replay carries %s = %q, want true", op, ReplayedHeader, got)
			}
			if got := afterFirst - before; got != c.events {
				t.Errorf("%s: the first send wrote %d events rows, want %d", op, got, c.events)
			}
			if got := w.count("events") - afterFirst; got != 0 {
				t.Errorf("%s: the replay wrote %d events rows, want none", op, got)
			}
		}
	})
}

func TestAReplayOfAKeyCreationReturnsTheSameKeyAndTheStoredRowHoldsNeitherKeyNorSecret(t *testing.T) {
	t.Parallel()
	forEachLeg(t, func(t *testing.T, _ string, dbs databases) {
		w := newIdemWorld(t, dbs)
		keys := w.count("api_keys")
		first := w.post("/api/v1/keys", `{"name":"laptop"}`, "claude@host", "make-laptop")
		second := w.post("/api/v1/keys", `{"name":"laptop"}`, "claude@host", "make-laptop")
		secret, _ := decode(t, first)["key"].(string)
		if !auth.WellFormed(secret) || decode(t, second)["key"] != secret || w.count("api_keys") != keys+1 {
			t.Fatalf("first %s, replay %s, keys %d -> %d", first.Body, second.Body, keys, w.count("api_keys"))
		}
		var stored, hashed string
		if err := w.st.Reader().QueryRowContext(context.Background(),
			`SELECT body, key_hash FROM idempotency_keys WHERE principal = 'key:caller'`).Scan(&stored, &hashed); err != nil {
			t.Fatal(err)
		}
		if strings.Contains(stored, secret) || strings.Contains(stored, "laptop") || strings.Contains(hashed, "make-laptop") {
			t.Errorf("the stored row holds the key or the answer: %q %q", stored, hashed)
		}
	})
}

func TestTheSameKeyWithADifferentRequestIs422WithANextAndWritesNothing(t *testing.T) {
	t.Parallel()
	forEachLeg(t, func(t *testing.T, _ string, dbs databases) {
		w := newIdemWorld(t, dbs)
		if rec := w.post("/api/v1/notes", `{"summary":"one"}`, "nik@phone", "k"); rec.Code != http.StatusCreated {
			t.Fatalf("first = %d %s", rec.Code, rec.Body)
		}
		events := w.count("events")
		for name, c := range map[string]struct{ path, body, actor string }{
			"body":   {"/api/v1/notes", `{"summary":"two"}`, "nik@phone"},
			"actor":  {"/api/v1/notes", `{"summary":"one"}`, "codex@host"},
			"path":   {"/api/v1/catchup", "", "nik@phone"},
			"target": {"/api/v1/notes?x=1", `{"summary":"one"}`, "nik@phone"},
		} {
			rec := w.post(c.path, c.body, c.actor, "k")
			doc := decode(t, rec)
			if rec.Code != http.StatusUnprocessableEntity || doc["next"] == "" || doc["next"] == nil || rec.Header().Get(ReplayedHeader) != "" {
				t.Errorf("a different %s = %d %s, want 422 with a next and no replay header", name, rec.Code, rec.Body)
			}
		}
		if w.count("events") != events || w.count("notes") != 1 {
			t.Errorf("a refused request wrote: events %d -> %d, notes %d", events, w.count("events"), w.count("notes"))
		}
	})
}

func TestAnotherPrincipalWithTheSameKeyIsANewRequest(t *testing.T) {
	t.Parallel()
	forEachLeg(t, func(t *testing.T, _ string, dbs databases) {
		w := newIdemWorld(t, dbs)
		a := w.call(w.token, http.MethodPost, "/api/v1/notes", `{"summary":"one"}`, "nik@phone", "shared")
		b := w.call(w.otherToken, http.MethodPost, "/api/v1/notes", `{"summary":"one"}`, "nik@phone", "shared")
		if a.Code != http.StatusCreated || b.Code != http.StatusCreated || b.Header().Get(ReplayedHeader) != "" || w.count("notes") != 2 {
			t.Fatalf("caller %d %s, other %d %s (replay %q), notes %d", a.Code, a.Body, b.Code, b.Body, b.Header().Get(ReplayedHeader), w.count("notes"))
		}
		again := w.call(w.otherToken, http.MethodPost, "/api/v1/notes", `{"summary":"one"}`, "nik@phone", "shared")
		if again.Header().Get(ReplayedHeader) != "true" || decode(t, again)["ref"] != decode(t, b)["ref"] || w.count("notes") != 2 {
			t.Errorf("the other principal's own replay = %d %s (replay %q), notes %d", again.Code, again.Body, again.Header().Get(ReplayedHeader), w.count("notes"))
		}
	})
}

func TestAKeyOlderThan24HoursIsNewAndTheSweeperPurgesIt(t *testing.T) {
	t.Parallel()
	forEachLeg(t, func(t *testing.T, _ string, dbs databases) {
		w := newIdemWorld(t, dbs)
		send := func() *httptest.ResponseRecorder {
			return w.post("/api/v1/notes", `{"summary":"daily"}`, "nik@phone", "daily")
		}
		first := send()
		w.clock.Advance(idempotency.Retention)
		if again := send(); again.Header().Get(ReplayedHeader) != "true" || w.count("notes") != 1 {
			t.Fatalf("at exactly 24 h = %d %s, replay %q, notes %d, want a replay", again.Code, again.Body, again.Header().Get(ReplayedHeader), w.count("notes"))
		}
		w.clock.Advance(time.Millisecond)
		fresh := send()
		if fresh.Code != http.StatusCreated || fresh.Header().Get(ReplayedHeader) != "" || w.count("notes") != 2 || decode(t, fresh)["ref"] == decode(t, first)["ref"] {
			t.Fatalf("past 24 h = %d %s (replay %q), notes %d, want a new note", fresh.Code, fresh.Body, fresh.Header().Get(ReplayedHeader), w.count("notes"))
		}
		w.clock.Advance(idempotency.Retention + time.Millisecond)
		store := idempotency.Store{Store: w.st, Now: w.clock.Now}
		if n, err := store.DeleteExpired(context.Background()); err != nil || n != 1 || w.count("idempotency_keys") != 0 {
			t.Errorf("the sweep deleted %d, %v, leaving %d rows", n, err, w.count("idempotency_keys"))
		}
	})
}

func TestAnInvalidKeyIs400WithANextAndWritesNothing(t *testing.T) {
	t.Parallel()
	forEachLeg(t, func(t *testing.T, _ string, dbs databases) {
		w := newIdemWorld(t, dbs)
		for name, keys := range map[string][]string{
			"empty":           {""},
			"256 characters":  {strings.Repeat("k", 256)},
			"a tab":           {"a\tb"},
			"a control byte":  {"a\x01b"},
			"DEL":             {"a\x7fb"},
			"non-ASCII":       {"café"},
			"two header rows": {"one", "two"},
		} {
			rec := w.call(w.token, http.MethodPost, "/api/v1/notes", `{"summary":"x"}`, "nik@phone", keys...)
			doc := decode(t, rec)
			if rec.Code != http.StatusBadRequest || doc["next"] == nil || doc["next"] == "" {
				t.Errorf("%s: %d %s, want 400 with a next", name, rec.Code, rec.Body)
			}
		}
		if w.count("notes") != 0 || w.count("idempotency_keys") != 0 {
			t.Errorf("a refused request wrote: notes %d, keys %d", w.count("notes"), w.count("idempotency_keys"))
		}
		for name, key := range map[string]string{"one character": "k", "255 characters": strings.Repeat("k", 255), "every printable": " !\"#~}"} {
			if rec := w.post("/api/v1/notes", `{"summary":"x"}`, "nik@phone", key); rec.Code != http.StatusCreated {
				t.Errorf("%s: %d %s, want 201", name, rec.Code, rec.Body)
			}
		}
	})
}

func TestRequestsWithoutTheHeaderOrOutsideItsScopeBehaveAsBefore(t *testing.T) {
	t.Parallel()
	w := newIdemWorld(t, sqliteDatabases{})
	for i := 0; i < 2; i++ {
		if rec := w.call(w.token, http.MethodPost, "/api/v1/notes", `{"summary":"same"}`, "nik@phone"); rec.Code != http.StatusCreated || rec.Header().Get(ReplayedHeader) != "" {
			t.Fatalf("without a key = %d %s", rec.Code, rec.Body)
		}
	}
	if w.count("notes") != 2 || w.count("idempotency_keys") != 0 {
		t.Errorf("notes %d, keys %d, want 2 and 0", w.count("notes"), w.count("idempotency_keys"))
	}
	// A read, and a write that is not a POST, ignore the header, even an invalid one.
	if rec := w.call(w.token, http.MethodGet, "/api/v1/notes", "", "nik@phone", "bad\tkey"); rec.Code != http.StatusOK {
		t.Errorf("GET with an invalid key = %d %s, want 200", rec.Code, rec.Body)
	}
	w.must("/api/v1/initiatives", `{"slug":"aa","title":"A"}`, "claude@host")
	for i := 0; i < 2; i++ {
		if rec := w.call(w.token, http.MethodPatch, "/api/v1/initiatives/aa", `{"title":"B"}`, "nik@phone", "same"); rec.Code != http.StatusOK || rec.Header().Get(ReplayedHeader) != "" {
			t.Errorf("PATCH with a key = %d %s (replay %q), want an ordinary 200", rec.Code, rec.Body, rec.Header().Get(ReplayedHeader))
		}
	}
	if w.count("idempotency_keys") != 0 {
		t.Errorf("keys = %d, want 0", w.count("idempotency_keys"))
	}
}

func TestAnAnswerOf4xxIsReplayedAndTheCreatedStateIsNotWrittenTwice(t *testing.T) {
	t.Parallel()
	w := newIdemWorld(t, sqliteDatabases{})
	first := w.post("/api/v1/tasks/aa-9/done", "", "claude@host", "missing")
	w.must("/api/v1/initiatives", `{"slug":"aa","title":"A"}`, "claude@host")
	for i := 0; i < 9; i++ {
		w.newTask("t")
	}
	second := w.post("/api/v1/tasks/aa-9/done", "", "claude@host", "missing")
	if first.Code != http.StatusNotFound || second.Code != http.StatusNotFound || second.Header().Get(ReplayedHeader) != "true" || second.Body.String() != first.Body.String() {
		t.Errorf("first %d %s, replay %d %s (%q), want the same 404", first.Code, first.Body, second.Code, second.Body, second.Header().Get(ReplayedHeader))
	}
}

// who is the identity authentication gives a request.
var who = auth.Identity{Principal: "key:caller", Actor: "claude@host"}

// directRequest is a POST of body to /api/v1/notes with the key and the identity who,
// for driving idempotent without authentication in front.
func directRequest(body io.Reader, key string) *http.Request {
	r := httptest.NewRequest(http.MethodPost, "/api/v1/notes", body)
	r.Header.Set(IdempotencyKeyHeader, key)
	return r.WithContext(withIdentity(r.Context(), who))
}

func newDirect(t *testing.T) (*idemWorld, *idempotency.Store) {
	t.Helper()
	w := newIdemWorld(t, sqliteDatabases{})
	return w, &idempotency.Store{Store: w.st, Now: w.clock.Now}
}

func TestADuplicateThatArrivesWhileTheFirstRunsIs409WithANext(t *testing.T) {
	t.Parallel()
	_, st := newDirect(t)
	entered, release := make(chan struct{}), make(chan struct{})
	var calls atomic.Int32
	h := idempotent(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls.Add(1)
		close(entered)
		<-release
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{"ok":true}`))
	}), st)

	firstDone := make(chan *httptest.ResponseRecorder)
	go func() {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, directRequest(strings.NewReader("{}"), "k"))
		firstDone <- rec
	}()
	<-entered
	dup := httptest.NewRecorder()
	h.ServeHTTP(dup, directRequest(strings.NewReader("{}"), "k"))
	var doc map[string]any
	_ = json.Unmarshal(dup.Body.Bytes(), &doc)
	if dup.Code != http.StatusConflict || doc["next"] == nil || doc["next"] == "" || dup.Header().Get(ReplayedHeader) != "" {
		t.Errorf("duplicate while running = %d %s, want 409 with a next", dup.Code, dup.Body)
	}
	close(release)
	first := <-firstDone
	if first.Code != http.StatusCreated || calls.Load() != 1 {
		t.Fatalf("first = %d %s after %d runs", first.Code, first.Body, calls.Load())
	}
	// The response was held back until the answer was stored: a retry sent on
	// receiving it is a replay, never a 409.
	again := httptest.NewRecorder()
	h.ServeHTTP(again, directRequest(strings.NewReader("{}"), "k"))
	if again.Code != http.StatusCreated || again.Header().Get(ReplayedHeader) != "true" || again.Body.String() != `{"ok":true}` || calls.Load() != 1 {
		t.Errorf("after the answer = %d %s (%q), %d runs, want a replay", again.Code, again.Body, again.Header().Get(ReplayedHeader), calls.Load())
	}
}

func TestAServerFailureReleasesTheKeySoTheRetryRunsAgain(t *testing.T) {
	t.Parallel()
	_, st := newDirect(t)
	var calls int
	statuses := []int{http.StatusInternalServerError, http.StatusServiceUnavailable, http.StatusBadGateway, http.StatusCreated}
	h := idempotent(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(statuses[calls])
		calls++
	}), st)
	for i, want := range statuses {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, directRequest(strings.NewReader("{}"), "k"))
		if rec.Code != want || rec.Header().Get(ReplayedHeader) != "" || calls != i+1 {
			t.Fatalf("send %d = %d (replay %q) after %d runs, want %d run afresh", i, rec.Code, rec.Header().Get(ReplayedHeader), calls, want)
		}
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, directRequest(strings.NewReader("{}"), "k"))
	if rec.Code != http.StatusCreated || rec.Header().Get(ReplayedHeader) != "true" || calls != len(statuses) {
		t.Errorf("after the 201 = %d (replay %q), %d runs, want its replay", rec.Code, rec.Header().Get(ReplayedHeader), calls)
	}
}

func TestAPanickingRequestReleasesTheKey(t *testing.T) {
	t.Parallel()
	w, st := newDirect(t)
	panicking := idempotent(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { panic("boom") }), st)
	func() {
		defer func() { _ = recover() }()
		panicking.ServeHTTP(httptest.NewRecorder(), directRequest(strings.NewReader("{}"), "k"))
	}()
	if w.count("idempotency_keys") != 0 {
		t.Errorf("keys = %d after a panic, want the claim released", w.count("idempotency_keys"))
	}
}

func TestTheBodyIsReadForTheFingerprintAndStillReachesTheHandler(t *testing.T) {
	t.Parallel()
	_, st := newDirect(t)
	var got string
	h := idempotent(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		got = string(b)
		w.WriteHeader(http.StatusOK)
	}), st)
	big := strings.Repeat("x", maxIdempotentBody)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, directRequest(strings.NewReader(big), "k"))
	if rec.Code != http.StatusOK || got != big {
		t.Fatalf("a body of exactly the limit = %d, handler read %d bytes, want 200 and all %d", rec.Code, len(got), len(big))
	}
	got = ""
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, directRequest(strings.NewReader(big+"x"), "k2"))
	var doc map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &doc)
	if rec.Code != http.StatusRequestEntityTooLarge || doc["next"] == nil || got != "" {
		t.Errorf("a body over the limit = %d %s, handler read %d bytes, want 413 with a next and no run", rec.Code, rec.Body, len(got))
	}
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, directRequest(iotest.ErrReader(io.ErrUnexpectedEOF), "k3"))
	if rec.Code != http.StatusBadRequest || got != "" {
		t.Errorf("an unreadable body = %d %s, want 400 and no run", rec.Code, rec.Body)
	}
}

func TestAKeyWithoutAStoreIs503AndAnUnauthenticatedRequestPassesThrough(t *testing.T) {
	t.Parallel()
	var runs int
	next := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { runs++; w.WriteHeader(http.StatusNoContent) })
	rec := httptest.NewRecorder()
	idempotent(next, nil).ServeHTTP(rec, directRequest(strings.NewReader("{}"), "k"))
	var doc map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &doc)
	if rec.Code != http.StatusServiceUnavailable || doc["next"] == nil || runs != 0 {
		t.Errorf("a key without a store = %d %s after %d runs, want 503 with a next and no run", rec.Code, rec.Body, runs)
	}
	// No identity: authentication is not in front, so the request is not ours.
	r := httptest.NewRequest(http.MethodPost, "/api/v1/notes", bytes.NewReader(nil))
	r.Header.Set(IdempotencyKeyHeader, "k")
	rec = httptest.NewRecorder()
	idempotent(next, nil).ServeHTTP(rec, r)
	if rec.Code != http.StatusNoContent || runs != 1 {
		t.Errorf("no identity = %d after %d runs, want the request passed on", rec.Code, runs)
	}
}

func TestOnlyAPostUnderAPIV1IsInScope(t *testing.T) {
	t.Parallel()
	for _, c := range []struct {
		method, path string
		want         bool
	}{
		{http.MethodPost, "/api/v1/notes", true},
		{http.MethodPost, "/api/v1/", true},
		{http.MethodPost, "/api/v1", false},
		{http.MethodPost, "/auth/logout", false},
		{http.MethodPost, "/api/v2/notes", false},
		{http.MethodPut, "/api/v1/cursors/notes", false},
		{http.MethodPatch, "/api/v1/tasks/aa-1", false},
		{http.MethodGet, "/api/v1/notes", false},
	} {
		if got := writesUnderAPI(httptest.NewRequest(c.method, c.path, nil)); got != c.want {
			t.Errorf("%s %s in scope = %v, want %v", c.method, c.path, got, c.want)
		}
	}
}

// failAnswers makes every write of a replay answer (the update that marks a key
// done) fail in the database itself, on either leg, and returns the function
// that lifts the fault.
func failAnswers(t *testing.T, w *idemWorld) (lift func()) {
	t.Helper()
	var set, unset []string
	if w.st.Dialect().Name() == "postgres" {
		set = []string{
			`CREATE FUNCTION idem_fail() RETURNS trigger AS $$ BEGIN RAISE EXCEPTION 'injected answer failure'; END $$ LANGUAGE plpgsql`,
			`CREATE TRIGGER idem_fail BEFORE UPDATE ON idempotency_keys FOR EACH ROW WHEN (NEW.state = 'done') EXECUTE FUNCTION idem_fail()`,
		}
		unset = []string{`DROP TRIGGER idem_fail ON idempotency_keys`, `DROP FUNCTION idem_fail()`}
	} else {
		set = []string{`CREATE TRIGGER idem_fail BEFORE UPDATE ON idempotency_keys WHEN NEW.state = 'done' BEGIN SELECT RAISE(ABORT, 'injected answer failure'); END`}
		unset = []string{`DROP TRIGGER idem_fail`}
	}
	run := func(stmts []string) {
		for _, q := range stmts {
			if _, err := w.st.Writer().ExecContext(context.Background(), q); err != nil {
				t.Fatalf("%s: %v", q, err)
			}
		}
	}
	run(set)
	return func() { run(unset) }
}

func TestAnAnswerThatCannotBeStoredNeverAdmitsASecondRunOfACommittedWrite(t *testing.T) {
	t.Parallel()
	forEachLeg(t, func(t *testing.T, _ string, dbs databases) {
		w := newIdemWorld(t, dbs)
		w.must("/api/v1/initiatives", `{"slug":"aa","title":"A"}`, "claude@host")
		lift := failAnswers(t, w)
		path, body := "/api/v1/tasks", `{"initiative":"aa","title":"once"}`
		tasks, events := w.count("tasks"), w.count("events")
		first := w.post(path, body, "claude@host", "k1")
		if first.Code != http.StatusServiceUnavailable || first.Header().Get(ReplayedHeader) != "" || strings.Contains(first.Body.String(), `"ref"`) {
			t.Fatalf("send with an answer that cannot be stored = %d %s, want a 503 that is not the success", first.Code, first.Body)
		}
		if doc := decode(t, first); doc["next"] == nil || doc["next"] == "" {
			t.Errorf("the 503 has no next: %s", first.Body)
		}
		if w.count("tasks") != tasks+1 || w.count("events") != events+1 {
			t.Fatalf("the first send wrote %d tasks and %d events, want 1 and 1", w.count("tasks")-tasks, w.count("events")-events)
		}
		// Retries with the key, while the fault lasts and after it is lifted,
		// are never a new run: 409, and no further row of any kind.
		for step, fix := range []func(){func() {}, lift} {
			fix()
			for i := 0; i < 2; i++ {
				retry := w.post(path, body, "claude@host", "k1")
				if retry.Code != http.StatusConflict || retry.Header().Get(ReplayedHeader) != "" {
					t.Errorf("retry %d of step %d = %d %s, want 409", i, step, retry.Code, retry.Body)
				}
			}
		}
		if w.count("tasks") != tasks+1 || w.count("events") != events+1 {
			t.Errorf("retries wrote %d tasks and %d events in all, want 1 and 1", w.count("tasks")-tasks, w.count("events")-events)
		}
		// Another principal's use of the key, and another key, are new requests.
		other := w.call(w.otherToken, http.MethodPost, path, body, "claude@host", "k1")
		fresh := w.post(path, body, "claude@host", "k2")
		if other.Code != http.StatusCreated || fresh.Code != http.StatusCreated || w.count("tasks") != tasks+3 {
			t.Errorf("other principal %d, new key %d, tasks +%d, want 201, 201 and +3", other.Code, fresh.Code, w.count("tasks")-tasks)
		}
		// With the fault lifted ordinary replay still works for the new key.
		again := w.post(path, body, "claude@host", "k2")
		if again.Code != http.StatusCreated || again.Header().Get(ReplayedHeader) != "true" || again.Body.String() != fresh.Body.String() || w.count("tasks") != tasks+3 {
			t.Errorf("replay after the fault = %d %s (%q), want %d %s replayed", again.Code, again.Body, again.Header().Get(ReplayedHeader), fresh.Code, fresh.Body)
		}
	})
}

func TestAnAnswerThatCannotBeStoredHoldsTheKeyForEveryKindOfSuccess(t *testing.T) {
	t.Parallel()
	forEachLeg(t, func(t *testing.T, _ string, dbs databases) {
		w := newIdemWorld(t, dbs)
		w.must("/api/v1/initiatives", `{"slug":"aa","title":"A"}`, "claude@host")
		ref := w.newTask("to close")
		lift := failAnswers(t, w)
		cases := []struct{ name, path, body string }{
			{"note", "/api/v1/notes", fmt.Sprintf(`{"summary":"hello about %s"}`, ref)},
			{"done", "/api/v1/tasks/" + ref + "/done", ""},
		}
		for i, c := range cases {
			key := fmt.Sprintf("held-%d", i)
			events := w.count("events")
			first := w.post(c.path, c.body, "claude@host", key)
			wrote := w.count("events") - events
			retry := w.post(c.path, c.body, "claude@host", key)
			if first.Code != http.StatusServiceUnavailable || wrote == 0 {
				t.Errorf("%s: first = %d %s with %d events, want a 503 after the write committed", c.name, first.Code, first.Body, wrote)
			}
			if retry.Code != http.StatusConflict || w.count("events")-events != wrote {
				t.Errorf("%s: retry = %d %s, events +%d, want 409 and +%d", c.name, retry.Code, retry.Body, w.count("events")-events, wrote)
			}
		}
		lift()
	})
}

func TestARefusalThatCannotBeStoredWroteNothingSoItsKeyIsReleased(t *testing.T) {
	t.Parallel()
	forEachLeg(t, func(t *testing.T, _ string, dbs databases) {
		w := newIdemWorld(t, dbs)
		lift := failAnswers(t, w)
		first := w.post("/api/v1/tasks/aa-9/done", "", "claude@host", "gone")
		if first.Code != http.StatusNotFound || first.Header().Get(ReplayedHeader) != "" || w.count("idempotency_keys") != 0 {
			t.Fatalf("refusal with the fault = %d %s, %d keys, want the 404 and the key released", first.Code, first.Body, w.count("idempotency_keys"))
		}
		lift()
		w.must("/api/v1/initiatives", `{"slug":"aa","title":"A"}`, "claude@host")
		for i := 0; i < 9; i++ {
			w.newTask("t")
		}
		retry := w.post("/api/v1/tasks/aa-9/done", "", "claude@host", "gone")
		replay := w.post("/api/v1/tasks/aa-9/done", "", "claude@host", "gone")
		if retry.Code != http.StatusOK || replay.Code != http.StatusOK || replay.Header().Get(ReplayedHeader) != "true" || replay.Body.String() != retry.Body.String() {
			t.Errorf("retry = %d %s, replay = %d %s (%q), want the run and then its replay", retry.Code, retry.Body, replay.Code, replay.Body, replay.Header().Get(ReplayedHeader))
		}
	})
}

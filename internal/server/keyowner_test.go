package server

import (
	"context"
	"net/http"
	"testing"
)

// ownedServer is a key server with an operator row, alice-op, and a second key,
// "owned", that she owns.
func ownedServer(t *testing.T) (*keyServer, string) {
	t.Helper()
	s := newKeyServer(t)
	if _, err := s.st.Writer().Exec(`INSERT INTO actors (id, handle, kind, first_seen_at, last_seen_at) VALUES ('OP1', 'alice-op', 'operator', 1, 1)`); err != nil {
		t.Fatal(err)
	}
	token, _, err := s.keys.CreateOwned(context.Background(), "owned", "admin-cli", "alice-op", 0)
	if err != nil {
		t.Fatal(err)
	}
	return s, token
}

func TestAKeyWriteCarriesTheKeysOwnerAsItsOriginAndTheAgentTakesIt(t *testing.T) {
	t.Parallel()
	s, owned := ownedServer(t)
	call := func(token, method, path, body, actor string) map[string]any {
		t.Helper()
		saved := s.token
		s.token = token
		defer func() { s.token = saved }()
		rec := s.send(method, path, body, actor)
		if rec.Code >= 300 {
			t.Fatalf("%s %s = %d %s", method, path, rec.Code, rec.Body)
		}
		return decode(t, rec)
	}
	call(owned, http.MethodPost, "/api/v1/initiatives", `{"slug":"mine","title":"Mine"}`, "claude@host")
	call(s.token, http.MethodPost, "/api/v1/initiatives", `{"slug":"theirs","title":"Theirs"}`, "codex@host")
	evs := call(s.token, http.MethodGet, "/api/v1/events?type=initiative_created", "", "reader")["items"].([]any)
	got := map[string]any{}
	for _, e := range evs {
		m := e.(map[string]any)
		got[m["initiative"].(string)] = m["origin"]
	}
	if len(got) != 2 || got["mine"] != "alice-op" || got["theirs"] != nil {
		t.Errorf("origins by initiative = %v, want mine from alice-op and theirs with none", got)
	}
	if actor := call(s.token, http.MethodGet, "/api/v1/actors/claude@host", "", "reader"); actor["origin"] != "alice-op" {
		t.Errorf("actor = %v, want origin alice-op", actor)
	}
	if actor := call(s.token, http.MethodGet, "/api/v1/actors/codex@host", "", "reader"); actor["origin"] != nil {
		t.Errorf("actor of an ownerless key = %v, want no origin", actor)
	}
	// A key made through the owned key is owned by alice-op, and says so on the
	// 201 and in the list; one made through the ownerless key has no owner.
	if made := call(owned, http.MethodPost, "/api/v1/keys", `{"name":"child"}`, "claude@host"); made["owner"] != "alice-op" {
		t.Errorf("created = %v, want owner alice-op", made)
	}
	if made := call(s.token, http.MethodPost, "/api/v1/keys", `{"name":"orphan"}`, "codex@host"); made["owner"] != nil {
		t.Errorf("created through an ownerless key = %v, want no owner", made)
	}
	owners := map[string]any{}
	for _, k := range call(s.token, http.MethodGet, "/api/v1/keys", "", "reader")["items"].([]any) {
		m := k.(map[string]any)
		owners[m["name"].(string)] = m["owner"]
	}
	want := map[string]any{"caller": nil, "owned": "alice-op", "child": "alice-op", "orphan": nil}
	for name, owner := range want {
		if owners[name] != owner {
			t.Errorf("listed owner of %s = %v, want %v (all: %v)", name, owners[name], owner, owners)
		}
	}
}

package server

import (
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/oapi-codegen/runtime"
)

func (s *keyServer) create(slug, extra string) *httptest.ResponseRecorder {
	s.t.Helper()
	return s.send(http.MethodPost, "/api/v1/initiatives", fmt.Sprintf(`{"slug":%q,"title":"Title of %s"%s}`, slug, slug, extra), "claude@host")
}

func TestCreateInitiativeReturnsItAndAppendsAnEventWithPrincipalAndActor(t *testing.T) {
	s := newKeyServer(t)
	rec := s.create("phone-onboard", `,"description":"# Plan","tags":["Work"," homelab","work"],"target_start":"2026-03-01","target_end":"2026-04-01"`)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create = %d %s", rec.Code, rec.Body)
	}
	doc := decode(t, rec)
	for k, want := range map[string]any{
		"slug": "phone-onboard", "ref": "phone-onboard", "title": "Title of phone-onboard", "description": "# Plan",
		"status": "open", "created_by": "claude@host", "target_start": "2026-03-01", "target_end": "2026-04-01",
	} {
		if doc[k] != want {
			t.Errorf("%s = %v, want %v", k, doc[k], want)
		}
	}
	if fmt.Sprint(doc["tags"]) != "[homelab work]" || len(fmt.Sprint(doc["id"])) != 26 {
		t.Errorf("tags %v id %v", doc["tags"], doc["id"])
	}
	if _, has := doc["closed_at"]; has {
		t.Error("an open initiative carries closed_at")
	}
	ev := s.events()
	if len(ev) != 1 || ev[0].typ != "initiative_created" || ev[0].principal != "key:caller" || ev[0].actor != "claude@host" || ev[0].ref != doc["id"] {
		t.Fatalf("events = %+v", ev)
	}
}

func TestCreateInitiativeRefusesBadFieldsWithAProblemAndNext(t *testing.T) {
	s := newKeyServer(t)
	cases := map[string]string{
		"x":                     "",
		"Has-Upper":             "",
		strings.Repeat("a", 41): "",
		"ok-slug":               `,"tags":["no way"]`,
		"ok-slug2":              `,"target_start":"2026-04-01","target_end":"2026-03-01"`,
		"ok-slug3":              `,"description":"` + strings.Repeat("x", 20001) + `"`,
	}
	for slug, extra := range cases {
		rec := s.create(slug, extra)
		doc := decode(t, rec)
		if rec.Code != http.StatusBadRequest || rec.Header().Get("Content-Type") != "application/problem+json" || doc["next"] == "" || doc["next"] == nil || doc["detail"] == nil {
			t.Errorf("%q%s: %d %v", slug, extra[:min(len(extra), 30)], rec.Code, doc)
		}
	}
	if rec := s.send(http.MethodPost, "/api/v1/initiatives", `{"slug":"abc","title":"  "}`, "a"); rec.Code != 400 {
		t.Errorf("blank title = %d", rec.Code)
	}
	if rec := s.send(http.MethodPost, "/api/v1/initiatives", `null`, "a"); rec.Code != 400 {
		t.Errorf("null body = %d", rec.Code)
	}
	if rec := s.send(http.MethodPost, "/api/v1/initiatives", `{"slug":"abc","title":"t","target_start":"soon"}`, "a"); rec.Code != 400 {
		t.Errorf("bad date = %d", rec.Code)
	}
	if n := len(s.events()); n != 0 {
		t.Errorf("refused creations left %d events", n)
	}
}

func TestCreateInitiativeDuplicateIsConflictWithNext(t *testing.T) {
	s := newKeyServer(t)
	s.create("phone-onboard", "")
	rec := s.create("phone-onboard", "")
	doc := decode(t, rec)
	if rec.Code != http.StatusConflict || !strings.Contains(fmt.Sprint(doc["next"]), "GET /api/v1/initiatives/phone-onboard") {
		t.Fatalf("duplicate = %d %v", rec.Code, doc)
	}
	if n := len(s.events()); n != 1 {
		t.Errorf("events = %d, want 1", n)
	}
}

func TestGetInitiativeBySlugAndUlidAndNotFound(t *testing.T) {
	s := newKeyServer(t)
	id := decode(t, s.create("phone-onboard", `,"tags":["work"]`))["id"].(string)
	for _, ref := range []string{"phone-onboard", id} {
		rec := s.send(http.MethodGet, "/api/v1/initiatives/"+ref, "", "a")
		doc := decode(t, rec)
		if rec.Code != 200 || doc["id"] != id || fmt.Sprint(doc["tags"]) != "[work]" {
			t.Errorf("GET %s = %d %v", ref, rec.Code, doc)
		}
	}
	rec := s.send(http.MethodGet, "/api/v1/initiatives/nope", "", "a")
	if doc := decode(t, rec); rec.Code != 404 || !strings.Contains(fmt.Sprint(doc["next"]), "GET /api/v1/initiatives") {
		t.Errorf("missing = %d %v", rec.Code, doc)
	}
}

func TestListInitiativesPagesFiltersAndRefusesBadParameters(t *testing.T) {
	s := newKeyServer(t)
	s.create("aa-one", `,"tags":["work","x"]`)
	s.create("bb-two", `,"tags":["work"]`)
	s.create("cc-three", `,"tags":["home"]`)
	rec := s.send(http.MethodGet, "/api/v1/initiatives?limit=2", "", "a")
	doc := decode(t, rec)
	items := doc["items"].([]any)
	if rec.Code != 200 || len(items) != 2 || doc["next_after"] != items[1].(map[string]any)["id"] {
		t.Fatalf("page 1 = %d %v", rec.Code, doc)
	}
	rec = s.send(http.MethodGet, "/api/v1/initiatives?limit=2&after="+doc["next_after"].(string), "", "a")
	doc = decode(t, rec)
	items = doc["items"].([]any)
	if len(items) != 1 || items[0].(map[string]any)["slug"] != "cc-three" || doc["next_after"] != nil {
		t.Fatalf("page 2 = %v", doc)
	}
	rec = s.send(http.MethodGet, "/api/v1/initiatives?after=aa-one&tag=work", "", "a")
	if items := decode(t, rec)["items"].([]any); len(items) != 1 || items[0].(map[string]any)["slug"] != "bb-two" {
		t.Errorf("after slug + tag = %v", items)
	}
	rec = s.send(http.MethodGet, "/api/v1/initiatives?tag=work&tag=x", "", "a")
	if items := decode(t, rec)["items"].([]any); len(items) != 1 {
		t.Errorf("two tags = %v", items)
	}
	for _, q := range []string{"limit=abc", "limit=0", "limit=1001", "limit=-3", "after=Bad!", "after=zz-nope", "tag=no%20way"} {
		rec := s.send(http.MethodGet, "/api/v1/initiatives?"+q, "", "a")
		doc := decode(t, rec)
		if rec.Code != 400 || rec.Header().Get("Content-Type") != "application/problem+json" || doc["next"] == nil {
			t.Errorf("?%s = %d %v", q, rec.Code, doc)
		}
	}
}

func TestInitiativesAreUnavailableWithoutAStore(t *testing.T) {
	h := NewHandler(ok, acceptKey, nil, nil)
	for _, c := range []struct{ method, path string }{{"GET", "/api/v1/initiatives"}, {"POST", "/api/v1/initiatives"}, {"GET", "/api/v1/initiatives/abc"}} {
		r := httptest.NewRequest(c.method, c.path, strings.NewReader(`{"slug":"abc","title":"t"}`))
		r.Header.Set("Authorization", "Bearer "+testKey)
		r.Header.Set(ActorHeader, "tester")
		r.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, r)
		if rec.Code != http.StatusServiceUnavailable {
			t.Errorf("%s %s = %d", c.method, c.path, rec.Code)
		}
	}
}

func TestInitiativeStatusEnumAcceptsOnlyTheDeclaredStates(t *testing.T) {
	if !Open.Valid() || !Closed.Valid() || InitiativeStatus("gone").Valid() {
		t.Error("InitiativeStatus.Valid is wrong")
	}
}

func TestProblemDocumentsCarryTheDefaultNextOfTheirStatus(t *testing.T) {
	for status, want := range map[int]string{401: "Bearer", 404: "Check the path", 409: "Read what exists", 503: "Try again", 400: "Fix the request"} {
		rec := httptest.NewRecorder()
		writeProblem(rec, status, "d")
		if got := decode(t, rec)["next"]; !strings.Contains(fmt.Sprint(got), want) {
			t.Errorf("%d: next = %v, want it to contain %q", status, got, want)
		}
	}
}

func TestBindFailedAnswersRequiredAndInvalidParameters(t *testing.T) {
	var seen []error
	siw := &ServerInterfaceWrapper{ErrorHandlerFunc: func(_ http.ResponseWriter, _ *http.Request, err error) { seen = append(seen, err) }}
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	siw.bindFailed(nil, r, "limit", &runtime.RequiredParameterError{ParamName: "limit"})
	siw.bindFailed(nil, r, "limit", errors.New("not a number"))
	var required *RequiredParamError
	var invalid *InvalidParamFormatError
	if len(seen) != 2 || !errors.As(seen[0], &required) || required.ParamName != "limit" || !errors.As(seen[1], &invalid) || invalid.ParamName != "limit" {
		t.Errorf("answers = %#v", seen)
	}
}

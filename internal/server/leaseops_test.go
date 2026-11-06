package server

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/n-orlov/board/internal/domain"
	"github.com/n-orlov/board/internal/leases"
)

func TestLeaseOperationsAnswer503WithoutAStore(t *testing.T) {
	ctx := context.Background()
	start, _ := leaseOps{}.StartLease(ctx, StartLeaseRequestObject{})
	list, _ := leaseOps{}.ListLeases(ctx, ListLeasesRequestObject{})
	get, _ := leaseOps{}.GetLease(ctx, GetLeaseRequestObject{Ref: "L1"})
	renew, _ := leaseOps{}.RenewLease(ctx, RenewLeaseRequestObject{Ref: "L1"})
	end, _ := leaseOps{}.EndLease(ctx, EndLeaseRequestObject{Ref: "L1"})
	ack, _ := leaseOps{}.AckLease(ctx, AckLeaseRequestObject{Ref: "L1"})
	for name, got := range map[string]any{"start": start, "list": list, "get": get, "renew": renew, "end": end, "ack": ack} {
		if p, ok := got.(problemResponse); !ok || p.status != http.StatusServiceUnavailable {
			t.Errorf("%s without a store = %#v, want a 503 problem", name, got)
		}
	}
}

func TestLeaseWritesWithoutAPrincipalAre401(t *testing.T) {
	o := leaseOps{leases: &leases.Store{}}
	got, _ := o.StartLease(context.Background(), StartLeaseRequestObject{Body: &LeaseStart{Task: "aa-1", Intent: "x"}})
	if p, ok := got.(problemResponse); !ok || p.status != http.StatusUnauthorized {
		t.Errorf("start without a principal = %#v, want 401", got)
	}
}

func TestLeaseFailureMapsEachErrorToItsStatus(t *testing.T) {
	at := time.Date(2026, 11, 5, 12, 0, 0, 123000000, time.UTC)
	for name, tc := range map[string]struct {
		err    error
		status int
	}{
		"invalid":  {leases.InvalidError{Msg: "bad", Next: "fix it"}, http.StatusBadRequest},
		"gone":     {leases.GoneError{Ref: "L1", ExpiredAt: at}, http.StatusGone},
		"conflict": {leases.ConflictError{Msg: "no", Next: "later"}, http.StatusConflict},
		"missing":  {errors.Join(leases.ErrNotFound), http.StatusNotFound},
	} {
		p, ok := leaseFailure(tc.err)
		if !ok || p.status != tc.status {
			t.Errorf("%s = %+v, %v; want status %d", name, p, ok, tc.status)
		}
	}
	if _, ok := leaseFailure(errors.New("disk")); ok {
		t.Error("a server error was mapped to a problem")
	}
	p, _ := leaseFailure(leases.GoneError{Ref: "L1", ExpiredAt: at})
	if p.extra["expired_at"] != "2026-11-05T12:00:00.123Z" {
		t.Errorf("expired_at = %v", p.extra["expired_at"])
	}
}

func TestLeaseResultKeepsAServerErrorAnError(t *testing.T) {
	if _, err := leaseResult[GetLeaseResponseObject](errors.New("disk")); err == nil {
		t.Error("a server error was swallowed")
	}
	if r, err := leaseResult[GetLeaseResponseObject](leases.ConflictError{Msg: "x"}); err != nil || r == nil {
		t.Errorf("a conflict = %v, %v; want a problem response", r, err)
	}
}

func TestAConflictWithNoResourcesIsAnEmptyList(t *testing.T) {
	c := apiConflict(domain.Holding{Lease: "L1", Holder: "claude@host", Task: "aa-1", Intent: "x"})
	raw, _ := json.Marshal(c)
	if !strings.Contains(string(raw), `"resources":[]`) {
		t.Errorf("conflict = %s", raw)
	}
}

func TestLeaseRoundTripThroughTheHandler(t *testing.T) {
	s := newKeyServer(t)
	s.create("aa", "")
	s.task(`{"initiative":"aa","title":"one"}`)
	rec := s.send(http.MethodPost, "/api/v1/leases", `{"task":"aa-1","intent":"work","resources":["db:x"]}`, "claude@host")
	doc := decode(t, rec)
	if rec.Code != http.StatusCreated || doc["conflicts"] == nil || doc["lease"].(map[string]any)["ref"] != "L1" {
		t.Fatalf("start = %d %s", rec.Code, rec.Body)
	}
	if rec := s.send(http.MethodPost, "/api/v1/leases", `{"task":"aa-1","intent":"again"}`, "claude@host"); rec.Code != http.StatusOK {
		t.Errorf("second start = %d %s, want 200", rec.Code, rec.Body)
	}
	for path, want := range map[string]int{
		"/api/v1/leases/L1":            http.StatusOK,
		"/api/v1/leases/L9":            http.StatusNotFound,
		"/api/v1/leases?state=bad":     http.StatusBadRequest,
		"/api/v1/leases?task=aa-9":     http.StatusNotFound,
		"/api/v1/leases?limit=0":       http.StatusBadRequest,
		"/api/v1/leases?resource=db:x": http.StatusOK,
	} {
		if rec := s.send(http.MethodGet, path, "", "claude@host"); rec.Code != want {
			t.Errorf("GET %s = %d %s, want %d", path, rec.Code, rec.Body, want)
		}
	}
	if rec := s.send(http.MethodPost, "/api/v1/leases/L1/end", `{"outcome":"done"}`, "codex@host"); rec.Code != http.StatusOK ||
		decode(t, rec)["ended_by"] != "codex@host" {
		t.Errorf("end = %d %s", rec.Code, rec.Body)
	}
}

func TestAListParameterSentTwiceIsRefused(t *testing.T) {
	s := newKeyServer(t)
	for _, p := range []string{"state", "actor", "task", "resource", "limit", "after"} {
		if rec := s.send(http.MethodGet, "/api/v1/leases?"+p+"=1&"+p+"=2", "", "claude@host"); rec.Code != http.StatusBadRequest {
			t.Errorf("%s sent twice = %d %s, want 400", p, rec.Code, rec.Body)
		}
	}
}

func TestQueryBinderAnswersOnlyTheFirstFailure(t *testing.T) {
	var seen []error
	siw := &ServerInterfaceWrapper{ErrorHandlerFunc: func(_ http.ResponseWriter, _ *http.Request, err error) { seen = append(seen, err) }}
	b := siw.queryBinder(nil, httptest.NewRequest(http.MethodGet, "/", nil))
	b.check("a", nil)
	if b.failed || len(seen) != 0 {
		t.Fatalf("a bound parameter failed the request: %v", seen)
	}
	b.check("limit", errors.New("not a number"))
	b.check("after", errors.New("second"))
	var invalid *InvalidParamFormatError
	if !b.failed || len(seen) != 1 || !errors.As(seen[0], &invalid) || invalid.ParamName != "limit" {
		t.Errorf("answers = %#v, want only the limit failure", seen)
	}
}

func TestRenewAndEndHandlersReadTheirOptionalBodies(t *testing.T) {
	s := newKeyServer(t)
	s.create("aa", "")
	s.task(`{"initiative":"aa","title":"one"}`)
	s.send(http.MethodPost, "/api/v1/leases", `{"task":"aa-1","intent":"work"}`, "claude@host")
	for _, tc := range []struct {
		path, body string
		want       int
	}{
		{"/api/v1/leases/L1/renew", `{"ttl_seconds":3600,"intent":"more","resources":["r"]}`, http.StatusOK},
		{"/api/v1/leases/L1/renew", ``, http.StatusOK},
		{"/api/v1/leases/L1/renew", `{"intent":""}`, http.StatusBadRequest},
		{"/api/v1/leases/L1/renew", `{broken`, http.StatusBadRequest},
		{"/api/v1/leases/L1/end", `{broken`, http.StatusBadRequest},
		{"/api/v1/leases", `{broken`, http.StatusBadRequest},
		{"/api/v1/leases/L1/end", ``, http.StatusOK},
		{"/api/v1/leases/L1/renew", ``, http.StatusConflict},
	} {
		if rec := s.send(http.MethodPost, tc.path, tc.body, "claude@host"); rec.Code != tc.want {
			t.Errorf("POST %s %q = %d %s, want %d", tc.path, tc.body, rec.Code, rec.Body, tc.want)
		}
	}
}

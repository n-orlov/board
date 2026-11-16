package server

import (
	"context"
	"errors"
	"net/http"
	"testing"
	"time"

	"github.com/n-orlov/village-hall/internal/asks"
)

func TestAskOperationsAnswer503WithoutAStore(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	raise, _ := askOps{}.RaiseAsk(ctx, RaiseAskRequestObject{})
	list, _ := askOps{}.ListAsks(ctx, ListAsksRequestObject{})
	get, _ := askOps{}.GetAsk(ctx, GetAskRequestObject{Ref: "A1"})
	answer, _ := askOps{}.AnswerAsk(ctx, AnswerAskRequestObject{Ref: "A1"})
	withdraw, _ := askOps{}.WithdrawAsk(ctx, WithdrawAskRequestObject{Ref: "A1"})
	for name, got := range map[string]any{"raise": raise, "list": list, "get": get, "answer": answer, "withdraw": withdraw} {
		if p, ok := got.(problemResponse); !ok || p.status != http.StatusServiceUnavailable {
			t.Errorf("%s without a store = %#v, want a 503 problem", name, got)
		}
	}
}

func TestAskWritesWithoutAPrincipalAre401(t *testing.T) {
	t.Parallel()
	o := askOps{asks: &asks.Store{}}
	ctx := context.Background()
	raise, _ := o.RaiseAsk(ctx, RaiseAskRequestObject{Body: &AskRaise{Question: "x"}})
	answer, _ := o.AnswerAsk(ctx, AnswerAskRequestObject{Ref: "A1", Body: &AskAnswer{Answer: "x"}})
	withdraw, _ := o.WithdrawAsk(ctx, WithdrawAskRequestObject{Ref: "A1"})
	for name, got := range map[string]any{"raise": raise, "answer": answer, "withdraw": withdraw} {
		if p, ok := got.(problemResponse); !ok || p.status != http.StatusUnauthorized {
			t.Errorf("%s without a principal = %#v, want 401", name, got)
		}
	}
}

func TestAskFailureMapsEachErrorToItsStatus(t *testing.T) {
	t.Parallel()
	for name, tc := range map[string]struct {
		err    error
		status int
	}{
		"invalid":  {asks.InvalidError{Msg: "bad", Next: "fix it"}, http.StatusBadRequest},
		"conflict": {asks.ConflictError{Msg: "no", Next: "later"}, http.StatusConflict},
		"missing":  {errors.Join(asks.ErrNotFound), http.StatusNotFound},
	} {
		p, ok := askFailure(tc.err)
		if !ok || p.status != tc.status {
			t.Errorf("%s = %+v, %v; want status %d", name, p, ok, tc.status)
		}
	}
	if _, ok := askFailure(errors.New("disk")); ok {
		t.Error("a server error was mapped to a problem")
	}
	if _, err := askResult[GetAskResponseObject](errors.New("disk")); err == nil {
		t.Error("a server error was swallowed")
	}
	if r, err := askResult[GetAskResponseObject](asks.ConflictError{Msg: "x"}); err != nil || r == nil {
		t.Errorf("a conflict = %v, %v; want a problem response", r, err)
	}
}

func TestAskRequestCarriesEveryFieldAndDefaultsTheRest(t *testing.T) {
	t.Parallel()
	mode, blocking, options, details := AskRaiseMode("poll"), true, []string{"a", "b"}, "d"
	task, ini := "aa-1", "aa"
	got := askRequest(&AskRaise{Task: &task, Initiative: &ini, Question: "q", Details: &details, Options: &options, Mode: &mode, Blocking: &blocking})
	want := asks.New{Task: "aa-1", Initiative: "aa", Question: "q", Details: "d", Options: []string{"a", "b"}, Mode: "poll", Blocking: true}
	if !equalNew(got, want) {
		t.Errorf("request = %+v, want %+v", got, want)
	}
	if bare := askRequest(&AskRaise{Question: "q"}); bare.Mode != "" || bare.Blocking || bare.Options != nil {
		t.Errorf("bare request = %+v, want the defaults left to the store", bare)
	}
}

func equalNew(a, b asks.New) bool {
	if len(a.Options) != len(b.Options) {
		return false
	}
	for i := range a.Options {
		if a.Options[i] != b.Options[i] {
			return false
		}
	}
	a.Options, b.Options = nil, nil
	return a.Task == b.Task && a.Initiative == b.Initiative && a.Question == b.Question && a.Details == b.Details &&
		a.Mode == b.Mode && a.Blocking == b.Blocking
}

func TestAnAskDocumentOmitsWhatIsUnsetAndListsNoOptionsAsEmpty(t *testing.T) {
	t.Parallel()
	at := time.Date(2026, 11, 9, 12, 0, 0, 0, time.UTC)
	o := askOps{asks: &asks.Store{Now: func() time.Time { return at }}}
	open := o.apiAsk(asks.Ask{ID: "I", Ref: "A1", Actor: "claude@host", Question: "q", State: "open", Mode: "poll", CreatedAt: at, PollSeenAt: &at})
	if open.Options == nil || len(open.Options) != 0 {
		t.Errorf("options = %#v, want an empty list", open.Options)
	}
	if open.Task != nil || open.Initiative != nil || open.Answer != nil || open.Channel != nil || open.WithdrawReason != nil {
		t.Errorf("unset fields are present: %+v", open)
	}
	if !open.AskerWaiting {
		t.Error("an open poll ask seen now is not waiting")
	}
	done := o.apiAsk(asks.Ask{State: "answered", Mode: "poll", Channel: "relay", Answer: "x", OnBehalfOf: "nik@phone", PollSeenAt: &at})
	if done.Channel == nil || *done.Channel != AskChannel("relay") || done.AskerWaiting || *done.OnBehalfOf != "nik@phone" {
		t.Errorf("answered ask = %+v", done)
	}
	if open.EndedAt != nil {
		t.Errorf("an open ask has ended_at %v", open.EndedAt)
	}
	if ended := o.apiAsk(asks.Ask{State: "withdrawn", Mode: "stop", EndedAt: &at}); ended.EndedAt == nil || !ended.EndedAt.Equal(at) {
		t.Errorf("ended_at = %v, want %s", ended.EndedAt, at)
	}
}

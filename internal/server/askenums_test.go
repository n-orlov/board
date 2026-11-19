package server

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestAskEnumsAcceptOnlyTheDeclaredValues(t *testing.T) {
	t.Parallel()
	valid := []interface{ Valid() bool }{
		AskChannelApi, AskChannelRelay, AskChannelUi,
		AskModePoll, AskModeStop,
		AskStateAnswered, AskStateOpen, AskStateWithdrawn,
		AskAnswerChannelApi, AskAnswerChannelRelay, AskAnswerChannelUi,
		AskRaiseModePoll, AskRaiseModeStop,
		AskRaiseLeaseActionNone, AskRaiseLeaseActionKeep, AskRaiseLeaseActionStop,
		AskRaisedChannelApi, AskRaisedChannelRelay, AskRaisedChannelUi, AskRaisedModePoll, AskRaisedModeStop,
		AskRaisedStateAnswered, AskRaisedStateOpen, AskRaisedStateWithdrawn,
		NotificationStatePending, NotificationStateSent, NotificationStateFailed,
		ListNotificationsParamsStatePending, ListNotificationsParamsStateSent, ListNotificationsParamsStateFailed,
		NotificationKindStranded, NotificationKindAsking, NotificationKindDone,
		TimelineTaskStatusOpen, TimelineTaskStatusDone, TimelineTaskStatusClosed, TimelineTaskKindWork, TimelineTaskKindWatch,
		TimelineLeaseStateLive, TimelineLeaseStateEnded, TimelineLeaseStateExpired,
		TagMarkSourceTask, TagMarkSourceInitiative, TagMarkSourceActor,
		Agent, Operator, ActorAskModePoll, ActorAskModeStop,
		TimelineMarkerTypeMilestone, TimelineMarkerTypeAsk, TimelineMarkerTypeCheck, TimelineMarkerTypeDone, TimelineMarkerTypeClosed,
	}
	for _, v := range valid {
		if !v.Valid() {
			t.Errorf("%v is declared but reported invalid", v)
		}
	}
	for _, v := range []interface{ Valid() bool }{
		AskChannel("x"), AskMode("x"), AskState("x"), AskAnswerChannel("x"), AskRaiseMode("x"), AskRaiseLeaseAction("x"), AskRaisedChannel("x"), AskRaisedMode("x"), AskRaisedState("x"), NotificationState("x"), NotificationKind("x"), ListNotificationsParamsState("x"),
		TimelineTaskStatus("x"), TimelineTaskKind("x"), TimelineLeaseState("x"), TimelineMarkerType("x"), ActorViewKind("x"), ActorAskMode("x"), TagMarkSource("x"),
	} {
		if v.Valid() {
			t.Errorf("%T: an undeclared value is valid", v)
		}
	}
}

// TestWithdrawAskReadsItsOptionalBody sends the withdraw operation a body that
// is not JSON, which is a 400, and an empty body, which is allowed.
func TestWithdrawAskReadsItsOptionalBody(t *testing.T) {
	t.Parallel()
	mux := http.NewServeMux()
	HandlerFromMux(strictOperations(stubOperations{mode: "fails"}, func(next StrictHandlerFunc, _ string) StrictHandlerFunc { return next }), mux)
	for body, want := range map[string]int{"{not json": http.StatusBadRequest, "": http.StatusInternalServerError} {
		req := httptest.NewRequest(http.MethodPost, "/api/v1/asks/A1/withdraw", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)
		if rec.Code != want {
			t.Errorf("withdraw with body %q = %d, want %d", body, rec.Code, want)
		}
	}
}

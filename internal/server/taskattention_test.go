package server

import (
	"net/http"
	"testing"

	"github.com/n-orlov/board/internal/asks"
	"github.com/n-orlov/board/internal/attention"
	"github.com/n-orlov/board/internal/auth"
	"github.com/n-orlov/board/internal/initiatives"
	"github.com/n-orlov/board/internal/leases"
	"github.com/n-orlov/board/internal/tasks"
)

// WithAttention and WithTasks give the task document its reasons in either order.
func TestTaskDocumentCarriesItsReasonsWhicheverOptionComesFirst(t *testing.T) {
	for name, order := range map[string]func(*keyServer) []Option{
		"attention first": func(s *keyServer) []Option {
			return []Option{WithAttention(&attention.Store{Store: s.st}), WithTasks(&tasks.Store{Store: s.st}, &leases.Store{Store: s.st}), WithAsks(&asks.Store{Store: s.st})}
		},
		"tasks first": func(s *keyServer) []Option {
			return []Option{WithTasks(&tasks.Store{Store: s.st}, &leases.Store{Store: s.st}), WithAsks(&asks.Store{Store: s.st}), WithAttention(&attention.Store{Store: s.st})}
		},
	} {
		t.Run(name, func(t *testing.T) {
			s := newKeyServer(t)
			s.h = NewHandler(ok, (&auth.Verifier{Store: s.st}).Verify, nil, s.keys,
				append([]Option{WithInitiatives(&initiatives.Store{Store: s.st})}, order(s)...)...)
			s.create("aa", "")
			s.task(`{"initiative":"aa","title":"one"}`)
			s.task(`{"initiative":"aa","title":"two"}`)
			if rec := s.send(http.MethodPost, "/api/v1/asks", `{"task":"aa-1","question":"Which?","blocking":true}`, "claude@host"); rec.Code != http.StatusCreated {
				t.Fatalf("raise = %d %s", rec.Code, rec.Body)
			}
			doc := decode(t, s.send(http.MethodGet, "/api/v1/tasks/aa-1", "", "claude@host"))
			list, _ := doc["attention"].([]any)
			if len(list) != 1 || list[0].(map[string]any)["reason"] != "asking" || list[0].(map[string]any)["blocking"] != true {
				t.Errorf("attention of aa-1 = %v", doc["attention"])
			}
			quiet := decode(t, s.send(http.MethodGet, "/api/v1/tasks/aa-2", "", "claude@host"))
			if empty, ok := quiet["attention"].([]any); !ok || len(empty) != 0 || quiet["stranded_lease"] != nil {
				t.Errorf("attention of aa-2 = %v", quiet["attention"])
			}
			only := decode(t, s.send(http.MethodGet, "/api/v1/tasks?attention=true", "", "claude@host"))
			items, _ := only["items"].([]any)
			if len(items) != 1 || items[0].(map[string]any)["ref"] != "aa-1" {
				t.Errorf("attention=true = %v", only)
			}
		})
	}
}

func TestApiTaskAttentionLeavesOutFlagsThatAreNotSet(t *testing.T) {
	got, lease := apiTaskAttention(attention.TaskReasons{Reasons: []attention.Reason{attention.ReasonAsking, attention.ReasonDone}})
	if len(got) != 2 || got[0].Blocking != nil || got[0].AskerWaiting != nil || got[1].Reason != TaskAttentionReasonDone || lease != nil {
		t.Errorf("reasons = %+v %v", got, lease)
	}
	got, _ = apiTaskAttention(attention.TaskReasons{Reasons: []attention.Reason{attention.ReasonAsking, attention.ReasonDone}, Blocking: true, AskerWaiting: true})
	if got[0].Blocking == nil || !*got[0].Blocking || got[0].AskerWaiting == nil || !*got[0].AskerWaiting || got[1].Blocking != nil {
		t.Errorf("flags = %+v", got)
	}
}

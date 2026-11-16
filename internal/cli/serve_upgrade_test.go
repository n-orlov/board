package cli

import (
	"context"
	"testing"

	"github.com/n-orlov/village-hall/internal/audit"
	"github.com/n-orlov/village-hall/internal/initiatives"
	"github.com/n-orlov/village-hall/internal/leases"
	"github.com/n-orlov/village-hall/internal/store"
	"github.com/n-orlov/village-hall/internal/tasks"
)

// TestServeReopensADoneTaskHoldingALiveLeaseOnStart runs serveUntil on a
// database holding a done task with a live lease and finds the task open, with
// one task_reopened event, after the first start and after a second.
func TestServeReopensADoneTaskHoldingALiveLeaseOnStart(t *testing.T) {
	ctx := context.Background()
	url := sqliteURL(t)
	st, err := store.Open(url)
	if err != nil {
		t.Fatal(err)
	}
	if err := st.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	who := audit.Actor{Handle: "claude@laptop"}
	if _, err := (initiatives.Store{Store: st}).Create(ctx, who, "key:x", initiatives.New{Slug: "aa", Title: "First"}); err != nil {
		t.Fatal(err)
	}
	task, err := (tasks.Store{Store: st}).Create(ctx, who, "key:x", tasks.New{Initiative: "aa", Title: "Held"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := (leases.Store{Store: st}).Start(ctx, who, "key:x", leases.Start{Task: task.Ref, Intent: "work"}); err != nil {
		t.Fatal(err)
	}
	if _, err := st.Writer().ExecContext(ctx, `UPDATE tasks SET status = 'done', done_at = updated_at, done_by = created_by`); err != nil {
		t.Fatal(err)
	}
	_ = st.Close()
	for start := 1; start <= 2; start++ {
		if code, errs := serveReady(t, url, true); code != ExitOK || errs != "" {
			t.Fatalf("start %d: code=%d err=%q", start, code, errs)
		}
		st, err := store.Open(url)
		if err != nil {
			t.Fatal(err)
		}
		got, err := (tasks.Store{Store: st}).Get(ctx, task.Ref)
		var events int
		if err == nil {
			err = st.Reader().QueryRowContext(ctx, `SELECT count(*) FROM events WHERE type = 'task_reopened' AND principal = 'system:upgrade'`).Scan(&events)
		}
		_ = st.Close()
		if err != nil || got.Status != "open" || events != 1 {
			t.Fatalf("after start %d: status %q, %d task_reopened events, err %v; want open and 1", start, got.Status, events, err)
		}
	}
}

package cli

import (
	"strings"
	"testing"

	"github.com/n-orlov/board/internal/store"
)

func TestServeRejectsAWordOutsideStrandedAskingDone(t *testing.T) {
	for _, v := range []string{"stranded,bogus", "all", "done,", ",done", "stranded;done", "Done"} {
		for _, url := range []string{"", "http://receiver.invalid/hook"} {
			code, errs := runServe(t, envWith(map[string]string{"BOARD_NOTIFY_ON": v, "BOARD_NOTIFY_URL": url}))
			if code != ExitError || !strings.Contains(errs, "BOARD_NOTIFY_ON") {
				t.Errorf("BOARD_NOTIFY_ON=%q url=%q: got code=%d err=%q", v, url, code, errs)
			}
		}
	}
}

func notifyStore(t *testing.T) *store.Store {
	t.Helper()
	st, err := store.Open(sqliteURL(t))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	return st
}

func TestConfigureNotifyIsOffWithoutAURL(t *testing.T) {
	st := notifyStore(t)
	if err := configureNotify(st, envWith(map[string]string{"BOARD_NOTIFY_ON": "done"})); err != nil {
		t.Fatal(err)
	}
	for _, k := range store.NotifyKinds {
		if st.Notifies(k) {
			t.Errorf("%s notifies with BOARD_NOTIFY_URL unset", k)
		}
	}
}

func TestConfigureNotifyTurnsOnEveryKindByDefault(t *testing.T) {
	st := notifyStore(t)
	if err := configureNotify(st, envWith(map[string]string{"BOARD_NOTIFY_URL": "http://receiver.invalid/hook"})); err != nil {
		t.Fatal(err)
	}
	for _, k := range store.NotifyKinds {
		if !st.Notifies(k) {
			t.Errorf("%s does not notify by default", k)
		}
	}
}

func TestConfigureNotifyTurnsOnTheKindsListed(t *testing.T) {
	st := notifyStore(t)
	env := envWith(map[string]string{"BOARD_NOTIFY_URL": "http://receiver.invalid/hook", "BOARD_NOTIFY_ON": "asking, done"})
	if err := configureNotify(st, env); err != nil {
		t.Fatal(err)
	}
	if st.Notifies("stranded") || !st.Notifies("asking") || !st.Notifies("done") {
		t.Fatalf("stranded=%v asking=%v done=%v", st.Notifies("stranded"), st.Notifies("asking"), st.Notifies("done"))
	}
}

func TestConfigureNotifyRefusesABadWord(t *testing.T) {
	err := configureNotify(notifyStore(t), envWith(map[string]string{"BOARD_NOTIFY_ON": "later"}))
	if err == nil || !strings.Contains(err.Error(), "BOARD_NOTIFY_ON") {
		t.Fatalf("err = %v", err)
	}
}

func TestConfigureNotifyLinksAlertsToThePublicURL(t *testing.T) {
	st := notifyStore(t)
	env := envWith(map[string]string{"BOARD_NOTIFY_URL": "http://receiver.invalid/hook", "BOARD_PUBLIC_URL": "https://board.example"})
	if err := configureNotify(st, env); err != nil {
		t.Fatal(err)
	}
	if st.PublicURL() != "https://board.example" {
		t.Fatalf("PublicURL = %q", st.PublicURL())
	}
}

func TestConfigureNotifyRefusesAPublicURLThatIsNotAbsolute(t *testing.T) {
	st := notifyStore(t)
	env := envWith(map[string]string{"BOARD_NOTIFY_URL": "http://receiver.invalid/hook", "BOARD_PUBLIC_URL": "board.example"})
	if err := configureNotify(st, env); err == nil || !strings.Contains(err.Error(), "BOARD_PUBLIC_URL") {
		t.Fatalf("err = %v", err)
	}
	if st.Notifies("done") {
		t.Fatal("alerts were turned on with a bad BOARD_PUBLIC_URL")
	}
}

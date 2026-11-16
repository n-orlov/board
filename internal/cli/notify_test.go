package cli

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/n-orlov/board/internal/asks"
	"github.com/n-orlov/board/internal/audit"
	"github.com/n-orlov/board/internal/store"
)

func TestServeRejectsAWordOutsideStrandedAskingDone(t *testing.T) {
	for _, v := range []string{"stranded,bogus", "all", "done,", ",done", "stranded;done", "Done"} {
		for _, url := range []string{"", "http://receiver.invalid/hook"} {
			code, errs := runServe(t, envWith(map[string]string{"BOARD_NOTIFY_ON": v, "BOARD_NOTIFY_URL": url}))
			if code != ExitError || !strings.Contains(errs, "VH_NOTIFY_ON") {
				t.Errorf("VH_NOTIFY_ON=%q url=%q: got code=%d err=%q", v, url, code, errs)
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
			t.Errorf("%s notifies with VH_NOTIFY_URL unset", k)
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
	if err == nil || !strings.Contains(err.Error(), "VH_NOTIFY_ON") {
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
	if err := configureNotify(st, env); err == nil || !strings.Contains(err.Error(), "VH_PUBLIC_URL") {
		t.Fatalf("err = %v", err)
	}
	if st.Notifies("done") {
		t.Fatal("alerts were turned on with a bad VH_PUBLIC_URL")
	}
}

func TestServeRejectsABadNotifyTimeoutOrHeaderWithoutRepeatingTheValue(t *testing.T) {
	for name, tc := range map[string]struct{ key, val string }{
		"timeout zero": {"VH_NOTIFY_TIMEOUT", "0s"}, "timeout word": {"VH_NOTIFY_TIMEOUT", "soon"},
		"header no value": {"VH_NOTIFY_HEADER", "X-Token"}, "header newline": {"VH_NOTIFY_HEADER", "X-Token: hunter2\nX-Other: 1"},
	} {
		for _, url := range []string{"", "http://receiver.invalid/hook"} {
			code, errs := runServe(t, envWith(map[string]string{tc.key: tc.val, "BOARD_NOTIFY_URL": url}))
			if code != ExitError || !strings.Contains(errs, tc.key) || strings.Contains(errs, "hunter2") {
				t.Errorf("%s url=%q: code=%d err=%q", name, url, code, errs)
			}
		}
	}
}

// TestServeDeliversTheOutboxWithTheHeaderAndLogsNoSecret runs serve against a
// receiver and an outbox row, and checks BOARD_NOTIFY_HEADER reached the
// receiver and appears in no log line, no event and no outbox row.
func TestServeDeliversTheOutboxWithTheHeaderAndLogsNoSecret(t *testing.T) {
	t.Parallel()
	const secret = "hunter2-receiver-token"
	url := sqliteURL(t)
	st, err := store.Open(url)
	if err != nil {
		t.Fatal(err)
	}
	if err := st.Migrate(context.Background()); err != nil {
		t.Fatal(err)
	}
	st.SetNotify(store.NotifyKinds)
	if _, err := (asks.Store{Store: st}).Raise(context.Background(), audit.Actor{Handle: "claude@host"}, "key:x", asks.New{Question: "ship it?"}); err != nil {
		t.Fatal(err)
	}
	var mu sync.Mutex
	var got []string
	rc := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		mu.Lock()
		got = append(got, r.Header.Get("X-Receiver-Token")+"|"+r.Header.Get("Content-Type")+"|"+string(b))
		mu.Unlock()
	}))
	defer rc.Close()
	env := envWith(map[string]string{"BOARD_NOTIFY_URL": rc.URL, "BOARD_NOTIFY_HEADER": "X-Receiver-Token: " + secret, "BOARD_NOTIFY_TIMEOUT": "2s", "BOARD_PUBLIC_URL": "https://board.example"})
	ctx, cancel := context.WithCancel(context.Background())
	var errb bytes.Buffer
	exit := make(chan int, 1)
	go func() { exit <- serveUntil(ctx, &errb, env, url, "127.0.0.1:0", false) }()
	sent := func() bool {
		var n int
		_ = st.Reader().QueryRowContext(ctx, `SELECT COUNT(*) FROM notifications WHERE state = 'sent'`).Scan(&n)
		return n == 1
	}
	delivered := sent()
	for deadline := time.Now().Add(8 * time.Second); !delivered && time.Now().Before(deadline); delivered = sent() {
		time.Sleep(50 * time.Millisecond)
	}
	cancel()
	if code := <-exit; code != ExitOK {
		t.Fatalf("serve exited %d: %s", code, errb.String())
	}
	if !delivered {
		t.Fatalf("the outbox row was not delivered; stderr %q", errb.String())
	}
	mu.Lock()
	defer mu.Unlock()
	if len(got) != 1 || !strings.HasPrefix(got[0], secret+"|application/json|{") {
		t.Fatalf("receiver got %q", got)
	}
	if strings.Contains(errb.String(), secret) {
		t.Errorf("a log line carries the header value: %q", errb.String())
	}
	for _, q := range []string{`SELECT COALESCE(group_concat(summary || type || principal), '') FROM events`, `SELECT COALESCE(group_concat(payload || COALESCE(last_error, '')), '') FROM notifications`} {
		var all string
		if err := st.Reader().QueryRowContext(context.Background(), q).Scan(&all); err != nil {
			t.Fatal(err)
		}
		if strings.Contains(all, secret) {
			t.Errorf("%s carries the header value", q)
		}
	}
}

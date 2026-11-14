package server

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"testing"
	"time"

	"github.com/n-orlov/board/internal/asks"
	"github.com/n-orlov/board/internal/audit"
)

// A wait whose until has passed by the time the server handles it answers at
// once with the ask as it stood at until: an answer or withdrawal committed
// after until (its transaction stamped before until, as when it held the writer
// across it) leaves the ask open, with none of the end's fields, though it is
// committed when the server reads it; one committed before until is the
// answer. Both databases.
func TestAWaitWhoseUntilHasPassedAnswersWithTheAskAsItStoodAtUntil(t *testing.T) {
	t.Parallel()
	forEachLeg(t, func(t *testing.T, _ string, dbs databases) {
		st := openStore(t, dbs.New(t))
		if err := st.Migrate(context.Background()); err != nil {
			t.Fatal(err)
		}
		r, _ := askRig(t, st, newToken(t, st), newTestClock(), time.Hour)
		for _, c := range []struct {
			withdraw, afterUntil bool
			want                 string
		}{
			{false, true, "open"}, {true, true, "open"},
			{false, false, "answered"}, {true, false, "withdrawn"},
		} {
			ref := r.raiseAsk()
			until := time.Now()
			ender := &asks.Store{Store: st, Now: func() time.Time { return until.Add(-time.Second) }}
			if !c.afterUntil {
				until = time.Now().Add(-time.Hour) // replaced once the end commits
			}
			var err error
			if c.withdraw {
				_, err = ender.Withdraw(context.Background(), audit.Actor{Handle: "claude@host"}, "key:x", ref, "moved on")
			} else {
				_, err = ender.Answer(context.Background(), audit.Actor{Handle: "nik@host", Operator: true}, "key:x", ref, asks.Reply{Answer: "late"})
			}
			if err != nil {
				t.Fatal(err)
			}
			if !c.afterUntil {
				until = time.Now()
			}
			started := time.Now()
			resp := r.do(http.MethodGet, "/api/v1/asks/"+ref+"?wait="+untilQuery("5", until), "", r.token)
			b, _ := io.ReadAll(resp.Body)
			_ = resp.Body.Close()
			var doc map[string]any
			_ = json.Unmarshal(b, &doc)
			_, answered := doc["answered_at"]
			_, withdrawn := doc["withdrawn_at"]
			_, answer := doc["answer"]
			if resp.StatusCode != http.StatusOK || doc["state"] != c.want || time.Since(started) > 2*time.Second {
				t.Errorf("%+v: got %d %v after %s, want 200 %q at once", c, resp.StatusCode, doc["state"], time.Since(started), c.want)
			}
			if c.want == "open" && (answered || withdrawn || answer) {
				t.Errorf("%+v: the open ask carries its later end: %s", c, b)
			}
		}
	})
}

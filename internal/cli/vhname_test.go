package cli

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"
)

// R2: the binary is vh, so every usage line the command tree can print names it.
func TestEveryCommandUsageLineBeginsUsageVh(t *testing.T) {
	none := func(string) string { return "" }
	var cmds []string
	for c := range Commands() {
		cmds = append(cmds, c)
	}
	sort.Strings(cmds)
	if len(cmds) < 30 {
		t.Fatalf("the command tree holds %d commands: the walk sees nothing", len(cmds))
	}
	for _, c := range cmds {
		words := strings.Fields(c)
		// A bare command line is malformed for every command that takes an argument;
		// an unknown flag is malformed for every command that takes none.
		var usage string
		for _, args := range [][]string{words, append(append([]string{}, words...), "--no-such-flag")} {
			code, out, errs := runEnv(none, args...)
			if code != ExitUsage || out != "" {
				continue
			}
			usage = errs
			break
		}
		if usage == "" {
			t.Errorf("%q: neither a bare line nor an unknown flag ended in a usage error", c)
			continue
		}
		if !strings.HasPrefix(usage, "usage: vh ") || strings.Count(usage, "\n") != 1 {
			t.Errorf("%q: usage line %q does not begin %q", c, usage, "usage: vh ")
		}
	}
}

// R2: a next hint names the command to run, and that command is vh. Every
// command that can fail against a server answering 404 and 409 is run, and
// every arrow line it prints is read.
func TestNoNextHintBeginsArrowBoard(t *testing.T) {
	for _, status := range []int{http.StatusNotFound, http.StatusConflict} {
		f := &fakeBoard{handler: func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(status)
			_, _ = w.Write([]byte(`{"error":{"code":"x","message":"gone"}}`))
		}}
		srv := httptest.NewServer(f)
		var hints int
		for _, args := range [][]string{
			{"init", "show", "aa"}, {"init", "ls"}, {"init", "close", "aa"}, {"init", "reopen", "aa"}, {"init", "edit", "aa", "--title", "t"},
			{"task", "show", "aa-1"}, {"task", "ls"}, {"task", "done", "aa-1"}, {"task", "reopen", "aa-1"}, {"task", "close", "aa-1"},
			{"task", "add", "aa", "t"}, {"task", "dep", "add", "aa-2", "aa-1"},
			{"lease", "start", "aa-1", "x"}, {"lease", "renew", "L1"}, {"lease", "end", "L1"}, {"lease", "ack", "L1"}, {"lease", "ls"},
			{"ask", "show", "A1"}, {"ask", "ls"}, {"ask", "aa-1", "q?"}, {"ask", "withdraw", "A1"}, {"answer", "A1", "a"},
			{"note", "show", "N1"}, {"note", "x"}, {"notes"}, {"now"}, {"attention"}, {"events"}, {"milestone", "aa-1", "m"}, {"check", "aa-1", "c"},
		} {
			_, _, errs := runEnv(f.env(srv), args...)
			for _, line := range strings.Split(errs, "\n") {
				if !strings.Contains(line, "→ ") {
					continue
				}
				if strings.Contains(line, "→ board ") {
					t.Errorf("%v (%d): next hint %q begins %q", args, status, line, "→ board ")
				}
				if strings.Contains(line, "→ vh ") {
					hints++
				}
			}
		}
		srv.Close()
		if hints < 10 {
			t.Errorf("status %d: only %d next hints begin %q: the walk sees nothing", status, hints, "→ vh ")
		}
	}
}

// R2: no literal in the package's sources, or in the protocol text it embeds,
// names a board command any more.
func TestNoSourceLiteralNamesABoardCommand(t *testing.T) {
	files, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatal(err)
	}
	files = append(files, "protocol.txt")
	arrow := regexp.MustCompile("(→|usage:) board |[\"`]board (task|init|lease|ask|answer|notes?|now|attention|events|milestone|check|help|whoami|version|serve|admin)\\b")
	for _, f := range files {
		if strings.HasSuffix(f, "_test.go") {
			continue
		}
		b, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		if m := arrow.Find(b); m != nil {
			t.Errorf("%s names a board command: %q", f, m)
		}
	}
}

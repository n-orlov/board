package cli

import (
	"bytes"
	"strings"
	"testing"

	"github.com/n-orlov/board/internal/version"
)

func run(args ...string) (int, string, string) {
	var out, errb bytes.Buffer
	code := Run(args, &out, &errb, func(string) string { return "" })
	return code, out.String(), errb.String()
}

func TestVersionPrintsCurrentVersion(t *testing.T) {
	code, out, errs := run("version")
	if code != ExitOK || errs != "" || out != version.Current()+"\n" {
		t.Fatalf("got code=%d out=%q err=%q", code, out, errs)
	}
}

func TestUsageErrors(t *testing.T) {
	for _, args := range [][]string{{}, {"bogus"}, {"version", "extra"}, {"serve", "extra"}} {
		code, out, errs := run(args...)
		if code != 2 || out != "" || errs != Usage+"\n" || strings.Count(errs, "\n") != 1 {
			t.Fatalf("%v: got code=%d out=%q err=%q", args, code, out, errs)
		}
	}
}

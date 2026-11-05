package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func junit(cases ...string) string {
	return `<?xml version="1.0"?><testsuites><testsuite name="f">` + strings.Join(cases, "") + `</testsuite></testsuites>`
}

func tc(name, child string) string {
	if child == "" {
		return `<testcase classname="f" name="` + name + `"/>`
	}
	return `<testcase classname="f" name="` + name + `">` + child + `</testcase>`
}

func write(t *testing.T, name, body string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	return p
}

func compare(t *testing.T, sqlite, postgres string) (int, string, string) {
	t.Helper()
	var out, errb bytes.Buffer
	code := run([]string{"sqlite=" + write(t, "a.xml", sqlite), "postgres=" + write(t, "b.xml", postgres)}, &out, &errb)
	return code, out.String(), errb.String()
}

func TestAgreeingLegsPass(t *testing.T) {
	both := junit(tc("TestFeatures", ""), tc("one", ""), tc("two", `<skipped/>`), tc("bad", `<failure message="x"/>`))
	code, out, errs := compare(t, both, both)
	if code != 0 || out != "" || errs != "" {
		t.Fatalf("code %d, out %q, err %q; want a silent 0 (red on both legs is not a divergence)", code, out, errs)
	}
}

func TestGreenOnOneLegRedOnTheOtherDiverges(t *testing.T) {
	for _, child := range []string{`<failure message="x"/>`, `<error message="x"/>`} {
		sqlite := junit(tc("TestFeatures", ""), tc("one", ""), tc("same", ""))
		postgres := junit(tc("TestFeatures", child), tc("one", child), tc("same", ""))
		code, out, errs := compare(t, sqlite, postgres)
		if code != 1 {
			t.Fatalf("%s: code %d, want 1", child, code)
		}
		if out != "DIVERGENT f one: sqlite=green postgres=red\n" {
			t.Errorf("%s: stdout %q names the wrong scenario, or reports the TestFeatures aggregate", child, out)
		}
		if !strings.Contains(errs, "1 scenario(s) differ") {
			t.Errorf("stderr %q", errs)
		}
	}
}

func TestScenarioRunOnOneLegOnlyHasNothingToDisagreeWith(t *testing.T) {
	code, out, _ := compare(t, junit(tc("one", "")), junit(tc("one", ""), tc("only-postgres", `<failure/>`)))
	if code != 0 || out != "" {
		t.Fatalf("code %d, out %q", code, out)
	}
}

func TestSkippedIsNeitherGreenNorRed(t *testing.T) {
	code, _, _ := compare(t, junit(tc("one", `<skipped/>`)), junit(tc("one", `<failure/>`)))
	if code != 0 {
		t.Fatalf("code %d: skipped against red is not green against red", code)
	}
}

func TestRetriedTestFoldedByJunitflakyIsGreen(t *testing.T) {
	folded := tc("one", `<rerunFailure message="first attempt"/>`)
	code, _, _ := compare(t, junit(tc("one", "")), junit(folded))
	if code != 0 {
		t.Fatalf("code %d: a test that failed once and then passed is green", code)
	}
}

func TestATestWithSeveralAttemptsIsRedWhenAnyAttemptIsRed(t *testing.T) {
	twice := junit(tc("one", `<failure/>`), tc("one", `<failure/>`), tc("two", `<skipped/>`), tc("two", ""), tc("three", ""), tc("three", `<skipped/>`))
	code, out, _ := compare(t, junit(tc("one", ""), tc("two", ""), tc("three", "")), twice)
	if code != 1 || !strings.Contains(out, "f one: sqlite=green postgres=red") || strings.Contains(out, "f two") || strings.Contains(out, "f three") {
		t.Fatalf("code %d, out %q", code, out)
	}
}

func TestNestedSuitesAndRootCasesAreRead(t *testing.T) {
	nested := `<testsuites><testsuite name="a"><testsuite name="b"><testcase classname="f" name="deep"><failure/></testcase></testsuite></testsuite><testcase classname="f" name="root"/></testsuites>`
	flat := `<testsuites><testsuite name="a"><testcase classname="f" name="deep"/></testsuite><testcase classname="f" name="root"><error/></testcase></testsuites>`
	code, out, _ := compare(t, nested, flat)
	if code != 1 || strings.Count(out, "DIVERGENT") != 2 {
		t.Fatalf("code %d, out %q", code, out)
	}
}

func TestThreeLegsNameEveryOutcome(t *testing.T) {
	var out, errb bytes.Buffer
	code := run([]string{
		"a=" + write(t, "a.xml", junit(tc("one", ""))),
		"b=" + write(t, "b.xml", junit(tc("one", `<failure/>`))),
		"c=" + write(t, "c.xml", junit(tc("two", ""))),
	}, &out, &errb)
	if code != 1 || out.String() != "DIVERGENT f one: a=green b=red\n" {
		t.Fatalf("code %d, out %q", code, out.String())
	}
}

func TestUsageAndInputErrorsExitTwo(t *testing.T) {
	good := write(t, "good.xml", junit())
	bad := write(t, "bad.xml", "<testsuites")
	cases := [][]string{
		nil,
		{"sqlite=" + good},
		{"sqlite=" + good, "postgres"},
		{"sqlite=" + good, "=" + good},
		{"sqlite=" + good, "postgres="},
		{"sqlite=" + good, "postgres=" + filepath.Join(t.TempDir(), "missing.xml")},
		{"sqlite=" + good, "postgres=" + bad},
	}
	for _, args := range cases {
		var out, errb bytes.Buffer
		if code := run(args, &out, &errb); code != 2 || !strings.HasPrefix(errb.String(), "legcompare: ") {
			t.Errorf("%q: code %d, stderr %q; want 2 and a legcompare: message", args, code, errb.String())
		}
	}
}

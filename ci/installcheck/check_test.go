package main

import (
	"bytes"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

func readFile(t *testing.T, path ...string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(path...))
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func realFiles(t *testing.T) (script, guide string) {
	t.Helper()
	return readFile(t, "..", "..", "install.sh"), readFile(t, "..", "..", "INSTALL-AGENT.md")
}

func TestTheRealFilesAgree(t *testing.T) {
	script, guide := realFiles(t)
	problems, err := Check(script, guide)
	if err != nil || len(problems) != 0 {
		t.Fatalf("Check on the real files = %q, %v; want no disagreement", problems, err)
	}
}

// The real files carry the facts the guide is checked against: pin the values
// so a pair of fixtures that mirrors them stays honest.
func TestTheRealScriptYieldsTheDocumentedFacts(t *testing.T) {
	script, _ := realFiles(t)
	f, err := parseScript(script)
	if err != nil {
		t.Fatal(err)
	}
	if f.asset != "vh_<os>_<arch>.tar.gz" || f.checksum != "checksums.txt" || f.dir != "~/.local/bin" {
		t.Errorf("asset %q checksums %q dir %q", f.asset, f.checksum, f.dir)
	}
	if list(f.oses) != "[darwin, linux]" || list(f.arches) != "[amd64, arm64]" {
		t.Errorf("os %v arch %v", f.oses, f.arches)
	}
	want := "[VH_API_URL, VH_DOWNLOAD_URL, VH_INSTALL_DIR, VH_REPO, VH_REQUIRE_IMMUTABLE, VH_VERSION]"
	if list(f.vars) != want {
		t.Errorf("variables %v, want %s", f.vars, want)
	}
}

// Each fixture directory holds an install.sh and an INSTALL-AGENT.md that
// differ on exactly one dimension; "agree" is the baseline they all derive
// from and must pass.
func TestFixturePairsFailOnTheirOwnDimension(t *testing.T) {
	cases := []struct{ dir, dimension string }{
		{"asset", "asset name pattern"},
		{"os", "OS list"},
		{"arch", "arch list"},
		{"checksums", "checksum file name"},
		{"directory", "default install directory"},
		{"variables", "environment variable names"},
	}
	baseScript := readFile(t, "testdata", "agree", "install.sh.fixture")
	baseGuide := readFile(t, "testdata", "agree", "INSTALL-AGENT.md.fixture")
	if problems, err := Check(baseScript, baseGuide); err != nil || len(problems) != 0 {
		t.Fatalf("the baseline pair must agree: %q, %v", problems, err)
	}
	for _, tc := range cases {
		t.Run(tc.dir, func(t *testing.T) {
			script := readFile(t, "testdata", tc.dir, "install.sh.fixture")
			guide := readFile(t, "testdata", tc.dir, "INSTALL-AGENT.md.fixture")
			if (script == baseScript) == (guide == baseGuide) {
				t.Fatal("a fixture pair changes exactly one of the two files from the baseline")
			}
			problems, err := Check(script, guide)
			if err != nil {
				t.Fatal(err)
			}
			if len(problems) != 1 || !strings.HasPrefix(problems[0], tc.dimension+":") {
				t.Fatalf("Check = %q, want exactly one disagreement, on the %s", problems, tc.dimension)
			}
		})
	}
}

func TestAStrayVariableInTheGuideIsADisagreement(t *testing.T) {
	script, guide := realFiles(t)
	problems, err := Check(script, guide+"\nExport VH_SECRET_SAUCE first.\n")
	if err != nil || len(problems) != 1 || !strings.Contains(problems[0], "VH_SECRET_SAUCE") {
		t.Fatalf("Check = %q, %v", problems, err)
	}
}

func TestAnUnparseableFileIsAnErrorNotAPass(t *testing.T) {
	script, guide := realFiles(t)
	for name, tc := range map[string]struct{ script, guide, want string }{
		"no asset line":       {strings.Replace(script, "asset=", "asset_=", 1), guide, "asset="},
		"no checksums line":   {strings.Replace(script, "checksums=", "sums=", 1), guide, "checksums="},
		"no dir line":         {strings.Replace(script, "\ndir=", "\ndirectory=", 1), guide, "dir="},
		"no os case":          {strings.Replace(script, `case "$os" in`, `case "$kernel" in`, 1), guide, `case "$os"`},
		"no arch case":        {strings.Replace(script, `case "$arch" in`, `case "$cpu" in`, 1), guide, `case "$arch"`},
		"no fact in guide":    {script, strings.Replace(guide, "- Arch:", "- Architectures:", 1), `"Arch"`},
		"twice in guide":      {script, guide + "\n- OS: `linux`\n", `"OS" fact is listed twice`},
		"two asset values":    {script, strings.Replace(guide, "- Asset: `vh_<os>_<arch>.tar.gz`", "- Asset: `a` `b`", 1), "exactly one value"},
		"no fact values":      {script, strings.Replace(guide, "- OS: `linux`, `darwin`", "- OS: linux, darwin", 1), `"OS"`},
		"no runtime variable": {script, regexp.MustCompile("(?m)^- Runtime variables:.*$").ReplaceAllString(guide, ""), "Runtime variables"},
	} {
		t.Run(name, func(t *testing.T) {
			problems, err := Check(tc.script, tc.guide)
			if err == nil || !strings.Contains(err.Error(), tc.want) || problems != nil {
				t.Fatalf("Check = %q, %v; want an error naming %q", problems, err, tc.want)
			}
		})
	}
}

func TestRunExitCodes(t *testing.T) {
	root := filepath.Join("..", "..")
	good := []string{filepath.Join(root, "install.sh"), filepath.Join(root, "INSTALL-AGENT.md")}
	tests := []struct {
		name string
		args []string
		code int
		out  string
		err  string
	}{
		{"agree", good, 0, "agree on the asset name", ""},
		{"disagree", []string{filepath.Join("testdata", "arch", "install.sh.fixture"), filepath.Join("testdata", "arch", "INSTALL-AGENT.md.fixture")}, 1, "", "arch list: install.sh has"},
		{"missing script", []string{"nope.sh", good[1]}, 2, "", "nope.sh"},
		{"missing guide", []string{good[0], "nope.md"}, 2, "", "nope.md"},
		{"unparseable", []string{filepath.Join("testdata", "agree", "INSTALL-AGENT.md.fixture"), good[1]}, 2, "", "install.sh: no asset= line found"},
		{"too many arguments", []string{"a", "b", "c"}, 2, "", "usage:"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var out, errOut bytes.Buffer
			code := run(tc.args, &out, &errOut)
			if code != tc.code || !strings.Contains(out.String(), tc.out) || !strings.Contains(errOut.String(), tc.err) {
				t.Fatalf("run = %d, stdout %q, stderr %q; want %d, %q, %q", code, out.String(), errOut.String(), tc.code, tc.out, tc.err)
			}
			if tc.code == 0 && errOut.Len() != 0 {
				t.Errorf("stderr on a pass: %q", errOut.String())
			}
		})
	}
}

// R9: INSTALL-AGENT.md has the nine steps of SPEC 8.5, each a command and
// the result to expect, and step 8 sets the four runtime variables.
func TestTheGuideHasTheNineStepsEachWithACommandAndAnExpectedResult(t *testing.T) {
	_, guide := realFiles(t)
	parts := regexp.MustCompile(`(?m)^## (\d+)\. `).Split(guide, -1)
	nums := regexp.MustCompile(`(?m)^## (\d+)\. `).FindAllStringSubmatch(guide, -1)
	if len(nums) != 9 {
		t.Fatalf("%d numbered steps, want 9", len(nums))
	}
	for i, m := range nums {
		if m[1] != string(rune('1'+i)) {
			t.Errorf("step %d is numbered %s", i+1, m[1])
		}
		body := parts[i+1]
		if !strings.Contains(body, "```sh\n") {
			t.Errorf("step %d has no command block", i+1)
		}
		if !strings.Contains(body, "Expect:") {
			t.Errorf("step %d states no expected result", i+1)
		}
	}
	step8 := parts[8]
	for _, v := range []string{"VH_URL", "VH_API_KEY", "VH_ACTOR", "VH_WAY_BACK"} {
		if !strings.Contains(step8, "export "+v+"=") {
			t.Errorf("step 8 does not export %s", v)
		}
	}
}

func TestTheReadmeLinksTheGuide(t *testing.T) {
	if !strings.Contains(readFile(t, "..", "..", "README.md"), "(INSTALL-AGENT.md)") {
		t.Fatal("README.md does not link INSTALL-AGENT.md")
	}
}

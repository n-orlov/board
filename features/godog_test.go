package features

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cucumber/godog"
)

// TestFeatures runs the Godog scenarios against the real binary.
//
// BOARD_GODOG_PATHS (comma separated feature files relative to this package,
// each optionally suffixed ":<line>") narrows the run to those files, and
// BOARD_DB_LEG (sqlite, the default, or postgres) selects the database the
// server under test runs on. Unset, the whole directory runs on SQLite. A
// scenario tagged @postgres runs on the Postgres leg only.
func TestFeatures(t *testing.T) {
	leg, err := dbLeg()
	if err != nil {
		t.Fatal(err)
	}
	bin := buildBoard(t)
	h := &harness{binary: bin, leg: leg}
	tags := "~@postgres"
	if leg == "postgres" {
		server, err := startPostgres(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { server.terminate(context.Background()) })
		h.shared, tags = server, ""
	}
	suite := godog.TestSuite{
		ScenarioInitializer: func(sc *godog.ScenarioContext) {
			registerSteps(sc, h)
		},
		Options: &godog.Options{
			Format:   godogFormat(),
			Paths:    godogPaths(),
			Strict:   true,
			Tags:     tags,
			TestingT: t,
		},
	}
	if suite.Run() != 0 {
		t.Fatal("godog feature suite failed")
	}
}

func godogPaths() []string {
	var paths []string
	for _, p := range strings.Split(os.Getenv("BOARD_GODOG_PATHS"), ",") {
		if p = strings.TrimSpace(p); p != "" {
			paths = append(paths, p)
		}
	}
	if len(paths) == 0 {
		return []string{"."}
	}
	return paths
}

// godogFormat returns the godog Format string TestFeatures uses: exactly
// "pretty" unless BOARD_GODOG_JUNIT names a path (godog's own JUnit formatter
// then writes one <testcase> per scenario there) or BOARD_GODOG_ALLURE names
// an absolute allure-results directory (the in-repo "allure" formatter,
// allure_formatter_test.go, writes Allure 2 results there). ci/suite.sh sets
// both; a local run that sets neither is unaffected.
func godogFormat() string {
	format := "pretty"
	if path := strings.TrimSpace(os.Getenv("BOARD_GODOG_JUNIT")); path != "" {
		format += ",junit:" + path
	}
	if dir := envAbsPath(allureDirEnv); dir != "" {
		if err := os.MkdirAll(dir, 0o750); err == nil {
			// godog needs a file for every "name:path" part: a one-line summary.
			format += ",allure:" + filepath.Join(dir, "godog-allure-summary.txt")
		}
	}
	return format
}

// envAbsPath returns the cleaned absolute path exported in the named
// environment variable, or "" when it is unset, blank or not absolute.
func envAbsPath(name string) string {
	p := strings.TrimSpace(os.Getenv(name))
	if !filepath.IsAbs(p) {
		return ""
	}
	return filepath.Clean(p)
}

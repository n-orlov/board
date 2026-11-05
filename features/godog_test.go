package features

import (
	"os"
	"strings"
	"testing"

	"github.com/cucumber/godog"
)

// TestFeatures runs the Godog scenarios against the real binary.
//
// BOARD_GODOG_PATHS (comma separated feature files relative to this package,
// each optionally suffixed ":<line>") narrows the run to those files, and
// BOARD_DB_LEG (sqlite, the default, or postgres) selects the database the
// server under test runs on. Unset, the whole directory runs on SQLite.
func TestFeatures(t *testing.T) {
	leg, err := dbLeg()
	if err != nil {
		t.Fatal(err)
	}
	bin := buildBoard(t)
	suite := godog.TestSuite{
		ScenarioInitializer: func(sc *godog.ScenarioContext) {
			registerSteps(sc, &harness{binary: bin, leg: leg})
		},
		Options: &godog.Options{
			Format:   "pretty",
			Paths:    godogPaths(),
			Strict:   true,
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

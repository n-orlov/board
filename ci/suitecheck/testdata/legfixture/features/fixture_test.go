// Package features is the leg fixture for ci/suitecheck's database-matrix test.
// Its TestFeatures stands in for the Godog entry point: two scenario subtests
// that both pass on every leg, except that "diverges" fails on the leg named by
// LEGFIXTURE_RED_ON (a database leg, "both", or nothing). It reads the leg from
// BOARD_DB_LEG exactly as the real features/ package does, so it proves
// ci/suite.sh hands each pass its own leg.
package features

import (
	"os"
	"testing"
)

func TestFeatures(t *testing.T) {
	leg := os.Getenv("BOARD_DB_LEG")
	red := os.Getenv("LEGFIXTURE_RED_ON")
	t.Run("agrees", func(t *testing.T) {})
	t.Run("diverges", func(t *testing.T) {
		if red == "both" || (red != "" && red == leg) {
			t.Fatalf("scenario is red on the %s leg", leg)
		}
	})
	if marker := os.Getenv("LEGFIXTURE_LEG_LOG"); marker != "" {
		f, err := os.OpenFile(marker, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
		if err != nil {
			t.Fatal(err)
		}
		defer f.Close()
		_, _ = f.WriteString(leg + "\n")
	}
}

// Package unit is the leg fixture's ordinary Go package: one passing test that
// covers its one statement, so ci/suite.sh's step-1 pass has something to run
// and the merged coverage profile is not empty.
package unit

import "testing"

func TestFixtureUnitPasses(t *testing.T) {
	if Answer() != 42 {
		t.Fatal("Answer")
	}
}

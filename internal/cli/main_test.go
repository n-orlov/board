package cli

import (
	"fmt"
	"os"
	"testing"
)

// TestMain registers the SQLite connection hook and function the live ask
// tests hold commits and mark writes with, before any (parallel) test opens a
// database: the driver reads its hooks and functions unlocked as it opens a
// connection, so registering one while another test opens its store would be
// a data race.
func TestMain(m *testing.M) {
	installCommitHook()
	if err := installMarkHold(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	os.Exit(m.Run())
}

package main

import (
	"os"
	"testing"
)

func TestMainExitsWithTheCommandsCode(t *testing.T) {
	oldArgs, oldExit := os.Args, exit
	t.Cleanup(func() { os.Args, exit = oldArgs, oldExit })
	os.Args = []string{"board", "version"}
	code := -1
	exit = func(c int) { code = c }
	main()
	if code != 0 {
		t.Fatalf("board version exited %d, want 0", code)
	}
	os.Args = []string{"board"}
	main()
	if code != 2 {
		t.Fatalf("board with no command exited %d, want 2 (usage)", code)
	}
}

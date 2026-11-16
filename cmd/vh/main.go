// Command board is the single binary: the server and its own CLI.
package main

import (
	"os"

	"github.com/n-orlov/village-hall/internal/cli"
)

// exit ends the process; a test replaces it.
var exit = os.Exit

func main() { exit(cli.Run(os.Args[1:], os.Stdout, os.Stderr, os.Getenv)) }

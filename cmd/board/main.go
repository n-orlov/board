// Command board is the single binary: the server and its own CLI.
package main

import (
	"os"

	"github.com/n-orlov/board/internal/cli"
)

func main() { os.Exit(cli.Run(os.Args[1:], os.Stdout, os.Stderr, os.Getenv)) }

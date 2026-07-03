package main

import (
	"os"

	"github.com/elev1e1nSure/sieve/internal/cli"
	"github.com/elev1e1nSure/sieve/internal/selfupdate"
)

func main() {
	if exitCode, handled := selfupdate.RunHelper(os.Args[1:]); handled {
		os.Exit(exitCode)
	}

	cli.Execute()
}

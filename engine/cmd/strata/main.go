// Command strata is the Strata engine CLI.
package main

import (
	"os"

	"github.com/zbmowrey/strata/engine/internal/cli"
)

func main() {
	os.Exit(cli.Main(os.Args[1:], cli.OSStreams()))
}

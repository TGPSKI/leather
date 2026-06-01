// Command leather is the main entrypoint for the leather agent orchestrator.
// Business logic lives in internal/; this file is a thin dispatcher only.
package main

import (
	"os"

	"github.com/tgpski/leather/internal/cli"
)

// version and commit are set by -ldflags at build time.
//
//	-X main.version=$(git describe --tags --always --dirty)
//	-X main.commit=$(git rev-parse --short HEAD)
var (
	version = "dev"
	commit  = "none"
)

func main() {
	os.Exit(cli.Run(os.Args[1:], os.Stdout, os.Stderr, version, commit))
}

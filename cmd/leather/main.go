// Command leather is the main entrypoint for the leather agent orchestrator.
// Business logic lives in internal/; this file is a thin dispatcher only.
package main

import (
	"os"

	"github.com/TGPSKI/leather/internal/cli"
)

// version and commit are set by -ldflags at build time.
//
//	-X main.version=$(git describe --tags --always --dirty)
//	-X main.commit=$(git rev-parse --short HEAD)
//
// When the stamps are absent (plain `go install module@tag`), they are
// resolved from the embedded Go build info instead.
var (
	version = "dev"
	commit  = "none"
)

func main() {
	v, c := cli.ResolveBuildVersion(version, commit)
	os.Exit(cli.Run(os.Args[1:], os.Stdout, os.Stderr, v, c))
}

package cli

import (
	"fmt"
	"io"
	"runtime"
	"runtime/debug"
)

// RunVersion prints build version and platform information to stdout.
func RunVersion(_ []string, stdout, _ io.Writer, version, commit string) int {
	fmt.Fprintf(stdout, "leather %s (%s) %s/%s\n", version, commit, runtime.GOOS, runtime.GOARCH)
	return 0
}

// ResolveBuildVersion fills in version/commit from the embedded Go build info
// when the Makefile's -ldflags stamps are absent — i.e. for binaries built by
// plain `go install module@tag`, which previously self-identified as
// "dev (none)" (issues #49/#50). The module version covers @tag installs;
// vcs.revision covers plain `go build` from a git checkout.
func ResolveBuildVersion(version, commit string) (string, string) {
	bi, ok := debug.ReadBuildInfo()
	if !ok {
		return version, commit
	}
	if version == "dev" && bi.Main.Version != "" && bi.Main.Version != "(devel)" {
		version = bi.Main.Version
	}
	if commit == "none" {
		var rev string
		dirty := false
		for _, s := range bi.Settings {
			switch s.Key {
			case "vcs.revision":
				rev = s.Value
			case "vcs.modified":
				dirty = s.Value == "true"
			}
		}
		if rev != "" {
			if len(rev) > 12 {
				rev = rev[:12]
			}
			if dirty {
				rev += "-dirty"
			}
			commit = rev
		}
	}
	return version, commit
}

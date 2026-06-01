package cli

import (
	"fmt"
	"io"
	"runtime"
)

// RunVersion prints build version and platform information to stdout.
func RunVersion(_ []string, stdout, _ io.Writer, version, commit string) int {
	fmt.Fprintf(stdout, "leather %s (%s) %s/%s\n", version, commit, runtime.GOOS, runtime.GOARCH)
	return 0
}

package cli

import (
	"fmt"
	"io"
	"strings"
)

// RunReplay implements `leather replay <file> [serve flags...]` as a thin
// wrapper around `leather serve --replay <file> --api`. The replay subsystem
// runs inside the serve loop; exposing a top-level `replay` subcommand makes
// the entry point discoverable (T6.8).
//
// Usage:
//
//	leather replay <snapshot.json> [--addr 127.0.0.1:7749] [other serve flags]
//	leather replay --live <dir>     [other serve flags]
func RunReplay(args []string, stdout, stderr io.Writer, version, commit string) int {
	if len(args) == 0 || args[0] == "-h" || args[0] == "--help" {
		fmt.Fprint(stderr, replayUsage)
		if len(args) == 0 {
			return 2
		}
		return 0
	}

	// Build a serve argv that always enables the API (replay needs it) and
	// passes through either --replay <file> or --replay-live <dir>.
	serveArgs := []string{"--api"}

	first := args[0]
	rest := args[1:]
	switch {
	case first == "--live" || first == "-l":
		if len(rest) == 0 {
			fmt.Fprintln(stderr, "leather replay: --live requires a directory path")
			return 2
		}
		serveArgs = append(serveArgs, "--replay-live", rest[0])
		serveArgs = append(serveArgs, rest[1:]...)
	case strings.HasPrefix(first, "-"):
		// Forward all flags directly to serve; user supplied their own --replay.
		serveArgs = append(serveArgs, args...)
	default:
		serveArgs = append(serveArgs, "--replay", first)
		serveArgs = append(serveArgs, rest...)
	}

	return RunServe(serveArgs, stdout, stderr, version, commit)
}

const replayUsage = `Usage: leather replay <snapshot.json> [serve flags...]
       leather replay --live <runs-dir> [serve flags...]

Starts leather in replay mode (read-only API server backed by a captured
snapshot or a directory of JSONL run records). Equivalent to:

  leather serve --api --replay <snapshot.json>
  leather serve --api --replay-live <runs-dir>

Useful flags forwarded to serve:
  --addr <host:port>   API bind address (default 127.0.0.1:7749)
  --devtools           enable the /api/devtools/* surface
`

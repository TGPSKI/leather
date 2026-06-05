// Package cli implements leather's command dispatch and serve loop.
package cli

import (
	"flag"
	"fmt"
	"io"
	"os"
)

// Run is leather's main entry point. It parses the first argument as a
// subcommand and dispatches to the appropriate handler, returning an exit code.
// version and commit are set by -ldflags at build time.
func Run(args []string, stdout, stderr io.Writer, version, commit string) int {
	if len(args) == 0 {
		fmt.Fprint(stdout, usage)
		return 0
	}

	cmd, rest := args[0], args[1:]
	switch cmd {
	case "serve":
		return RunServe(rest, stdout, stderr, version, commit)
	case "chat":
		return RunChat(rest, os.Stdin, stdout, stderr)
	case "run":
		return RunOnce(rest, stdout, stderr)
	case "validate":
		return RunValidate(rest, stdout, stderr)
	case "test-agent":
		return RunTestAgent(rest, stdout, stderr)
	case "status":
		return RunStatus(rest, stdout, stderr)
	case "version":
		return RunVersion(rest, stdout, stderr, version, commit)
	case "--version", "-v":
		// Top-level convenience: `leather --version` / `leather -v`.
		return RunVersion(rest, stdout, stderr, version, commit)
	case "doctor":
		return RunDoctor(rest, stdout, stderr)
	case "init":
		return RunInit(rest, stdout, stderr)
	case "ingest":
		return RunIngest(rest, stdout, stderr)
	case "replay":
		return RunReplay(rest, stdout, stderr, version, commit)
	case "help", "--help", "-h":
		fmt.Fprint(stdout, usage)
		return 0
	default:
		fmt.Fprintf(stderr, "leather: unknown command %q\n\n", cmd)
		fmt.Fprint(stderr, usage)
		return 2
	}
}

// newFlagSet returns a flag.FlagSet with ContinueOnError for the named subcommand.
func newFlagSet(name string, stderr io.Writer) *flag.FlagSet {
	fs := flag.NewFlagSet("leather "+name, flag.ContinueOnError)
	fs.SetOutput(stderr)
	return fs
}

// parseFlags parses args on fs. It returns false if parsing fails, writing the
// error to stderr. Callers should return a non-zero exit code when false.
func parseFlags(fs *flag.FlagSet, args []string) bool {
	if err := fs.Parse(args); err != nil {
		// flag.ErrHelp is printed by the flag package itself; suppress duplicate output.
		return false
	}
	return true
}

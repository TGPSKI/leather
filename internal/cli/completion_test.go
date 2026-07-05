package cli

import (
	"bytes"
	"io"
	"strings"
	"testing"
)

func TestRunCompletion_Bash(t *testing.T) {
	var out bytes.Buffer
	code := RunCompletion([]string{"bash"}, &out, io.Discard)
	if code != 0 {
		t.Fatalf("exit code = %d, want 0", code)
	}
	if !strings.Contains(out.String(), "complete -F _leather_complete leather") {
		t.Errorf("bash script missing complete registration:\n%s", out.String())
	}
}

func TestRunCompletion_Zsh(t *testing.T) {
	var out bytes.Buffer
	code := RunCompletion([]string{"zsh"}, &out, io.Discard)
	if code != 0 {
		t.Fatalf("exit code = %d, want 0", code)
	}
	if !strings.HasPrefix(out.String(), "#compdef leather") {
		t.Errorf("zsh script missing #compdef header:\n%s", out.String())
	}
}

func TestRunCompletion_Fish(t *testing.T) {
	var out bytes.Buffer
	code := RunCompletion([]string{"fish"}, &out, io.Discard)
	if code != 0 {
		t.Fatalf("exit code = %d, want 0", code)
	}
	if !strings.Contains(out.String(), "complete -c leather") {
		t.Errorf("fish script missing complete directives:\n%s", out.String())
	}
}

func TestRunCompletion_UnknownShell(t *testing.T) {
	var out, errOut bytes.Buffer
	code := RunCompletion([]string{"powershell"}, &out, &errOut)
	if code != 2 {
		t.Errorf("exit code = %d, want 2", code)
	}
	if !strings.Contains(errOut.String(), "unknown shell") {
		t.Errorf("stderr missing unknown shell message: %q", errOut.String())
	}
}

func TestRunCompletion_NoArgs(t *testing.T) {
	var out, errOut bytes.Buffer
	code := RunCompletion(nil, &out, &errOut)
	if code != 2 {
		t.Errorf("exit code = %d, want 2", code)
	}
	if !strings.Contains(errOut.String(), "leather completion") {
		t.Errorf("stderr missing usage: %q", errOut.String())
	}
}

func TestRunCompletion_Help(t *testing.T) {
	var out bytes.Buffer
	code := RunCompletion([]string{"--help"}, &out, io.Discard)
	if code != 0 {
		t.Fatalf("exit code = %d, want 0", code)
	}
	if !strings.Contains(out.String(), "leather completion") {
		t.Errorf("help output missing usage: %q", out.String())
	}
}

func TestRun_Completion_Dispatches(t *testing.T) {
	var out bytes.Buffer
	code := Run([]string{"completion", "bash"}, &out, io.Discard, "dev", "none")
	if code != 0 {
		t.Errorf("Run(completion bash) exit code = %d, want 0", code)
	}
	if !strings.Contains(out.String(), "_leather_complete") {
		t.Errorf("Run(completion bash) output missing script body")
	}
}

// Every top-level command name embedded in the completion scripts must match
// a real dispatch case in cli.go, so completions never suggest a dead command.
func TestCompletionScripts_CommandsMatchDispatch(t *testing.T) {
	knownCommands := []string{
		"doctor", "init", "serve", "run", "validate", "test-agent", "status",
		"dlq", "ingest", "workflow", "replay", "snapshot", "attach", "version",
		"completion", "help",
	}

	var bashOut, zshOut, fishOut bytes.Buffer
	RunCompletion([]string{"bash"}, &bashOut, io.Discard)
	RunCompletion([]string{"zsh"}, &zshOut, io.Discard)
	RunCompletion([]string{"fish"}, &fishOut, io.Discard)

	for _, cmd := range knownCommands {
		var out bytes.Buffer
		code := Run([]string{cmd, "--help"}, &out, io.Discard, "dev", "none")
		// Every known command must be dispatched (not fall into the unknown-command branch).
		if code == 2 && strings.Contains(out.String(), "unknown command") {
			t.Errorf("command %q in completion scripts is not a real dispatch case", cmd)
		}

		for name, script := range map[string]string{"bash": bashOut.String(), "zsh": zshOut.String(), "fish": fishOut.String()} {
			if !strings.Contains(script, cmd) {
				t.Errorf("%s completion script missing command %q", name, cmd)
			}
		}
	}
}

// Descriptions are what make zsh and fish list completions vertically (one per
// line) instead of collapsing into a horizontal grid. Guard against a
// regression that drops them.
func TestCompletionScripts_CommandsHaveDescriptions(t *testing.T) {
	var zshOut, fishOut bytes.Buffer
	RunCompletion([]string{"zsh"}, &zshOut, io.Discard)
	RunCompletion([]string{"fish"}, &fishOut, io.Discard)

	// zsh: _describe consumes value:description pairs.
	if !strings.Contains(zshOut.String(), "'serve:run the scheduler loop") {
		t.Error("zsh script missing described command entries (needed for vertical listing)")
	}
	if !strings.Contains(zshOut.String(), "_describe -V") {
		t.Error("zsh script should use _describe for described, vertical listing")
	}

	// fish: -d flags attach descriptions to each command.
	if !strings.Contains(fishOut.String(), "-a serve -d ") {
		t.Error("fish script missing described command entries (needed for vertical listing)")
	}
}

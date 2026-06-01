//go:build integration

package main

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// buildBinary compiles the leather binary into a temp dir and returns its path.
// The binary is automatically removed when the test finishes.
func buildBinary(t *testing.T) string {
	t.Helper()
	root := repoRoot(t)
	dir := t.TempDir()
	bin := filepath.Join(dir, "leather")
	cmd := exec.Command("go", "build", "-o", bin, "./cmd/leather")
	cmd.Dir = root
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("build leather: %v\n%s", err, out)
	}
	return bin
}

// repoRoot walks up from the current directory until it finds go.mod.
func repoRoot(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatal("could not find module root (go.mod)")
		}
		dir = parent
	}
}

// run executes leather with args and returns stdout, stderr, exit code.
func run(t *testing.T, bin string, args ...string) (stdout, stderr string, exitCode int) {
	t.Helper()
	var outBuf, errBuf bytes.Buffer
	cmd := exec.Command(bin, args...)
	cmd.Stdout = &outBuf
	cmd.Stderr = &errBuf
	err := cmd.Run()
	exitCode = 0
	if err != nil {
		if ee, ok := err.(*exec.ExitError); ok {
			exitCode = ee.ExitCode()
		} else {
			t.Fatalf("run leather %v: %v", args, err)
		}
	}
	return outBuf.String(), errBuf.String(), exitCode
}

func TestIntegration_Version(t *testing.T) {
	bin := buildBinary(t)
	stdout, _, code := run(t, bin, "version")
	if code != 0 {
		t.Errorf("exit code = %d, want 0", code)
	}
	if !strings.Contains(stdout, "leather") {
		t.Errorf("expected 'leather' in version output, got: %q", stdout)
	}
}

func TestIntegration_Help(t *testing.T) {
	bin := buildBinary(t)
	stdout, _, code := run(t, bin)
	if code != 0 {
		t.Errorf("exit code = %d, want 0", code)
	}
	if !strings.Contains(stdout, "serve") {
		t.Errorf("expected 'serve' in help output, got: %q", stdout)
	}
}

func TestIntegration_UnknownCommand(t *testing.T) {
	bin := buildBinary(t)
	_, stderr, code := run(t, bin, "notacommand")
	if code != 2 {
		t.Errorf("exit code = %d, want 2", code)
	}
	if !strings.Contains(stderr, "notacommand") {
		t.Errorf("expected command name in stderr, got: %q", stderr)
	}
}

func TestIntegration_Validate_NoDir(t *testing.T) {
	bin := buildBinary(t)
	_, stderr, code := run(t, bin, "validate", "--agent-dir", "/nonexistent/path")
	if code == 0 {
		t.Errorf("expected non-zero exit for missing agent dir")
		_ = stderr
	}
}

func TestIntegration_Validate_ValidAgents(t *testing.T) {
	bin := buildBinary(t)
	dir := t.TempDir()

	// Write a valid agent with a lifecycle file.
	writeFile(t, filepath.Join(dir, "test.agent.md"), `---
name: test-agent
---
System prompt.
`)
	writeFile(t, filepath.Join(dir, "test.lifecycle.yaml"), `agent: test-agent
schedule: "0 * * * *"
model: llama3
`)

	stdout, _, code := run(t, bin, "validate", "--agent-dir", dir)
	if code != 0 {
		t.Errorf("exit code = %d, want 0\nstdout: %s", code, stdout)
	}
}

func TestIntegration_Status_NoState(t *testing.T) {
	bin := buildBinary(t)
	stateDir := t.TempDir()
	stdout, _, code := run(t, bin, "status", "--state-dir", stateDir)
	if code != 0 {
		t.Errorf("exit code = %d, want 0", code)
	}
	if !strings.Contains(stdout, "no persisted job records") {
		t.Errorf("expected no-state message, got: %q", stdout)
	}
}

func TestIntegration_Serve_GracefulShutdown(t *testing.T) {
	if os.Getenv("LEATHER_TEST_BUILD") == "" {
		t.Skip("set LEATHER_TEST_BUILD=1 to run serve tests")
	}
	bin := buildBinary(t)
	dir := t.TempDir()

	// Empty agent dir — serve should start and accept SIGTERM.
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, bin, "serve", "--agent-dir", dir)
	var errBuf bytes.Buffer
	cmd.Stderr = &errBuf

	if err := cmd.Start(); err != nil {
		t.Fatalf("start serve: %v", err)
	}

	// Give the process a moment to start.
	time.Sleep(200 * time.Millisecond)

	// Send SIGTERM.
	if err := cmd.Process.Signal(os.Interrupt); err != nil {
		t.Fatalf("signal: %v", err)
	}

	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()

	select {
	case err := <-done:
		// Process exited — check it was clean.
		if err != nil {
			if ee, ok := err.(*exec.ExitError); ok && ee.ExitCode() != 0 {
				t.Logf("serve stderr: %s", errBuf.String())
				// Exit code 130 (SIGINT) is acceptable on Linux.
				if ee.ExitCode() != 130 {
					t.Errorf("serve exited with code %d", ee.ExitCode())
				}
			}
		}
	case <-time.After(8 * time.Second):
		cmd.Process.Kill()
		t.Fatal("serve did not shut down within 8 seconds after SIGTERM")
	}
}

// writeFile writes content to path at 0600, failing t on error.
func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0600); err != nil {
		t.Fatalf("writeFile %s: %v", path, err)
	}
}

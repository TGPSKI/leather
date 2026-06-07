package cli

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/tgpski/leather/internal/session"
)

// writeWorkflowFixture writes a tannery.yaml, a curing def, and a minimal
// agent .md file so RunWorkflowRun has everything it needs.
// Returns (tanneryPath, stateDir).
func writeWorkflowFixture(t *testing.T, base string) (string, string) {
	t.Helper()
	hideDir := filepath.Join(base, "hides")
	curingDir := filepath.Join(base, "curings")
	artDir := filepath.Join(base, "artifacts")
	agentDir := filepath.Join(base, "agents")
	stateDir := filepath.Join(base, "state")
	for _, d := range []string{hideDir, curingDir, artDir, agentDir, stateDir} {
		if err := os.MkdirAll(d, 0700); err != nil {
			t.Fatal(err)
		}
	}

	// tannery.yaml with one route and one queue.
	tannContent := "hide_dir: " + hideDir + "\n" +
		"curing_dir: " + curingDir + "\n" +
		"artifact_dir: " + artDir + "\n" +
		"routes:\n" +
		"  - name: test-route\n" +
		"    match:\n" +
		"      source: cli\n" +
		"      event_type: test.raw\n" +
		"    hide_kind: test.raw\n" +
		"    curing: test-curing\n" +
		"    queue: test-queue\n" +
		"queues:\n" +
		"  test-queue:\n" +
		"    concurrency: 1\n" +
		"    max_depth: 100\n"
	tannPath := filepath.Join(base, "tannery.yaml")
	if err := os.WriteFile(tannPath, []byte(tannContent), 0600); err != nil {
		t.Fatal(err)
	}

	// Curing definition.
	curingContent := "name: test-curing\n" +
		"agent: test-agent\n" +
		"hide_types:\n" +
		"  - test.raw\n" +
		"queue: test-queue\n" +
		"max_attempts: 3\n"
	if err := os.WriteFile(filepath.Join(curingDir, "test-curing.curing.yaml"), []byte(curingContent), 0600); err != nil {
		t.Fatal(err)
	}

	// Minimal agent frontmatter + body.
	agentContent := "---\nname: test-agent\nmodel: test-model\nenabled: true\n---\nRespond with OK.\n"
	if err := os.WriteFile(filepath.Join(agentDir, "test-agent.agent.md"), []byte(agentContent), 0600); err != nil {
		t.Fatal(err)
	}

	return tannPath, stateDir
}

func TestRunWorkflow_NoArgs(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := RunWorkflow(nil, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("exit %d, stderr: %s", code, stderr.String())
	}
	if !strings.Contains(stdout.String(), "workflow") {
		t.Errorf("expected usage in stdout, got: %s", stdout.String())
	}
}

func TestRunWorkflow_UnknownSubcommand(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := RunWorkflow([]string{"bogus"}, &stdout, &stderr)
	if code != 2 {
		t.Errorf("exit = %d, want 2", code)
	}
	if !strings.Contains(stderr.String(), "unknown sub-command") {
		t.Errorf("expected 'unknown sub-command' in stderr, got: %s", stderr.String())
	}
}

func TestRunWorkflow_Help(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := RunWorkflow([]string{"--help"}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("exit %d", code)
	}
	if stdout.Len() == 0 {
		t.Error("expected usage output")
	}
}

func TestRunWorkflowRun_MissingTannery(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := RunWorkflowRun([]string{"--kind", "test.raw"}, &stdout, &stderr)
	if code != 2 {
		t.Errorf("exit = %d, want 2", code)
	}
}

func TestRunWorkflowRun_MissingKind(t *testing.T) {
	dir := t.TempDir()
	tannPath, stateDir := writeWorkflowFixture(t, dir)

	var stdout, stderr bytes.Buffer
	code := RunWorkflowRun([]string{
		"--tannery", tannPath,
		"--state-dir", stateDir,
		// --kind omitted, --curing also omitted
	}, &stdout, &stderr)
	if code != 2 {
		t.Errorf("exit = %d, want 2; stderr: %s", code, stderr.String())
	}
}

func TestRunWorkflowRun_ExplicitCuringMissingQueue(t *testing.T) {
	dir := t.TempDir()
	tannPath, stateDir := writeWorkflowFixture(t, dir)

	var stdout, stderr bytes.Buffer
	code := RunWorkflowRun([]string{
		"--tannery", tannPath,
		"--state-dir", stateDir,
		"--curing", "test-curing",
		// --queue omitted
	}, &stdout, &stderr)
	if code != 2 {
		t.Errorf("exit = %d, want 2; stderr: %s", code, stderr.String())
	}
}

func TestRunWorkflowRun_NoMatchingRoute(t *testing.T) {
	dir := t.TempDir()
	tannPath, stateDir := writeWorkflowFixture(t, dir)

	old := os.Stdin
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	os.Stdin = r
	defer func() { os.Stdin = old }()
	_, _ = w.WriteString("payload")
	_ = w.Close()

	var stdout, stderr bytes.Buffer
	code := RunWorkflowRun([]string{
		"--tannery", tannPath,
		"--state-dir", stateDir,
		"--kind", "no-such-kind",
		"--source", "cli",
	}, &stdout, &stderr)
	if code != 2 {
		t.Errorf("exit = %d, want 2; stderr: %s", code, stderr.String())
	}
}

func TestRunWorkflowRun_Success_Stdin(t *testing.T) {
	dir := t.TempDir()
	tannPath, stateDir := writeWorkflowFixture(t, dir)

	// Inject MockLLM.
	orig := workflowLLMClient
	workflowLLMClient = session.NewMockLLM(session.MockConfig{
		Response:         "workflow OK",
		TokensPerMessage: 5,
	})
	defer func() { workflowLLMClient = orig }()

	old := os.Stdin
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	os.Stdin = r
	defer func() { os.Stdin = old }()
	_, _ = w.WriteString("test payload from stdin")
	_ = w.Close()

	agentDir := filepath.Join(dir, "agents")
	var stdout, stderr bytes.Buffer
	code := RunWorkflowRun([]string{
		"--tannery", tannPath,
		"--state-dir", stateDir,
		"--agent-dir", agentDir,
		"--kind", "test.raw",
		"--source", "cli",
		"--settle", "50ms",
	}, &stdout, &stderr)

	if code != 0 {
		t.Fatalf("exit %d, stderr: %s", code, stderr.String())
	}
	out := stdout.String()
	if !strings.Contains(out, "hide_id") {
		t.Errorf("expected 'hide_id' in output, got:\n%s", out)
	}
	if !strings.Contains(out, "workflow OK") {
		t.Errorf("expected artifact content in output, got:\n%s", out)
	}
}

func TestRunWorkflowRun_Success_File(t *testing.T) {
	dir := t.TempDir()
	tannPath, stateDir := writeWorkflowFixture(t, dir)

	orig := workflowLLMClient
	workflowLLMClient = session.NewMockLLM(session.MockConfig{
		Response:         "file OK",
		TokensPerMessage: 5,
	})
	defer func() { workflowLLMClient = orig }()

	inputFile := filepath.Join(dir, "input.txt")
	if err := os.WriteFile(inputFile, []byte("file content"), 0600); err != nil {
		t.Fatal(err)
	}

	agentDir := filepath.Join(dir, "agents")
	var stdout, stderr bytes.Buffer
	code := RunWorkflowRun([]string{
		"--tannery", tannPath,
		"--state-dir", stateDir,
		"--agent-dir", agentDir,
		"--kind", "test.raw",
		"--source", "cli",
		"--settle", "50ms",
		inputFile,
	}, &stdout, &stderr)

	if code != 0 {
		t.Fatalf("exit %d, stderr: %s", code, stderr.String())
	}
	if !strings.Contains(stdout.String(), "file OK") {
		t.Errorf("expected artifact content in output, got:\n%s", stdout.String())
	}
}

func TestRunWorkflowRun_ExplicitCuring(t *testing.T) {
	dir := t.TempDir()
	tannPath, stateDir := writeWorkflowFixture(t, dir)

	orig := workflowLLMClient
	workflowLLMClient = session.NewMockLLM(session.MockConfig{
		Response:         "explicit OK",
		TokensPerMessage: 5,
	})
	defer func() { workflowLLMClient = orig }()

	old := os.Stdin
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	os.Stdin = r
	defer func() { os.Stdin = old }()
	_, _ = w.WriteString("payload")
	_ = w.Close()

	agentDir := filepath.Join(dir, "agents")
	var stdout, stderr bytes.Buffer
	code := RunWorkflowRun([]string{
		"--tannery", tannPath,
		"--state-dir", stateDir,
		"--agent-dir", agentDir,
		"--curing", "test-curing",
		"--queue", "test-queue",
		"--kind", "test.raw",
		"--settle", "50ms",
	}, &stdout, &stderr)

	if code != 0 {
		t.Fatalf("exit %d, stderr: %s", code, stderr.String())
	}
	if !strings.Contains(stdout.String(), "curing     test-curing") {
		t.Errorf("expected curing in output, got:\n%s", stdout.String())
	}
}

func TestRunWorkflowRun_DLQ(t *testing.T) {
	dir := t.TempDir()
	tannPath, stateDir := writeWorkflowFixture(t, dir)

	// Write a curing with max_attempts: 1 so the first failure goes straight to DLQ.
	curingContent := "name: test-curing\n" +
		"agent: test-agent\n" +
		"hide_types:\n" +
		"  - test.raw\n" +
		"queue: test-queue\n" +
		"max_attempts: 1\n"
	curingDir := filepath.Join(dir, "curings")
	if err := os.WriteFile(filepath.Join(curingDir, "test-curing.curing.yaml"), []byte(curingContent), 0600); err != nil {
		t.Fatal(err)
	}

	orig := workflowLLMClient
	workflowLLMClient = session.NewMockLLM(session.MockConfig{
		Err: errors.New("mock LLM failure"),
	})
	defer func() { workflowLLMClient = orig }()

	old := os.Stdin
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	os.Stdin = r
	defer func() { os.Stdin = old }()
	_, _ = w.WriteString("payload")
	_ = w.Close()

	agentDir := filepath.Join(dir, "agents")
	var stdout, stderr bytes.Buffer
	code := RunWorkflowRun([]string{
		"--tannery", tannPath,
		"--state-dir", stateDir,
		"--agent-dir", agentDir,
		"--kind", "test.raw",
		"--source", "cli",
		"--settle", "50ms",
	}, &stdout, &stderr)

	if code != 1 {
		t.Errorf("exit = %d, want 1; stderr: %s", code, stderr.String())
	}
	if !strings.Contains(stderr.String(), "dlq") {
		t.Errorf("expected 'dlq' in stderr, got: %s", stderr.String())
	}
}

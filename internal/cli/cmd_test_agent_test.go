package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestRunTestAgent_Basic verifies a successful run: agent file exists, mock response
// appears in stdout, exit 0.
func TestRunTestAgent_Basic(t *testing.T) {
	dir := t.TempDir()
	agentFile := filepath.Join(dir, "my-test.agent.md")
	content := "---\nname: my-test\n---\nYou are a test agent."
	if err := os.WriteFile(agentFile, []byte(content), 0600); err != nil {
		t.Fatal(err)
	}

	var stdout, stderr strings.Builder
	code := RunTestAgent([]string{"--mock-response", "hello", agentFile}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("expected exit 0, got %d; stderr=%q", code, stderr.String())
	}

	out := stdout.String()
	if !strings.Contains(out, "assistant: hello") {
		t.Errorf("expected 'assistant: hello' in output, got:\n%s", out)
	}
	if !strings.Contains(out, "status: success") {
		t.Errorf("expected 'status: success' in output, got:\n%s", out)
	}
	if !strings.Contains(out, "--- test-agent: my-test ---") {
		t.Errorf("expected header line in output, got:\n%s", out)
	}
}

// TestRunTestAgent_MissingAgentFile verifies that omitting the positional arg exits 2.
func TestRunTestAgent_MissingAgentFile(t *testing.T) {
	var stdout, stderr strings.Builder
	code := RunTestAgent([]string{}, &stdout, &stderr)
	if code != 2 {
		t.Fatalf("expected exit 2, got %d; stderr=%q", code, stderr.String())
	}
}

// TestRunTestAgent_InvalidAgentFile verifies that a nonexistent path exits 1.
func TestRunTestAgent_InvalidAgentFile(t *testing.T) {
	var stdout, stderr strings.Builder
	code := RunTestAgent([]string{"/nonexistent/path/missing.agent.md"}, &stdout, &stderr)
	if code != 1 {
		t.Fatalf("expected exit 1, got %d; stderr=%q", code, stderr.String())
	}
	if !strings.Contains(stderr.String(), "test-agent") {
		t.Errorf("expected error to mention 'test-agent', got stderr=%q", stderr.String())
	}
}

// TestRunTestAgent_WithLifecycle verifies that --lifecycle applies lifecycle fields.
func TestRunTestAgent_WithLifecycle(t *testing.T) {
	dir := t.TempDir()

	agentFile := filepath.Join(dir, "lc-agent.agent.md")
	agentContent := "---\nname: lc-agent\n---\nYou are a lifecycle agent."
	if err := os.WriteFile(agentFile, []byte(agentContent), 0600); err != nil {
		t.Fatal(err)
	}

	lcFile := filepath.Join(dir, "lc-agent.lifecycle.yaml")
	lcContent := "agent: lc-agent\nschedule: once\nmodel: test-model\n"
	if err := os.WriteFile(lcFile, []byte(lcContent), 0600); err != nil {
		t.Fatal(err)
	}

	var stdout, stderr strings.Builder
	code := RunTestAgent([]string{"--lifecycle", lcFile, "--mock-response", "lifecycle ok", agentFile}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("expected exit 0, got %d; stderr=%q stdout=%q", code, stderr.String(), stdout.String())
	}

	out := stdout.String()
	if !strings.Contains(out, "assistant: lifecycle ok") {
		t.Errorf("expected 'assistant: lifecycle ok' in output, got:\n%s", out)
	}
}

// TestRunTestAgent_ToolResponseFlag verifies that --tool-response is accepted without error.
func TestRunTestAgent_ToolResponseFlag(t *testing.T) {
	dir := t.TempDir()
	agentFile := filepath.Join(dir, "tool-test.agent.md")
	content := "---\nname: tool-test\n---\nTool test agent."
	if err := os.WriteFile(agentFile, []byte(content), 0600); err != nil {
		t.Fatal(err)
	}

	var stdout, stderr strings.Builder
	code := RunTestAgent([]string{
		"--tool-response", "my-tool=result text",
		"--mock-response", "done",
		agentFile,
	}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("expected exit 0, got %d; stderr=%q", code, stderr.String())
	}
}

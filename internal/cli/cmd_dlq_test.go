package cli

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/tgpski/leather/internal/model"
)

// writeDLQItem writes a QueueItem as a JSONL line to the given queue file.
func writeDLQItem(t *testing.T, path string, item model.QueueItem) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	b, err := json.Marshal(item)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0600)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer f.Close()
	_, _ = f.Write(append(b, '\n'))
}

func TestRunDLQInspect_Empty(t *testing.T) {
	stateDir := t.TempDir()
	var stdout, stderr bytes.Buffer
	code := RunDLQ([]string{"inspect", "--state-dir", stateDir}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("exit code = %d, want 0; stderr=%s", code, stderr.String())
	}
	if !strings.Contains(stdout.String(), "empty") {
		t.Errorf("stdout = %q, want to contain 'empty'", stdout.String())
	}
}

func TestRunDLQInspect_WithItems(t *testing.T) {
	stateDir := t.TempDir()
	queuePath := filepath.Join(stateDir, "queues", "outbound-dlq.jsonl")

	item := model.QueueItem{
		ID:         "odlq_20260101_1200_abcd",
		AgentName:  "my-agent",
		ToolName:   "github_list_issues",
		ToolTarget: "https://api.github.com/repos/acme/repo/issues",
		EnqueuedAt: time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC).Unix(),
		Payload: map[string]any{
			"tool":    "github_list_issues",
			"error":   "status 503: service unavailable",
			"attempt": float64(3),
		},
	}
	writeDLQItem(t, queuePath, item)

	var stdout, stderr bytes.Buffer
	code := RunDLQ([]string{"inspect", "--state-dir", stateDir}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("exit code = %d, want 0; stderr=%s", code, stderr.String())
	}
	out := stdout.String()
	if !strings.Contains(out, "odlq_20260101_1200_abcd") {
		t.Errorf("stdout missing item ID; got:\n%s", out)
	}
	if !strings.Contains(out, "github_list_issues") {
		t.Errorf("stdout missing tool name; got:\n%s", out)
	}
	if !strings.Contains(out, "my-agent") {
		t.Errorf("stdout missing agent name; got:\n%s", out)
	}
}

func TestRunDLQRequeue_MovesItem(t *testing.T) {
	stateDir := t.TempDir()
	queueDir := filepath.Join(stateDir, "queues")
	dlqPath := filepath.Join(queueDir, "outbound-dlq.jsonl")

	item := model.QueueItem{
		ID:         "odlq_20260101_1200_beef",
		AgentName:  "requeue-agent",
		ToolName:   "my_tool",
		EnqueuedAt: time.Now().Unix(),
		Payload:    map[string]any{"tool": "my_tool", "error": "timeout"},
	}
	writeDLQItem(t, dlqPath, item)

	var stdout, stderr bytes.Buffer
	code := RunDLQ([]string{
		"requeue",
		"--queue", "outbound-dlq",
		"--work-queue", "my-work-queue",
		"--state-dir", stateDir,
		"odlq_20260101_1200_beef",
	}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("exit code = %d, want 0; stderr=%s", code, stderr.String())
	}
	if !strings.Contains(stdout.String(), "requeued") {
		t.Errorf("stdout = %q, want to contain 'requeued'", stdout.String())
	}

	// DLQ should now be empty.
	dlqData, err := os.ReadFile(dlqPath)
	if err != nil && !os.IsNotExist(err) {
		t.Fatalf("read dlq: %v", err)
	}
	if len(strings.TrimSpace(string(dlqData))) != 0 {
		t.Errorf("DLQ not empty after requeue; contents: %s", dlqData)
	}

	// Work queue should contain the item.
	workPath := filepath.Join(queueDir, "my-work-queue.jsonl")
	workData, err := os.ReadFile(workPath)
	if err != nil {
		t.Fatalf("read work queue: %v", err)
	}
	var moved model.QueueItem
	if err := json.Unmarshal(bytes.TrimSpace(workData), &moved); err != nil {
		t.Fatalf("unmarshal work item: %v", err)
	}
	if moved.ID != item.ID {
		t.Errorf("moved item ID = %q, want %q", moved.ID, item.ID)
	}
	if moved.AttemptCount != 0 {
		t.Errorf("moved item AttemptCount = %d, want 0 (reset)", moved.AttemptCount)
	}
}

func TestRunDLQRequeue_ItemNotFound(t *testing.T) {
	stateDir := t.TempDir()
	// Create empty dlq.
	queueDir := filepath.Join(stateDir, "queues")
	if err := os.MkdirAll(queueDir, 0700); err != nil {
		t.Fatal(err)
	}
	dlqPath := filepath.Join(queueDir, "outbound-dlq.jsonl")
	if err := os.WriteFile(dlqPath, nil, 0600); err != nil {
		t.Fatal(err)
	}

	var stdout, stderr bytes.Buffer
	code := RunDLQ([]string{
		"requeue",
		"--work-queue", "dest",
		"--state-dir", stateDir,
		"nonexistent-id",
	}, &stdout, &stderr)
	if code != 1 {
		t.Errorf("exit code = %d, want 1", code)
	}
	if !strings.Contains(stderr.String(), "not found") {
		t.Errorf("stderr = %q, want to contain 'not found'", stderr.String())
	}
}

func TestRunDLQRequeue_MissingItemID(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := RunDLQ([]string{"requeue"}, &stdout, &stderr)
	if code != 2 {
		t.Errorf("exit code = %d, want 2", code)
	}
}

func TestRunDLQUnknownSubcommand(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := RunDLQ([]string{"bogus"}, &stdout, &stderr)
	if code != 2 {
		t.Errorf("exit code = %d, want 2", code)
	}
}

func TestRunDLQHelp(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := RunDLQ([]string{"--help"}, &stdout, &stderr)
	if code != 0 {
		t.Errorf("exit code = %d, want 0", code)
	}
	if !strings.Contains(stdout.String(), "inspect") {
		t.Errorf("help output missing 'inspect'; got: %s", stdout.String())
	}
}

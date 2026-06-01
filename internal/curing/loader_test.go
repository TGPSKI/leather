package curing

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/tgpski/leather/internal/model"
)

const minimalCuring = `
name: pr-review
agent: pr-review
queue: github-prs
`

const fullCuring = `
name: pr-review
description: Review PRs from GitHub
agent: pr-review
hide_types:
  - github.pr_review_thread
  - github.pr_diff
queue: github-prs
page_size_bytes: 4096
max_attempts: 5
timeout_seconds: 600
output:
  notify: slack
  queue: review-results
`

const zeroTimeoutCuring = `
name: fast-review
agent: fast-agent
queue: fast-queue
timeout_seconds: 0
`

func writeCuringFile(t *testing.T, dir, name, content string) string {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte(content), 0600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestParseCuringYAML_Minimal(t *testing.T) {
	def, err := parseCuringYAML(minimalCuring)
	if err != nil {
		t.Fatalf("parseCuringYAML: %v", err)
	}
	if def.Name != "pr-review" {
		t.Errorf("Name: got %q", def.Name)
	}
	if def.Agent != "pr-review" {
		t.Errorf("Agent: got %q", def.Agent)
	}
	if def.Queue != "github-prs" {
		t.Errorf("Queue: got %q", def.Queue)
	}
	// Defaults applied.
	if def.PageSizeBytes != 3800 {
		t.Errorf("PageSizeBytes: got %d, want 3800", def.PageSizeBytes)
	}
	if def.MaxAttempts != 3 {
		t.Errorf("MaxAttempts: got %d, want 3", def.MaxAttempts)
	}
	if def.TimeoutSeconds != 900 {
		t.Errorf("TimeoutSeconds: got %d, want 900", def.TimeoutSeconds)
	}
}

func TestParseCuringYAML_Full(t *testing.T) {
	def, err := parseCuringYAML(fullCuring)
	if err != nil {
		t.Fatalf("parseCuringYAML: %v", err)
	}
	if def.Description != "Review PRs from GitHub" {
		t.Errorf("Description: got %q", def.Description)
	}
	if len(def.HideTypes) != 2 {
		t.Errorf("HideTypes: got %v", def.HideTypes)
	}
	if def.PageSizeBytes != 4096 {
		t.Errorf("PageSizeBytes: got %d", def.PageSizeBytes)
	}
	if def.MaxAttempts != 5 {
		t.Errorf("MaxAttempts: got %d", def.MaxAttempts)
	}
	if def.TimeoutSeconds != 600 {
		t.Errorf("TimeoutSeconds: got %d", def.TimeoutSeconds)
	}
	if def.Output.Notify != "slack" {
		t.Errorf("Output.Notify: got %q", def.Output.Notify)
	}
	if def.Output.Queue != "review-results" {
		t.Errorf("Output.Queue: got %q", def.Output.Queue)
	}
}

func TestParseCuringYAML_ZeroTimeout_Preserved(t *testing.T) {
	def, err := parseCuringYAML(zeroTimeoutCuring)
	if err != nil {
		t.Fatalf("parseCuringYAML: %v", err)
	}
	if def.TimeoutSeconds != 0 {
		t.Errorf("TimeoutSeconds: got %d, want 0 (explicit no-timeout)", def.TimeoutSeconds)
	}
}

func TestParseCuringYAML_MissingName(t *testing.T) {
	src := "agent: foo\nqueue: bar\n"
	if _, err := parseCuringYAML(src); err == nil {
		t.Error("expected error for missing name")
	}
}

func TestParseCuringYAML_MissingAgent(t *testing.T) {
	src := "name: foo\nqueue: bar\n"
	if _, err := parseCuringYAML(src); err == nil {
		t.Error("expected error for missing agent")
	}
}

func TestParseCuringYAML_MissingQueue(t *testing.T) {
	src := "name: foo\nagent: bar\n"
	if _, err := parseCuringYAML(src); err == nil {
		t.Error("expected error for missing queue")
	}
}

func TestParseCuringYAML_AllRequiredMissing(t *testing.T) {
	src := "description: only description\n"
	_, err := parseCuringYAML(src)
	if err == nil {
		t.Fatal("expected combined error")
	}
	msg := err.Error()
	if !strings.Contains(msg, "name") {
		t.Errorf("expected 'name' in error: %v", err)
	}
	if !strings.Contains(msg, "agent") {
		t.Errorf("expected 'agent' in error: %v", err)
	}
	if !strings.Contains(msg, "queue") {
		t.Errorf("expected 'queue' in error: %v", err)
	}
}

func TestLoadDir_Empty(t *testing.T) {
	dir := t.TempDir()
	defs, err := LoadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(defs) != 0 {
		t.Errorf("expected empty, got %d", len(defs))
	}
}

func TestLoadDir_NotExist(t *testing.T) {
	defs, err := LoadDir("/tmp/no_such_leather_curing_dir_xyz")
	if err != nil {
		t.Fatal(err)
	}
	if defs != nil {
		t.Errorf("expected nil, got %v", defs)
	}
}

func TestLoadDir_SkipsNonCuringYAML(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "foo.yaml"), []byte("name: ignored"), 0600)
	os.WriteFile(filepath.Join(dir, "bar.worker.yaml"), []byte("name: ignored"), 0600)
	writeCuringFile(t, dir, "ok.curing.yaml", minimalCuring)

	defs, err := LoadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(defs) != 1 {
		t.Errorf("expected 1 def, got %d", len(defs))
	}
}

func TestLoadDir_SetsSourcePath(t *testing.T) {
	dir := t.TempDir()
	path := writeCuringFile(t, dir, "pr.curing.yaml", minimalCuring)

	defs, err := LoadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(defs) != 1 {
		t.Fatalf("expected 1, got %d", len(defs))
	}
	if defs[0].SourcePath != path {
		t.Errorf("SourcePath: got %q, want %q", defs[0].SourcePath, path)
	}
}

func TestLoadDir_CollectsErrors(t *testing.T) {
	dir := t.TempDir()
	writeCuringFile(t, dir, "bad.curing.yaml", "agent: foo\n") // missing name+queue
	writeCuringFile(t, dir, "ok.curing.yaml", minimalCuring)

	defs, err := LoadDir(dir)
	if err == nil {
		t.Error("expected error for bad file")
	}
	// Good file should still be returned.
	_ = defs
	_ = model.CuringDefinition{}
}

package agent

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/TGPSKI/leather/internal/model"
)

func TestParseFrontMatter_QueueInput(t *testing.T) {
	src := `---
name: drainer
queue_input: intake-queue
---
Body.
`
	fm, _, err := parseFrontMatter(src)
	if err != nil {
		t.Fatalf("parseFrontMatter: %v", err)
	}
	if fm.QueueInput != "intake-queue" {
		t.Errorf("QueueInput = %q, want %q", fm.QueueInput, "intake-queue")
	}
}

func TestLoadFile_QueueInputReachesAgent(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "drainer.agent.md")
	src := "---\nname: drainer\nqueue_input: intake-queue\n---\nBody.\n"
	if err := os.WriteFile(path, []byte(src), 0o600); err != nil {
		t.Fatal(err)
	}
	a, err := LoadFile(path)
	if err != nil {
		t.Fatalf("LoadFile: %v", err)
	}
	if a.QueueInput != "intake-queue" {
		t.Errorf("Agent.QueueInput = %q, want %q", a.QueueInput, "intake-queue")
	}
}

func TestApplyLifecycle_QueueInputPrecedence(t *testing.T) {
	a := model.Agent{QueueInput: "from-frontmatter"}
	applyLifecycle(&a, lifecycleRecord{QueueInput: "from-lifecycle"})
	if a.QueueInput != "from-lifecycle" {
		t.Errorf("QueueInput = %q, want lifecycle to take precedence", a.QueueInput)
	}
	b := model.Agent{QueueInput: "from-frontmatter"}
	applyLifecycle(&b, lifecycleRecord{})
	if b.QueueInput != "from-frontmatter" {
		t.Errorf("QueueInput = %q, want frontmatter preserved when lifecycle unset", b.QueueInput)
	}
}

package agent

import (
	"os"
	"path/filepath"
	"testing"
)

// A turn declaring `clear: true` must parse as a per-turn context reset, alongside the
// existing tools:/skills:/toolsets: declarations, and must not disturb them.
func TestParseTurnClear(t *testing.T) {
	body := `---
name: t
---
System prompt.
---
tools: [get_thing]

Fetch it.
---
clear: true
tools: []

Decide from {{captured}} alone.`
	dir := t.TempDir()
	p := filepath.Join(dir, "t.agent.md")
	if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	a, err := LoadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	if len(a.UserPrompts) != 2 {
		t.Fatalf("turns = %d, want 2", len(a.UserPrompts))
	}
	if len(a.TurnClear) != 2 || a.TurnClear[0] || !a.TurnClear[1] {
		t.Fatalf("TurnClear = %v, want [false true]", a.TurnClear)
	}
	// clear: must not swallow the turn's other declarations or its prompt text
	if got := a.TurnTools[1]; got == nil || len(got) != 0 {
		t.Fatalf("turn 1 tools = %v, want an empty non-nil slice (tools withheld)", got)
	}
	if a.UserPrompts[1] != "Decide from {{captured}} alone." {
		t.Fatalf("turn 1 prompt = %q", a.UserPrompts[1])
	}
}

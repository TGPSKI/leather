package runner

import (
	"context"
	"strings"
	"testing"

	"github.com/TGPSKI/leather/internal/model"
	"github.com/TGPSKI/leather/internal/session"
	"github.com/TGPSKI/leather/internal/tool"
)

// An agent whose skill reference resolves to nothing used to run anyway, with
// its prompt still naming tools the tool-calling API never received — and the
// most probable completion is to narrate the calls (issue #72). The run must
// now fail before the first LLM call.
func TestRunner_UnresolvedSkillFailsBeforeAnyLLMCall(t *testing.T) {
	mock := session.NewMockLLM(session.MockConfig{Response: "invented tool output"})
	r := &Runner{
		Client:        mock,
		Registry:      loadTestRegistry(t),
		Log:           testLogger(t),
		MaxToolRounds: 5,
	}

	a := testAgent("ghost-caller")
	a.UserPrompt = "do the thing"
	a.Skills = []string{"not-a-skill"}

	rec, err := r.Run(context.Background(), a, testBudget())
	if err == nil {
		t.Fatal("run succeeded with an unresolvable skill reference")
	}
	if !strings.Contains(err.Error(), "not-a-skill") {
		t.Errorf("error %q does not name the dangling reference", err)
	}
	if rec.Status != model.JobStatusError {
		t.Errorf("status = %q, want error", rec.Status)
	}
	if mock.CallCount() != 0 {
		t.Errorf("LLM call count = %d, want 0 — the guard must fire first", mock.CallCount())
	}
}

// The same guard covers per-turn scopes, which are resolved mid-run and so used
// to fail silently only on the turn that named them.
func TestRunner_UnresolvedTurnToolsetFails(t *testing.T) {
	mock := session.NewMockLLM(session.MockConfig{Response: "ok"})
	r := &Runner{
		Client:        mock,
		Registry:      loadTestRegistry(t),
		Log:           testLogger(t),
		MaxToolRounds: 5,
	}

	a := testAgent("turn-scoped")
	a.UserPrompts = []string{"first", "second"}
	a.TurnToolsets = [][]string{nil, {"not-a-toolset"}}

	_, err := r.Run(context.Background(), a, testBudget())
	if err == nil {
		t.Fatal("run succeeded with an unresolvable per-turn toolset")
	}
	if !strings.Contains(err.Error(), "turn 1") {
		t.Errorf("error %q does not name the offending turn", err)
	}
	if mock.CallCount() != 0 {
		t.Errorf("LLM call count = %d, want 0", mock.CallCount())
	}
}

// An agent that names nothing is a valid tool-less agent and still runs.
func TestRunner_NoScopeStillRuns(t *testing.T) {
	mock := session.NewMockLLM(session.MockConfig{Response: "hello"})
	r := &Runner{
		Client:        mock,
		Registry:      loadTestRegistry(t),
		Log:           testLogger(t),
		MaxToolRounds: 5,
	}
	a := testAgent("plain")
	a.UserPrompt = "hi"
	if _, err := r.Run(context.Background(), a, testBudget()); err != nil {
		t.Fatalf("tool-less agent failed: %v", err)
	}
}

// loadTestRegistry returns a registry holding one real skill, so a dangling
// reference is distinguishable from an empty registry.
func loadTestRegistry(t *testing.T) *tool.Registry {
	t.Helper()
	reg := tool.NewRegistry()
	if err := reg.Register(model.Skill{
		Name:  "real",
		Tools: []model.ToolDefinition{{Name: "real-tool", Type: "http"}},
	}); err != nil {
		t.Fatal(err)
	}
	return reg
}

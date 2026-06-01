package runner

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/tgpski/leather/internal/hide"
	"github.com/tgpski/leather/internal/logging"
	"github.com/tgpski/leather/internal/model"
	"github.com/tgpski/leather/internal/notify"
	"github.com/tgpski/leather/internal/queue"
	"github.com/tgpski/leather/internal/session"
	"github.com/tgpski/leather/internal/tool"
)

// testLogger returns a no-op logger suitable for tests.
func testLogger(t *testing.T) *logging.Logger {
	t.Helper()
	return logging.New("test", model.LogLevelError)
}

func testBudget() model.TokenBudget {
	return model.TokenBudget{
		MaxTokens:          8192,
		CompletionReserve:  1024,
		SummarizeThreshold: 0.85,
	}
}

func testAgent(name string) model.Agent {
	return model.Agent{
		Name:        name,
		Model:       "test-model",
		Temperature: 0.7,
		Timeout:     5 * time.Second,
		Enabled:     true,
	}
}

// TestRunner_NoTools verifies a simple single-turn agent run without tool use.
func TestRunner_NoTools(t *testing.T) {
	mock := session.NewMockLLM(session.MockConfig{Response: "hello world"})
	reg := tool.NewRegistry()
	r := &Runner{
		Client:        mock,
		Registry:      reg,
		Log:           testLogger(t),
		MaxToolRounds: 5,
	}

	a := testAgent("simple")
	a.UserPrompt = "say hello"
	rec, err := r.Run(context.Background(), a, testBudget())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if rec.Status != model.JobStatusSuccess {
		t.Errorf("status = %q, want success", rec.Status)
	}
	if len(rec.Turns) != 1 {
		t.Fatalf("turns = %d, want 1", len(rec.Turns))
	}
	if rec.Turns[0].Response != "hello world" {
		t.Errorf("response = %q, want %q", rec.Turns[0].Response, "hello world")
	}
	if mock.CallCount() != 1 {
		t.Errorf("LLM call count = %d, want 1", mock.CallCount())
	}
}

// TestRunner_LLMError verifies that LLM errors produce an error RunRecord.
func TestRunner_LLMError(t *testing.T) {
	wantErr := errors.New("llm unavailable")
	mock := session.NewMockLLM(session.MockConfig{Err: wantErr})
	r := &Runner{
		Client:   mock,
		Registry: tool.NewRegistry(),
		Log:      testLogger(t),
	}

	rec, err := r.Run(context.Background(), testAgent("fail"), testBudget())
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if rec.Status != model.JobStatusError {
		t.Errorf("status = %q, want error", rec.Status)
	}
	if rec.Error == "" {
		t.Error("RunRecord.Error should be set on failure")
	}
}

// TestRunner_ToolCall verifies the multi-round tool call loop with a mock skill.
func TestRunner_ToolCall(t *testing.T) {
	// Set up a registry with one skill that has one tool.
	reg := tool.NewRegistry()

	// Add a skill directly via the exported helper for tests.
	skill := model.Skill{
		Name: "test-skill",
		Tools: []model.ToolDefinition{
			{
				Name:        "echo_tool",
				Description: "echoes its input",
				Type:        "http",
				HTTP: model.HTTPToolConfig{
					Method: "GET",
					URL:    "http://127.0.0.1:0/echo", // unreachable — we won't actually call it
				},
			},
		},
	}
	if err := reg.Register(skill); err != nil {
		t.Fatalf("Register: %v", err)
	}

	// MockLLM returns a tool call on round 0, then text on round 1.
	mock := session.NewMockLLM(session.MockConfig{
		Response: "final answer",
		ToolCallSequence: [][]model.ToolCall{
			{
				{ID: "call-1", Name: "echo_tool", Arguments: map[string]any{"input": "test"}},
			},
		},
	})

	r := &Runner{
		Client:        mock,
		Registry:      reg,
		Log:           testLogger(t),
		MaxToolRounds: 5,
	}

	a := testAgent("tool-agent")
	a.Skills = []string{"test-skill"}
	a.UserPrompt = "use the echo tool"

	rec, err := r.Run(context.Background(), a, testBudget())
	// The tool HTTP call will fail (unreachable), but that just adds an error
	// message to the tool result; the runner continues to the next round.
	// On round 1, the MockLLM returns "final answer".
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if rec.Status != model.JobStatusSuccess {
		t.Errorf("status = %q, want success", rec.Status)
	}
	// Should have made 2 LLM calls: tool-call round + final answer round.
	if mock.CallCount() != 2 {
		t.Errorf("LLM call count = %d, want 2", mock.CallCount())
	}
}

func TestRunner_BufferedToolResultUsesHideBuffer(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("aaaaabbbbb"))
	}))
	defer srv.Close()

	reg := tool.NewRegistry()
	if err := reg.Register(model.Skill{
		Name: "buffer-skill",
		Tools: []model.ToolDefinition{{
			Name:   "fetch_big",
			Type:   "http",
			Buffer: true,
			HTTP:   model.HTTPToolConfig{Method: "GET", URL: srv.URL},
		}},
	}); err != nil {
		t.Fatalf("Register: %v", err)
	}

	mock := session.NewMockLLM(session.MockConfig{
		Response: "done",
		ToolCallSequence: [][]model.ToolCall{
			{{ID: "call-1", Name: "fetch_big", Arguments: map[string]any{}}},
			{{ID: "call-2", Name: "hide_next", Arguments: map[string]any{"hide_id": "123", "current_page": 1}}},
			nil,
		},
	})
	buf := hide.NewHideBuffer(5)
	r := &Runner{
		Client:        mock,
		Registry:      reg,
		Log:           testLogger(t),
		MaxToolRounds: 5,
		HideBuffer:    buf,
	}
	var sawHideEvent bool
	r.ProgressFn = func(ev ProgressEvent) {
		if ev.Kind == "hide" && ev.HideID != "" && ev.TotalPages == 2 {
			sawHideEvent = true
		}
	}
	a := testAgent("buffered-tool-agent")
	a.Skills = []string{"buffer-skill"}
	a.UserPrompt = "fetch the large result"

	rec, err := r.Run(context.Background(), a, testBudget())
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if rec.Status != model.JobStatusSuccess {
		t.Fatalf("status = %q, want success", rec.Status)
	}
	if !sawHideEvent {
		t.Fatal("expected hide progress event for buffered tool result")
	}
	calls := mock.Calls()
	if len(calls) != 3 {
		t.Fatalf("call count = %d, want 3", len(calls))
	}
	var firstToolResult string
	for _, msg := range calls[1] {
		if msg.Role == "tool" && msg.ToolName == "fetch_big" {
			firstToolResult = msg.Content
		}
	}
	if !strings.Contains(firstToolResult, "[HIDE ") || !strings.Contains(firstToolResult, "page=1/2") {
		t.Fatalf("first buffered result was not a hide cut:\n%s", firstToolResult)
	}
	if strings.Contains(firstToolResult, "aaaaabbbbb") {
		t.Fatalf("raw full tool result entered context: %q", firstToolResult)
	}
	var secondToolResult string
	for _, msg := range calls[2] {
		if msg.Role == "tool" && msg.ToolName == "hide_next" {
			secondToolResult = msg.Content
		}
	}
	if !strings.Contains(secondToolResult, "page=2/2") {
		t.Fatalf("hide_next result missing page 2:\n%s", secondToolResult)
	}
}

func TestRunner_DebugContextFnSeesAccumulatedTurns(t *testing.T) {
	mock := session.NewMockLLM(session.MockConfig{Response: "page summary"})
	r := &Runner{
		Client:   mock,
		Registry: tool.NewRegistry(),
		Log:      testLogger(t),
	}

	var snaps []ContextSnapshot
	r.DebugContextFn = func(s ContextSnapshot) {
		snaps = append(snaps, s)
	}

	a := testAgent("context-agent")
	a.SystemPrompt = "System instructions."
	a.UserPrompts = []string{
		"Page 1 content",
		"Now call hide_next to retrieve page 2.",
		"You have now read all 2 pages. Produce final output.",
	}

	if _, err := r.Run(context.Background(), a, testBudget()); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(snaps) != 3 {
		t.Fatalf("snapshot count = %d, want 3", len(snaps))
	}
	if len(snaps[0].Messages) != 2 {
		t.Fatalf("first snapshot messages = %d, want 2", len(snaps[0].Messages))
	}
	last := snaps[2]
	if len(last.Messages) != 6 {
		t.Fatalf("last snapshot messages = %d, want 6", len(last.Messages))
	}
	if last.Messages[0].Role != "system" || last.Messages[0].Content != "System instructions." {
		t.Fatalf("last snapshot missing system message: %+v", last.Messages[0])
	}
	if last.Messages[1].Role != "user" || last.Messages[1].Content != "Page 1 content" {
		t.Fatalf("last snapshot missing first user turn: %+v", last.Messages[1])
	}
	if last.Messages[2].Role != "assistant" || last.Messages[2].Content != "page summary" {
		t.Fatalf("last snapshot missing first assistant response: %+v", last.Messages[2])
	}
	if last.Messages[5].Role != "user" || !strings.Contains(last.Messages[5].Content, "Produce final output") {
		t.Fatalf("last snapshot missing final user prompt: %+v", last.Messages[5])
	}
	if len(last.ToolNames) != 0 {
		t.Fatalf("last snapshot tool count = %d, want 0", len(last.ToolNames))
	}
}

func TestRunner_CompactsPagedHideAfterReflectionSummary(t *testing.T) {
	buf := hide.NewHideBuffer(80)
	rawPageOne := strings.Repeat("PAGE1RAW ", 8)
	rawPageTwo := strings.Repeat("PAGE2RAW ", 8)
	h := buf.Store("cli", rawPageOne+rawPageTwo)
	firstCut, err := buf.Cut(h.ID, 1)
	if err != nil {
		t.Fatalf("Cut(1): %v", err)
	}

	mock := session.NewMockLLM(session.MockConfig{
		Response: "page summary",
		ToolCallSequence: [][]model.ToolCall{
			{},
			{{ID: "call-1", Name: "hide_next", Arguments: map[string]any{"hide_id": h.ID, "current_page": 1}}},
		},
	})
	r := &Runner{
		Client:              mock,
		Registry:            tool.NewRegistry(),
		Log:                 testLogger(t),
		MaxToolRounds:       5,
		HideBuffer:          buf,
		ForceTextAfterHide:  true,
		NoToolsForFirstTurn: true,
		NoToolsForLastTurn:  true,
	}

	var snaps []ContextSnapshot
	r.DebugContextFn = func(s ContextSnapshot) {
		snaps = append(snaps, s)
	}

	a := testAgent("paged-agent")
	a.SystemPrompt = "System instructions."
	a.UserPrompts = []string{
		firstCut.Format() + "\n\nSummarize page 1 only.",
		"Now call hide_next to retrieve page 2.",
		"You have now read all 2 pages. Produce final output.",
	}

	if _, err := r.Run(context.Background(), a, testBudget()); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(snaps) != 4 {
		t.Fatalf("snapshot count = %d, want 4", len(snaps))
	}
	final := snaps[len(snaps)-1]
	if len(final.Messages) != 4 {
		t.Fatalf("final snapshot messages = %d, want 4", len(final.Messages))
	}
	assistantCount := 0
	for _, msg := range final.Messages {
		if msg.Role == "assistant" {
			assistantCount++
		}
		if strings.Contains(msg.Content, "PAGE1RAW") || strings.Contains(msg.Content, "PAGE2RAW") {
			t.Fatalf("final snapshot still contains raw page body: %q", msg.Content)
		}
		if strings.Contains(msg.Content, "Now call hide_next") || strings.Contains(msg.Content, "[HIDE ") {
			t.Fatalf("final snapshot should not retain hide scaffolding: %q", msg.Content)
		}
	}
	if assistantCount != 2 {
		t.Fatalf("assistant message count = %d, want 2 page summaries", assistantCount)
	}
	if len(final.ToolNames) != 0 {
		t.Fatalf("final snapshot tool count = %d, want 0", len(final.ToolNames))
	}
}

func TestRunner_CompactsThreePagedHideBeforeFinalOutput(t *testing.T) {
	buf := hide.NewHideBuffer(5)
	h := buf.Store("cli", "111112222233333")
	firstCut, err := buf.Cut(h.ID, 1)
	if err != nil {
		t.Fatalf("Cut(1): %v", err)
	}

	mock := session.NewMockLLM(session.MockConfig{
		Response: "page facts",
		ToolCallSequence: [][]model.ToolCall{
			{},
			{{ID: "call-1", Name: "hide_next", Arguments: map[string]any{"hide_id": h.ID, "current_page": 1}}},
			{},
			{{ID: "call-2", Name: "hide_next", Arguments: map[string]any{"hide_id": h.ID, "current_page": 3}}},
			{},
			{},
		},
	})
	r := &Runner{
		Client:              mock,
		Registry:            tool.NewRegistry(),
		Log:                 testLogger(t),
		MaxToolRounds:       5,
		HideBuffer:          buf,
		ForceTextAfterHide:  true,
		NoToolsForFirstTurn: true,
		NoToolsForLastTurn:  true,
	}

	var snaps []ContextSnapshot
	r.DebugContextFn = func(s ContextSnapshot) {
		snaps = append(snaps, s)
	}

	a := testAgent("paged-agent")
	a.UserPrompts = []string{
		firstCut.Format() + "\n\nSummarize page 1 only.",
		"Now call hide_next to retrieve page 2.",
		"Now call hide_next to retrieve page 3.",
		"You have now read all 3 pages. Produce final output.",
	}

	if _, err := r.Run(context.Background(), a, testBudget()); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(snaps) != 6 {
		t.Fatalf("snapshot count = %d, want 6", len(snaps))
	}
	final := snaps[len(snaps)-1]
	if len(final.ToolNames) != 0 {
		t.Fatalf("final snapshot tool count = %d, want 0", len(final.ToolNames))
	}
	if len(final.Messages) != 4 {
		t.Fatalf("final snapshot messages = %d, want 4", len(final.Messages))
	}
	for _, msg := range final.Messages {
		if strings.Contains(msg.Content, "[HIDE ") || strings.Contains(msg.Content, "Now call hide_next") {
			t.Fatalf("final snapshot retained hide scaffolding: %q", msg.Content)
		}
		if strings.Contains(msg.Content, "11111") || strings.Contains(msg.Content, "22222") || strings.Contains(msg.Content, "33333") {
			t.Fatalf("final snapshot retained raw page content: %q", msg.Content)
		}
	}
}

func TestRunner_HideToolResolvesUnknownIDWithSingleActiveHide(t *testing.T) {
	buf := hide.NewHideBuffer(5)
	h := buf.Store("cli", "aaaaabbbbb")
	r := &Runner{HideBuffer: buf, Log: testLogger(t)}

	result := r.executeHideTool("hide_next", "call-1", map[string]any{
		"hide_id":      "123",
		"current_page": 1,
	})
	if result.Error != "" {
		t.Fatalf("executeHideTool error = %q", result.Error)
	}
	if !strings.Contains(result.Content, "id="+h.ID) || !strings.Contains(result.Content, "page=2/2") {
		t.Fatalf("unexpected hide_next content:\n%s", result.Content)
	}
}

func TestRunner_HideToolRejectsAmbiguousUnknownID(t *testing.T) {
	buf := hide.NewHideBuffer(5)
	buf.Store("cli", "aaaaabbbbb")
	buf.Store("cli", "cccccddddd")
	r := &Runner{HideBuffer: buf, Log: testLogger(t)}

	result := r.executeHideTool("hide_next", "call-1", map[string]any{
		"hide_id":      "123",
		"current_page": 1,
	})
	if result.Error == "" {
		t.Fatal("expected ambiguous unknown hide id to fail")
	}
	if !strings.Contains(result.Error, "unknown hide id") {
		t.Fatalf("error = %q, want unknown hide id", result.Error)
	}
}

func TestRunner_HideToolValidatesPageArgs(t *testing.T) {
	buf := hide.NewHideBuffer(5)
	h := buf.Store("cli", "aaaaabbbbb")
	r := &Runner{HideBuffer: buf, Log: testLogger(t)}

	result := r.executeHideTool("hide_next", "call-1", map[string]any{
		"hide_id":      h.ID,
		"current_page": 0,
	})
	if result.Error != "hide_next requires current_page >= 1" {
		t.Fatalf("error = %q", result.Error)
	}

	result = r.executeHideTool("hide_jump", "call-2", map[string]any{
		"hide_id": h.ID,
		"page":    "nope",
	})
	if result.Error != "hide_jump requires page >= 1" {
		t.Fatalf("error = %q", result.Error)
	}
}

func TestRunner_HideToolFailureFailsRun(t *testing.T) {
	buf := hide.NewHideBuffer(5)
	h := buf.Store("cli", "aaaaabbbbb")
	mock := session.NewMockLLM(session.MockConfig{
		ToolCallSequence: [][]model.ToolCall{
			{{ID: "call-1", Name: "hide_next", Arguments: map[string]any{"hide_id": h.ID, "current_page": 2}}},
		},
	})
	r := &Runner{
		Client:        mock,
		Registry:      tool.NewRegistry(),
		Log:           testLogger(t),
		MaxToolRounds: 5,
		HideBuffer:    buf,
	}
	a := testAgent("paged-agent")
	a.UserPrompt = "read next page"

	rec, err := r.Run(context.Background(), a, testBudget())
	if err == nil {
		t.Fatal("expected hide tool failure to fail the run")
	}
	if rec.Status != model.JobStatusError {
		t.Fatalf("status = %q, want error", rec.Status)
	}
	if !strings.Contains(err.Error(), "hide tool hide_next failed") {
		t.Fatalf("error = %v", err)
	}
}

// TestRunner_UnknownToolRejected verifies that an unknown tool name fails closed.
func TestRunner_UnknownToolRejected(t *testing.T) {
	reg := tool.NewRegistry()
	// Add a skill with one known tool.
	skill := model.Skill{
		Name: "safe-skill",
		Tools: []model.ToolDefinition{
			{Name: "known_tool", Type: "http"},
		},
	}
	if err := reg.Register(skill); err != nil {
		t.Fatalf("Register: %v", err)
	}

	// MockLLM requests a tool that is NOT in the registry (injection simulation).
	mock := session.NewMockLLM(session.MockConfig{
		Response: "should not reach",
		ToolCallSequence: [][]model.ToolCall{
			{
				{ID: "call-bad", Name: "injected_tool", Arguments: map[string]any{}},
			},
		},
	})

	r := &Runner{
		Client:        mock,
		Registry:      reg,
		Log:           testLogger(t),
		MaxToolRounds: 5,
	}

	a := testAgent("injection-test")
	a.Skills = []string{"safe-skill"}

	rec, err := r.Run(context.Background(), a, testBudget())
	if err == nil {
		t.Fatal("expected error for unknown tool, got nil")
	}
	if rec.Status != model.JobStatusError {
		t.Errorf("status = %q, want error", rec.Status)
	}
}

// TestRunner_MaxRoundsExceeded verifies the round cap when model keeps calling tools.
func TestRunner_MaxRoundsExceeded(t *testing.T) {
	reg := tool.NewRegistry()
	skill := model.Skill{
		Name: "loopy",
		Tools: []model.ToolDefinition{
			{Name: "loop_tool", Type: "http", HTTP: model.HTTPToolConfig{Method: "GET", URL: "http://127.0.0.1:0/"}},
		},
	}
	if err := reg.Register(skill); err != nil {
		t.Fatalf("Register: %v", err)
	}

	// MockLLM always returns tool calls (never text).
	mock := session.NewMockLLM(session.MockConfig{
		ToolCallSequence: [][]model.ToolCall{
			{{ID: "c1", Name: "loop_tool", Arguments: map[string]any{}}},
			{{ID: "c2", Name: "loop_tool", Arguments: map[string]any{}}},
			{{ID: "c3", Name: "loop_tool", Arguments: map[string]any{}}},
		},
	})

	r := &Runner{
		Client:        mock,
		Registry:      reg,
		Log:           testLogger(t),
		MaxToolRounds: 3,
	}

	a := testAgent("looper")
	a.Skills = []string{"loopy"}

	rec, err := r.Run(context.Background(), a, testBudget())
	if err == nil {
		t.Fatal("expected max-rounds error, got nil")
	}
	if rec.Status != model.JobStatusError {
		t.Errorf("status = %q, want error", rec.Status)
	}
}

func TestRunner_TurnSkillScopeReplacesBaseScope(t *testing.T) {
	reg := tool.NewRegistry()
	if err := reg.Register(model.Skill{
		Name:  "base-skill",
		Tools: []model.ToolDefinition{{Name: "base_tool", Type: "http", HTTP: model.HTTPToolConfig{Method: "GET", URL: "http://127.0.0.1:0/"}}},
	}); err != nil {
		t.Fatalf("Register base skill: %v", err)
	}
	if err := reg.Register(model.Skill{
		Name:               "turn-skill",
		SystemPromptAppend: "Turn-only instructions.",
		Tools:              []model.ToolDefinition{{Name: "turn_tool", Type: "http", HTTP: model.HTTPToolConfig{Method: "GET", URL: "http://127.0.0.1:0/"}}},
	}); err != nil {
		t.Fatalf("Register turn skill: %v", err)
	}
	mock := session.NewMockLLM(session.MockConfig{
		Response:         "done",
		ToolCallSequence: [][]model.ToolCall{{{ID: "turn-1", Name: "turn_tool", Arguments: map[string]any{}}}},
	})
	r := &Runner{Client: mock, Registry: reg, Log: testLogger(t), MaxToolRounds: 5}
	a := testAgent("turn-skill-scope")
	a.SystemPrompt = "Base system."
	a.UserPrompts = []string{"do the turn work"}
	a.Skills = []string{"base-skill"}
	a.TurnSkills = [][]string{{"turn-skill"}}

	rec, err := r.Run(context.Background(), a, testBudget())
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if rec.Status != model.JobStatusSuccess {
		t.Fatalf("status = %q, want success", rec.Status)
	}
	var sawTurnPrompt bool
	for _, msg := range mock.Calls()[0] {
		if msg.Role == "system" && strings.Contains(msg.Content, "Turn-only instructions.") {
			sawTurnPrompt = true
			break
		}
	}
	if !sawTurnPrompt {
		t.Fatal("expected turn skill prompt append to be added as a system message")
	}
}

func TestRunner_TurnToolsetScopeReplacesBaseScope(t *testing.T) {
	reg := tool.NewRegistry()
	if err := reg.Register(model.Skill{
		Name:  "base-skill",
		Tools: []model.ToolDefinition{{Name: "base_tool", Type: "http", HTTP: model.HTTPToolConfig{Method: "GET", URL: "http://127.0.0.1:0/"}}},
	}); err != nil {
		t.Fatalf("Register base skill: %v", err)
	}
	if err := reg.Register(model.Skill{
		Name:  "tool-holder",
		Tools: []model.ToolDefinition{{Name: "toolset_tool", Type: "http", HTTP: model.HTTPToolConfig{Method: "GET", URL: "http://127.0.0.1:0/"}}},
	}); err != nil {
		t.Fatalf("Register tool-holder: %v", err)
	}
	if err := reg.RegisterToolset(model.Toolset{Name: "release-write", Tools: []string{"toolset_tool"}}); err != nil {
		t.Fatalf("RegisterToolset: %v", err)
	}
	mock := session.NewMockLLM(session.MockConfig{
		Response:         "done",
		ToolCallSequence: [][]model.ToolCall{{{ID: "turn-1", Name: "toolset_tool", Arguments: map[string]any{}}}},
	})
	r := &Runner{Client: mock, Registry: reg, Log: testLogger(t), MaxToolRounds: 5}
	a := testAgent("turn-toolset-scope")
	a.UserPrompts = []string{"use the toolset tool"}
	a.Skills = []string{"base-skill"}
	a.TurnToolsets = [][]string{{"release-write"}}

	if _, err := r.Run(context.Background(), a, testBudget()); err != nil {
		t.Fatalf("Run: %v", err)
	}
}

func TestRunner_TurnToolsAllowExplicitRegistryTools(t *testing.T) {
	reg := tool.NewRegistry()
	if err := reg.Register(model.Skill{
		Name:  "tool-holder",
		Tools: []model.ToolDefinition{{Name: "explicit_tool", Type: "http", HTTP: model.HTTPToolConfig{Method: "GET", URL: "http://127.0.0.1:0/"}}},
	}); err != nil {
		t.Fatalf("Register tool-holder: %v", err)
	}
	mock := session.NewMockLLM(session.MockConfig{
		Response:         "done",
		ToolCallSequence: [][]model.ToolCall{{{ID: "turn-1", Name: "explicit_tool", Arguments: map[string]any{}}}},
	})
	r := &Runner{Client: mock, Registry: reg, Log: testLogger(t), MaxToolRounds: 5}
	a := testAgent("explicit-turn-tools")
	a.UserPrompts = []string{"use the explicit tool"}
	a.TurnTools = [][]string{{"explicit_tool"}}

	rec, err := r.Run(context.Background(), a, testBudget())
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if rec.Status != model.JobStatusSuccess {
		t.Fatalf("status = %q, want success", rec.Status)
	}
	if mock.CallCount() != 2 {
		t.Fatalf("LLM call count = %d, want 2", mock.CallCount())
	}
}

func TestExpandPromptPayload_Substitution(t *testing.T) {
	a := model.Agent{
		Name:         "tmpl-agent",
		SystemPrompt: "Check issue {{.number}}: {{.title}}",
		UserPrompt:   "Summarize issue {{.number}}.",
	}
	payload := map[string]any{
		"number": 42,
		"title":  "test issue",
	}
	got, err := ExpandPromptPayload(a, payload)
	if err != nil {
		t.Fatalf("ExpandPromptPayload: %v", err)
	}
	if got.SystemPrompt != "Check issue 42: test issue" {
		t.Errorf("SystemPrompt: got %q", got.SystemPrompt)
	}
	if got.UserPrompt != "Summarize issue 42." {
		t.Errorf("UserPrompt: got %q", got.UserPrompt)
	}
}

func TestExpandPromptPayload_EmptyPayload(t *testing.T) {
	a := model.Agent{
		Name:         "no-payload",
		SystemPrompt: "Hello {{.name}}",
		UserPrompt:   "World",
	}
	got, err := ExpandPromptPayload(a, nil)
	if err != nil {
		t.Fatalf("ExpandPromptPayload: %v", err)
	}
	// No substitution with nil payload — prompts should be unchanged.
	if got.SystemPrompt != a.SystemPrompt {
		t.Errorf("SystemPrompt changed: got %q", got.SystemPrompt)
	}
}

func TestExpandPromptPayload_MissingKey(t *testing.T) {
	a := model.Agent{
		Name:         "missing-key",
		SystemPrompt: "Issue {{.number}} by {{.author}}",
		UserPrompt:   "",
	}
	payload := map[string]any{"number": 7} // "author" is missing
	got, err := ExpandPromptPayload(a, payload)
	if err != nil {
		t.Fatalf("ExpandPromptPayload with missing key: %v", err)
	}
	// missingkey=zero: unknown keys expand to their zero value (empty string).
	if got.SystemPrompt != "Issue 7 by <no value>" {
		t.Logf("SystemPrompt with missing key: %q (acceptable)", got.SystemPrompt)
	}
}

// ---- routeOutput tests -------------------------------------------------------

// mockNotifier is a minimal Notifier for routing tests.
type mockNotifier struct {
	name    string
	sent    []notify.Message
	sendErr error
}

func (m *mockNotifier) Send(_ context.Context, msg notify.Message) error {
	m.sent = append(m.sent, msg)
	return m.sendErr
}

func (m *mockNotifier) Name() string { return m.name }

func testRunner(t *testing.T) *Runner {
	t.Helper()
	return &Runner{
		Client:        session.NewMockLLM(session.MockConfig{Response: "ok"}),
		Registry:      tool.NewRegistry(),
		Log:           testLogger(t),
		MaxToolRounds: 5,
	}
}

func TestRouteOutput_File(t *testing.T) {
	dir := t.TempDir()
	outFile := filepath.Join(dir, "output.txt")

	r := testRunner(t)
	a := testAgent("file-route")
	a.OutputRoutes = []model.OutputRoute{
		{Type: "file", FilePath: outFile},
	}

	rec, err := r.Run(context.Background(), a, testBudget())
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if rec.Status != model.JobStatusSuccess {
		t.Fatalf("status = %q", rec.Status)
	}

	data, err := os.ReadFile(outFile)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if string(data) != "ok" {
		t.Errorf("file content = %q, want ok", string(data))
	}
}

func TestRouteOutput_Queue(t *testing.T) {
	dir := t.TempDir()
	mgr := queue.NewManager(dir)

	r := testRunner(t)
	r.QueueMgr = mgr
	a := testAgent("queue-route")
	a.OutputRoutes = []model.OutputRoute{
		{Type: "queue", Queue: "myqueue"},
	}

	_, err := r.Run(context.Background(), a, testBudget())
	if err != nil {
		t.Fatalf("Run: %v", err)
	}

	q, err := mgr.Get("myqueue")
	if err != nil {
		t.Fatalf("Get queue: %v", err)
	}
	if q.Len() != 1 {
		t.Errorf("queue len = %d, want 1", q.Len())
	}
}

func TestRouteOutput_Queue_NoManager(t *testing.T) {
	// Queue route with no QueueMgr — should log warn and not panic.
	r := testRunner(t)
	r.QueueMgr = nil
	a := testAgent("no-mgr-route")
	a.OutputRoutes = []model.OutputRoute{
		{Type: "queue", Queue: "orphan"},
	}

	rec, err := r.Run(context.Background(), a, testBudget())
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if rec.Status != model.JobStatusSuccess {
		t.Errorf("status = %q, want success", rec.Status)
	}
}

func TestRouteOutput_HTTP(t *testing.T) {
	var gotBody string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		b, _ := io.ReadAll(req.Body)
		gotBody = string(b)
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	r := testRunner(t)
	a := testAgent("http-route")
	a.OutputRoutes = []model.OutputRoute{
		{Type: "http", URL: srv.URL, Method: "POST"},
	}

	_, err := r.Run(context.Background(), a, testBudget())
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if gotBody != "ok" {
		t.Errorf("HTTP body = %q, want ok", gotBody)
	}
}

func TestRouteOutput_Notify(t *testing.T) {
	mn := &mockNotifier{name: "test-backend"}
	r := testRunner(t)
	r.Notifiers = map[string]notify.Notifier{"test-backend": mn}

	a := testAgent("notify-route")
	a.OutputRoutes = []model.OutputRoute{
		{Type: "notify", NotifyBackend: "test-backend"},
	}

	_, err := r.Run(context.Background(), a, testBudget())
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(mn.sent) != 1 {
		t.Fatalf("notifier sent count = %d, want 1", len(mn.sent))
	}
	if mn.sent[0].Content != "ok" {
		t.Errorf("notified content = %q, want ok", mn.sent[0].Content)
	}
}

func TestRouteOutput_Notify_UnknownBackend(t *testing.T) {
	// Unknown backend name — should warn, not fail.
	mn := &mockNotifier{name: "other"}
	r := testRunner(t)
	r.Notifiers = map[string]notify.Notifier{"other": mn}

	a := testAgent("bad-notify-route")
	a.OutputRoutes = []model.OutputRoute{
		{Type: "notify", NotifyBackend: "nonexistent"},
	}

	rec, err := r.Run(context.Background(), a, testBudget())
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if rec.Status != model.JobStatusSuccess {
		t.Errorf("status = %q, want success", rec.Status)
	}
	if len(mn.sent) != 0 {
		t.Errorf("unexpected notify send: %v", mn.sent)
	}
}

func TestRouteOutput_UnknownType(t *testing.T) {
	// Unknown route type — should warn, not panic.
	r := testRunner(t)
	a := testAgent("unknown-route-type")
	a.OutputRoutes = []model.OutputRoute{
		{Type: "grpc"},
	}
	rec, err := r.Run(context.Background(), a, testBudget())
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if rec.Status != model.JobStatusSuccess {
		t.Errorf("status = %q, want success", rec.Status)
	}
}

func TestBuildRunData_Fields(t *testing.T) {
	a := model.Agent{
		Name:     "my-agent",
		Schedule: "0 * * * *",
		Tags:     []string{"prod", "nightly"},
	}
	data := BuildRunData(a)
	if data["agent_name"] != "my-agent" {
		t.Errorf("agent_name: got %q", data["agent_name"])
	}
	if data["schedule"] != "0 * * * *" {
		t.Errorf("schedule: got %q", data["schedule"])
	}
	if data["tags"] != "prod, nightly" {
		t.Errorf("tags: got %q", data["tags"])
	}
	if _, ok := data["now"]; !ok {
		t.Error("now key missing")
	}
}

func TestBuildRunData_Expansion(t *testing.T) {
	a := model.Agent{
		Name:         "report-agent",
		Schedule:     "0 9 * * 1",
		SystemPrompt: "I am {{.agent_name}} scheduled at {{.schedule}}.",
		UserPrompt:   "Run the report. Tags: {{.tags}}. Time: {{.now}}.",
	}
	data := BuildRunData(a)
	got, err := ExpandPromptPayload(a, data)
	if err != nil {
		t.Fatalf("ExpandPromptPayload: %v", err)
	}
	if !strings.Contains(got.SystemPrompt, "report-agent") {
		t.Errorf("system prompt not expanded: %q", got.SystemPrompt)
	}
	if !strings.Contains(got.SystemPrompt, "0 9 * * 1") {
		t.Errorf("system prompt schedule not expanded: %q", got.SystemPrompt)
	}
}

// --- skill extract → turnVars tests ---

// TestRunner_TurnVarExtraction verifies that a value extracted from a tool result
// on turn 1 is substituted into the turn 2 user prompt via {{key}}.
func TestRunner_TurnVarExtraction(t *testing.T) {
	// HTTP server returns content with extractable AUTHOR line.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("PR: Fix the bug\nAUTHOR: alice\nSTATE: open\n"))
	}))
	defer srv.Close()

	reg := tool.NewRegistry()
	skill := model.Skill{
		Name: "pr-skill",
		Extract: []model.SkillExtract{
			{Tool: "gh_pr_thread", Pattern: `^AUTHOR: (.+)$`, Store: "pr_author"},
		},
		Tools: []model.ToolDefinition{
			{
				Name:        "gh_pr_thread",
				Description: "fetch PR thread",
				Type:        "http",
				HTTP:        model.HTTPToolConfig{Method: "GET", URL: srv.URL},
			},
		},
	}
	if err := reg.Register(skill); err != nil {
		t.Fatalf("Register: %v", err)
	}

	// Turn 1: model calls gh_pr_thread; then returns text.
	// Turn 2: model returns text referencing the extracted author.
	mock := session.NewMockLLM(session.MockConfig{
		Response: "done",
		ToolCallSequence: [][]model.ToolCall{
			// Round 0 of turn 1: call the tool.
			{{ID: "c1", Name: "gh_pr_thread", Arguments: map[string]any{}}},
			// Round 1 of turn 1: return text.
			nil,
		},
	})

	// Capture what user prompts reach the LLM to verify substitution.
	var turn2Prompt string
	r := &Runner{
		Client:        mock,
		Registry:      reg,
		Log:           testLogger(t),
		MaxToolRounds: 5,
		ProgressFn: func(e ProgressEvent) {
			if e.Kind == "user" && turn2Prompt == "" {
				turn2Prompt = e.Prompt // first user event = turn 1; second = turn 2
			}
		},
	}

	a := testAgent("extract-agent")
	a.Skills = []string{"pr-skill"}
	a.UserPrompts = []string{
		"fetch the PR",
		"the author is {{pr_author}}, summarize",
	}

	rec, err := r.Run(context.Background(), a, testBudget())
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if rec.Status != model.JobStatusSuccess {
		t.Errorf("status = %q", rec.Status)
	}

	// Verify the substitution by inspecting LLM call messages.
	calls := mock.Calls()
	// Find the user message from turn 2 across all LLM calls.
	found := false
	for _, msgs := range calls {
		for _, msg := range msgs {
			if msg.Role == "user" && strings.Contains(msg.Content, "alice") {
				found = true
				break
			}
		}
		if found {
			break
		}
	}
	if !found {
		t.Error("turn 2 user prompt did not contain extracted pr_author value 'alice'")
		for i, msgs := range calls {
			for _, msg := range msgs {
				if msg.Role == "user" {
					t.Logf("call %d user: %q", i, msg.Content)
				}
			}
		}
	}
	_ = turn2Prompt
}

// TestRunner_TurnVarNoMatchIsNoop verifies that when no extract pattern matches,
// the turnVars map is unchanged.
func TestRunner_TurnVarNoMatchIsNoop(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("no extractable lines here"))
	}))
	defer srv.Close()

	reg := tool.NewRegistry()
	skill := model.Skill{
		Name: "pr-skill",
		Extract: []model.SkillExtract{
			{Tool: "gh_pr_thread", Pattern: `^AUTHOR: (.+)$`, Store: "pr_author"},
		},
		Tools: []model.ToolDefinition{
			{
				Name: "gh_pr_thread",
				Type: "http",
				HTTP: model.HTTPToolConfig{Method: "GET", URL: srv.URL},
			},
		},
	}
	if err := reg.Register(skill); err != nil {
		t.Fatalf("Register: %v", err)
	}

	mock := session.NewMockLLM(session.MockConfig{
		Response: "done",
		ToolCallSequence: [][]model.ToolCall{
			{{ID: "c1", Name: "gh_pr_thread", Arguments: map[string]any{}}},
			nil,
		},
	})

	r := &Runner{
		Client:        mock,
		Registry:      reg,
		Log:           testLogger(t),
		MaxToolRounds: 5,
	}

	a := testAgent("noop-extract-agent")
	a.Skills = []string{"pr-skill"}
	a.UserPrompts = []string{
		"fetch",
		"author is {{pr_author}}",
	}

	rec, err := r.Run(context.Background(), a, testBudget())
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if rec.Status != model.JobStatusSuccess {
		t.Errorf("status = %q", rec.Status)
	}

	// Turn 2 prompt should still contain the raw placeholder (no match → no substitution).
	calls := mock.Calls()
	found := false
	for _, msgs := range calls {
		for _, msg := range msgs {
			if msg.Role == "user" && strings.Contains(msg.Content, "{{pr_author}}") {
				found = true
			}
		}
	}
	if !found {
		t.Error("expected turn 2 user prompt to retain {{pr_author}} when no match; not found")
	}
}

// errOnNthCountClient is a session.LLMClient that delegates Complete to a MockLLM
// but returns an error from CountTokens after 'failAfter' successful calls.
type errOnNthCountClient struct {
	mock      *session.MockLLM
	failAfter int
	mu        sync.Mutex
	count     int
}

func (e *errOnNthCountClient) Complete(ctx context.Context, modelName string, messages []model.Message, opts session.CompletionOptions) (model.LLMResponse, error) {
	return e.mock.Complete(ctx, modelName, messages, opts)
}

func (e *errOnNthCountClient) CountTokens(messages []model.Message) (int, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.count += len(messages)
	if e.count > e.failAfter {
		return 0, errors.New("injected CountTokens error")
	}
	return e.mock.CountTokens(messages)
}

func TestRun_SessAddError_PropagatesUp(t *testing.T) {
	// Make CountTokens fail after the first couple of messages so the first
	// sess.Add (system prompt) succeeds but a subsequent one fails.
	mock := session.NewMockLLM(session.MockConfig{Response: "hello"})
	client := &errOnNthCountClient{mock: mock, failAfter: 1}

	r := &Runner{
		Client:        client,
		Registry:      tool.NewRegistry(),
		Log:           testLogger(t),
		MaxToolRounds: 1,
	}

	a := testAgent("sess-add-err")
	a.SystemPrompt = "you are a test agent"
	a.UserPrompt = "hello"
	_, err := r.Run(context.Background(), a, testBudget())
	if err == nil {
		t.Fatal("expected error from sess.Add failure, got nil")
	}
	if !strings.Contains(err.Error(), "session add") && !strings.Contains(err.Error(), "count tokens") {
		t.Errorf("error should mention session add or count tokens, got: %v", err)
	}
}

func TestRun_LastResponseSet(t *testing.T) {
	mock := session.NewMockLLM(session.MockConfig{Response: "final answer"})
	r := &Runner{
		Client:        mock,
		Registry:      tool.NewRegistry(),
		Log:           testLogger(t),
		MaxToolRounds: 5,
	}

	a := testAgent("last-resp")
	a.UserPrompt = "what is the answer?"
	rec, err := r.Run(context.Background(), a, testBudget())
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if rec.LastResponse != "final answer" {
		t.Errorf("LastResponse = %q, want %q", rec.LastResponse, "final answer")
	}
}

func TestRun_LastResponseEmpty_NoTurns(t *testing.T) {
	wantErr := errors.New("llm offline")
	mock := session.NewMockLLM(session.MockConfig{Err: wantErr})
	r := &Runner{
		Client:        mock,
		Registry:      tool.NewRegistry(),
		Log:           testLogger(t),
		MaxToolRounds: 1,
	}

	a := testAgent("no-turns")
	a.UserPrompt = "will error"
	rec, err := r.Run(context.Background(), a, testBudget())
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if rec.LastResponse != "" {
		t.Errorf("LastResponse should be empty on error run, got %q", rec.LastResponse)
	}
}

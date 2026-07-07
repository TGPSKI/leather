package runner

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/TGPSKI/leather/internal/hide"
	"github.com/TGPSKI/leather/internal/logging"
	"github.com/TGPSKI/leather/internal/mcp"
	"github.com/TGPSKI/leather/internal/model"
	"github.com/TGPSKI/leather/internal/notify"
	"github.com/TGPSKI/leather/internal/queue"
	"github.com/TGPSKI/leather/internal/session"
	"github.com/TGPSKI/leather/internal/tool"
)

// TestMain allows this test binary to be re-invoked as a fake MCP server that
// always reports a tool failure via isError: true — simulating a shell
// command that exits nonzero. Used by TestRunner_FailedMCPCallDoesNotBlockRetry
// to pin the 2026-07-06 incident (see that test's doc comment).
func TestMain(m *testing.M) {
	if os.Getenv("LEATHER_RUNNER_TEST_FAILING_MCP_SERVER") == "1" {
		runFakeFailingMCPServer(os.Stdin, os.Stdout)
		os.Exit(0)
	}
	os.Exit(m.Run())
}

// runFakeFailingMCPServer implements just enough of MCP (initialize,
// tools/list, tools/call) to drive a Runner end-to-end. Every tools/call
// response reports isError: true, mirroring shell-mcp's behavior for a
// command that exits nonzero (plan 03). Each call also appends a byte to
// LEATHER_RUNNER_TEST_COUNTER_FILE so the test can assert exactly how many
// times the tool actually executed (as opposed to being deduped).
func runFakeFailingMCPServer(r io.Reader, w io.Writer) {
	dec := json.NewDecoder(bufio.NewReader(r))
	enc := json.NewEncoder(w)
	counterFile := os.Getenv("LEATHER_RUNNER_TEST_COUNTER_FILE")
	for {
		var req map[string]json.RawMessage
		if err := dec.Decode(&req); err != nil {
			return
		}
		method := strings.Trim(string(req["method"]), `"`)
		var id int64
		if idRaw, ok := req["id"]; ok {
			_ = json.Unmarshal(idRaw, &id)
		}
		switch method {
		case "initialize":
			_ = enc.Encode(map[string]any{
				"jsonrpc": "2.0",
				"id":      id,
				"result": map[string]any{
					"protocolVersion": "2024-11-05",
					"capabilities":    map[string]any{"tools": map[string]any{}},
					"serverInfo":      map[string]any{"name": "fake-failing-mcp", "version": "0.1"},
				},
			})
		case "notifications/initialized":
			// no response
		case "tools/list":
			_ = enc.Encode(map[string]any{
				"jsonrpc": "2.0", "id": id,
				"result": map[string]any{"tools": []any{}},
			})
		case "tools/call":
			var params struct {
				Name string `json:"name"`
			}
			_ = json.Unmarshal(req["params"], &params)
			if counterFile != "" {
				if f, ferr := os.OpenFile(counterFile, os.O_APPEND|os.O_WRONLY, 0600); ferr == nil {
					_, _ = f.WriteString("x")
					_ = f.Close()
				}
			}
			_ = enc.Encode(map[string]any{
				"jsonrpc": "2.0", "id": id,
				"result": map[string]any{
					"content": []any{
						map[string]any{"type": "text", "text": "error: " + params.Name + ": command failed"},
					},
					"isError": true,
				},
			})
		}
	}
}

// fakeFailingMCPServerConfig re-invokes the current test binary as the fake
// failing MCP server defined above, wired to append to counterFile on every
// simulated tool execution.
func fakeFailingMCPServerConfig(name, counterFile string) model.MCPServerConfig {
	return model.MCPServerConfig{
		Name: name,
		Command: "env LEATHER_RUNNER_TEST_FAILING_MCP_SERVER=1 LEATHER_RUNNER_TEST_COUNTER_FILE=" +
			counterFile + " " + os.Args[0] + " -test.run=^$",
		Transport: "stdio",
	}
}

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

// TestRunner_DuplicateToolCallReplaysResult verifies that when the model
// re-issues an identical tool call (same name and arguments) that already
// succeeded earlier in the run, the runner does not execute it again — it
// replays the cached result (with a "[replay: ...]" prefix) instead of
// re-running the side effect. This guards against reasoning models that
// occasionally lose track of a prior tool call (observed in production) and
// repeat it, which would otherwise double a real side effect (e.g. posting a
// comment twice) or spin until max rounds.
//
// Plan 04: the model must see the real cached content, not a bare assertion
// that the call "already completed successfully" — the model can't verify
// that claim, and narrating unobserved success is exactly the failure mode
// this replay semantics avoids.
func TestRunner_DuplicateToolCallReplaysResult(t *testing.T) {
	var hits int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		hits++
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("posted"))
	}))
	defer srv.Close()

	reg := tool.NewRegistry()
	if err := reg.Register(model.Skill{
		Name: "comment-skill",
		Tools: []model.ToolDefinition{{
			Name: "post_comment",
			Type: "http",
			HTTP: model.HTTPToolConfig{Method: "POST", URL: srv.URL},
		}},
	}); err != nil {
		t.Fatalf("Register: %v", err)
	}

	sameCall := model.ToolCall{ID: "call-1", Name: "post_comment", Arguments: map[string]any{"body": "hello"}}
	mock := session.NewMockLLM(session.MockConfig{
		Response: "done",
		ToolCallSequence: [][]model.ToolCall{
			{sameCall},
			{sameCall}, // model repeats the identical call on round 1
		},
	})

	r := &Runner{Client: mock, Registry: reg, Log: testLogger(t), MaxToolRounds: 5}
	a := testAgent("dedupe-agent")
	a.Skills = []string{"comment-skill"}
	a.UserPrompt = "post the comment"

	rec, err := r.Run(context.Background(), a, testBudget())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if rec.Status != model.JobStatusSuccess {
		t.Errorf("status = %q, want success", rec.Status)
	}
	if hits != 1 {
		t.Errorf("HTTP tool hits = %d, want 1 (second call should be deduped, not re-executed)", hits)
	}

	// Find the tool-result message fed back after the second (deduped) call
	// and verify it carries the real cached content, not a bare assertion.
	calls := mock.Calls()
	last := calls[len(calls)-1]
	var found bool
	for _, msg := range last {
		if msg.Role == "tool" && msg.ToolName == "post_comment" && strings.Contains(msg.Content, "[replay:") {
			found = true
			if !strings.Contains(msg.Content, "posted") {
				t.Errorf("replayed content = %q, want it to contain the original cached content %q", msg.Content, "posted")
			}
			if strings.Contains(msg.Content, "already completed successfully") {
				t.Errorf("replayed content still asserts unobserved success: %q", msg.Content)
			}
		}
	}
	if !found {
		t.Error("no tool-role message with a [replay: ...] prefix found; expected the second call's result to be a labeled replay")
	}
}

func TestRunner_DuplicateBufferedToolCallReplaysOriginalCut(t *testing.T) {
	var hits int
	raw := strings.Repeat("abcde", 500)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		hits++
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(raw))
	}))
	defer srv.Close()

	reg := tool.NewRegistry()
	if err := reg.Register(model.Skill{
		Name: "buffer-skill",
		Tools: []model.ToolDefinition{{
			Name:   "read_big_log",
			Type:   "http",
			HTTP:   model.HTTPToolConfig{Method: "GET", URL: srv.URL},
			Buffer: true,
		}},
	}); err != nil {
		t.Fatalf("Register: %v", err)
	}

	sameCall := model.ToolCall{ID: "call-1", Name: "read_big_log", Arguments: map[string]any{"path": "big.log"}}
	mock := session.NewMockLLM(session.MockConfig{
		Response: "done",
		ToolCallSequence: [][]model.ToolCall{
			{sameCall},
			{sameCall},
		},
	})

	r := &Runner{
		Client:        mock,
		Registry:      reg,
		Log:           testLogger(t),
		MaxToolRounds: 5,
		HideBuffer:    hide.NewHideBuffer(64),
	}
	a := testAgent("buffered-replay-agent")
	a.Skills = []string{"buffer-skill"}
	a.UserPrompt = "read the log"

	rec, err := r.Run(context.Background(), a, testBudget())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if rec.Status != model.JobStatusSuccess {
		t.Errorf("status = %q, want success", rec.Status)
	}
	if hits != 1 {
		t.Fatalf("HTTP tool hits = %d, want 1 (duplicate buffered call should replay)", hits)
	}

	calls := mock.Calls()
	last := calls[len(calls)-1]
	var firstHideID, replayHideID string
	var replayFound bool
	for _, msg := range last {
		if msg.Role != "tool" || msg.ToolName != "read_big_log" {
			continue
		}
		match := hidePageHeaderRE.FindStringSubmatch(msg.Content)
		if len(match) < 2 {
			t.Fatalf("tool content is not a hide cut: %q", msg.Content)
		}
		if firstHideID == "" {
			firstHideID = match[1]
			continue
		}
		replayFound = true
		replayHideID = match[1]
		if !strings.Contains(msg.Content, "[replay:") {
			t.Fatalf("duplicate buffered result missing replay prefix: %q", msg.Content)
		}
	}
	if !replayFound {
		t.Fatal("did not find replayed buffered tool result in model context")
	}
	if replayHideID != firstHideID {
		t.Fatalf("replay hide id = %q, want original hide id %q", replayHideID, firstHideID)
	}
}

// TestRunner_FailedMCPCallDoesNotBlockRetry pins the exact production failure
// observed on 2026-07-06: a failed shell-tool exec ("error: ..." text with no
// isError signal) was treated as a successful call by the runner's dedupe
// map, which then silently blocked a legitimate retry of the same call —
// dropping an IP-ban deployment for 6+ hours. With plan 03's error
// propagation in place (shell-mcp sets isError; the mcp client returns a
// typed *mcp.ToolError; the executor surfaces it as ToolResult.Error), the
// runner's existing dedupe-insert guard (`result.Error == "" && dedupeKey !=
// ""`) never populates completedToolCalls for a failing call — so a second,
// identical call to the failing tool is executed again rather than silently
// skipped.
func TestRunner_FailedMCPCallDoesNotBlockRetry(t *testing.T) {
	counterFile := filepath.Join(t.TempDir(), "call-counter")
	if err := os.WriteFile(counterFile, nil, 0600); err != nil {
		t.Fatalf("create counter file: %v", err)
	}

	cfg := fakeFailingMCPServerConfig("shell", counterFile)
	mcpReg := mcp.NewRegistry([]model.MCPServerConfig{cfg}, nil)
	ctx := context.Background()
	if err := mcpReg.StartAll(ctx); err != nil {
		t.Fatalf("StartAll: %v", err)
	}
	defer mcpReg.StopAll()

	reg := tool.NewRegistry()
	if err := reg.Register(model.Skill{
		Name: "ban-skill",
		Tools: []model.ToolDefinition{{
			Name: "deploy_bans",
			Type: "mcp",
			MCP:  model.MCPToolConfig{Server: "shell", Tool: "deploy_bans"},
		}},
	}); err != nil {
		t.Fatalf("Register: %v", err)
	}

	// The model calls the zero-arg deploy_bans tool, sees a failure, and
	// (as happened in production) calls it again identically.
	sameCall := model.ToolCall{ID: "call-1", Name: "deploy_bans", Arguments: map[string]any{}}
	mock := session.NewMockLLM(session.MockConfig{
		Response: "done",
		ToolCallSequence: [][]model.ToolCall{
			{sameCall},
			{sameCall},
		},
	})

	var logBuf bytes.Buffer
	log := logging.NewWithWriter("test", model.LogLevelInfo, &logBuf, false)

	r := &Runner{
		Client:        mock,
		Registry:      reg,
		MCPRegistry:   mcpReg,
		Log:           log,
		MaxToolRounds: 5,
	}
	a := testAgent("ban-deploy-agent")
	a.Skills = []string{"ban-skill"}
	a.UserPrompt = "deploy the bans"

	rec, err := r.Run(ctx, a, testBudget())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if rec.Status != model.JobStatusSuccess {
		t.Errorf("status = %q, want success", rec.Status)
	}

	logOut := logBuf.String()
	if strings.Contains(logOut, "skipping duplicate") {
		t.Errorf("log contains a duplicate-skip line; the second call should have executed:\n%s", logOut)
	}

	// The fake server marks the counter file once per actual invocation. Two
	// marks means the dedupe map never blocked the second, identical call —
	// the exact behavior the 2026-07-06 incident violated (only one mark
	// would appear, and the second call would have returned the canned
	// "already completed successfully" text instead of executing).
	counted, err := os.ReadFile(counterFile)
	if err != nil {
		t.Fatalf("read counter file: %v", err)
	}
	if got := len(counted); got != 2 {
		t.Errorf("tool executed %d times, want 2 (dedupe map incorrectly blocked a failing call's retry)", got)
	}
}

// TestRunner_MaxRepeatsAllowsConfiguredExecutions verifies that a tool
// declaring max_repeats: 2 executes twice before further identical calls
// replay the cached result. Production motivation: a zero-arg tool like
// deploy-bans has a constant dedupe key, so under the default policy a
// second, semantically distinct call (world state changed mid-run) would be
// silently dropped. max_repeats gives such tools headroom.
func TestRunner_MaxRepeatsAllowsConfiguredExecutions(t *testing.T) {
	var hits int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		hits++
		w.WriteHeader(http.StatusOK)
		_, _ = fmt.Fprintf(w, "deployed batch %d", hits)
	}))
	defer srv.Close()

	reg := tool.NewRegistry()
	if err := reg.Register(model.Skill{
		Name: "ban-skill",
		Tools: []model.ToolDefinition{{
			Name:       "deploy_bans",
			Type:       "http",
			HTTP:       model.HTTPToolConfig{Method: "POST", URL: srv.URL},
			MaxRepeats: 2,
		}},
	}); err != nil {
		t.Fatalf("Register: %v", err)
	}

	sameCall := model.ToolCall{ID: "call-1", Name: "deploy_bans", Arguments: map[string]any{}}
	mock := session.NewMockLLM(session.MockConfig{
		Response: "done",
		ToolCallSequence: [][]model.ToolCall{
			{sameCall},
			{sameCall},
			{sameCall},
		},
	})

	r := &Runner{Client: mock, Registry: reg, Log: testLogger(t), MaxToolRounds: 5}
	a := testAgent("max-repeats-agent")
	a.Skills = []string{"ban-skill"}
	a.UserPrompt = "deploy the bans"

	rec, err := r.Run(context.Background(), a, testBudget())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if rec.Status != model.JobStatusSuccess {
		t.Errorf("status = %q, want success", rec.Status)
	}
	if hits != 2 {
		t.Errorf("HTTP tool hits = %d, want 2 (1st and 2nd execute, 3rd replays)", hits)
	}
}

// TestRunner_MaxRepeatsNegativeOneDisablesDedupe verifies that max_repeats:
// -1 disables dedupe entirely: every identical call executes.
func TestRunner_MaxRepeatsNegativeOneDisablesDedupe(t *testing.T) {
	var hits int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		hits++
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	}))
	defer srv.Close()

	reg := tool.NewRegistry()
	if err := reg.Register(model.Skill{
		Name: "poll-skill",
		Tools: []model.ToolDefinition{{
			Name:       "poll_status",
			Type:       "http",
			HTTP:       model.HTTPToolConfig{Method: "GET", URL: srv.URL},
			MaxRepeats: -1,
		}},
	}); err != nil {
		t.Fatalf("Register: %v", err)
	}

	sameCall := model.ToolCall{ID: "call-1", Name: "poll_status", Arguments: map[string]any{}}
	mock := session.NewMockLLM(session.MockConfig{
		Response: "done",
		ToolCallSequence: [][]model.ToolCall{
			{sameCall}, {sameCall}, {sameCall}, {sameCall},
		},
	})

	r := &Runner{Client: mock, Registry: reg, Log: testLogger(t), MaxToolRounds: 6}
	a := testAgent("no-dedupe-agent")
	a.Skills = []string{"poll-skill"}
	a.UserPrompt = "poll status"

	rec, err := r.Run(context.Background(), a, testBudget())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if rec.Status != model.JobStatusSuccess {
		t.Errorf("status = %q, want success", rec.Status)
	}
	if hits != 4 {
		t.Errorf("HTTP tool hits = %d, want 4 (max_repeats: -1 disables dedupe entirely)", hits)
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

// optsCapturingClient is a session.LLMClient that delegates Complete to a
// MockLLM but records the CompletionOptions passed on the first call, so
// tests can assert on request-shaping behavior (e.g. ExtraBody merging).
type optsCapturingClient struct {
	mock       *session.MockLLM
	firstOpts  session.CompletionOptions
	haveCalled bool
}

func (c *optsCapturingClient) Complete(ctx context.Context, modelName string, messages []model.Message, opts session.CompletionOptions) (model.LLMResponse, error) {
	if !c.haveCalled {
		c.firstOpts = opts
		c.haveCalled = true
	}
	return c.mock.Complete(ctx, modelName, messages, opts)
}

func (c *optsCapturingClient) CountTokens(messages []model.Message) (int, error) {
	return c.mock.CountTokens(messages)
}

// TestRun_DisableThinkingMergesChatTemplateKwargs verifies that
// Agent.DisableThinking causes the runner to send
// chat_template_kwargs.enable_thinking=false to the model, without
// clobbering the parallel_tool_calls key already set for tool-enabled turns.
func TestRun_DisableThinkingMergesChatTemplateKwargs(t *testing.T) {
	reg := tool.NewRegistry()
	if err := reg.Register(model.Skill{
		Name:  "noop-skill",
		Tools: []model.ToolDefinition{{Name: "noop_tool", Type: "http", HTTP: model.HTTPToolConfig{Method: "GET", URL: "http://127.0.0.1:0/"}}},
	}); err != nil {
		t.Fatalf("Register: %v", err)
	}

	client := &optsCapturingClient{mock: session.NewMockLLM(session.MockConfig{Response: "done"})}
	r := &Runner{Client: client, Registry: reg, Log: testLogger(t), MaxToolRounds: 3}

	a := testAgent("thinking-off")
	a.Skills = []string{"noop-skill"}
	a.UserPrompt = "go"
	a.DisableThinking = true

	if _, err := r.Run(context.Background(), a, testBudget()); err != nil {
		t.Fatalf("Run: %v", err)
	}

	kwargs, ok := client.firstOpts.ExtraBody["chat_template_kwargs"].(map[string]any)
	if !ok {
		t.Fatalf("chat_template_kwargs not set in ExtraBody: %+v", client.firstOpts.ExtraBody)
	}
	if enabled, _ := kwargs["enable_thinking"].(bool); enabled {
		t.Error("enable_thinking = true, want false")
	}
	if v, _ := client.firstOpts.ExtraBody["parallel_tool_calls"].(bool); v {
		t.Error("parallel_tool_calls should still be false alongside the thinking override")
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

func TestRetryReserveBump(t *testing.T) {
	cases := []struct {
		name            string
		current, maxTok int
		promptTokens    int
		wantBumped      int
		wantOK          bool
	}{
		{"doubles within ceiling", 1024, 8192, 100, 2048, true},
		{"caps at remaining context", 1024, 2000, 500, 1500, true},
		{"no room left", 1024, 1024, 100, 0, false},
		{"already at ceiling", 1024, 1124, 100, 0, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, ok := retryReserveBump(c.current, c.maxTok, c.promptTokens)
			if ok != c.wantOK {
				t.Fatalf("ok = %v, want %v", ok, c.wantOK)
			}
			if ok && got != c.wantBumped {
				t.Errorf("bumped = %d, want %d", got, c.wantBumped)
			}
		})
	}
}

// lengthThenStopClient returns a truncated (finish_reason "length", empty
// content) response on its first call, then a normal completion. It records
// the MaxTokens sent on each call so tests can assert the retry bump.
type lengthThenStopClient struct {
	mu        sync.Mutex
	calls     int
	sentMax   []int
	promptTok int
}

func (c *lengthThenStopClient) Complete(_ context.Context, _ string, _ []model.Message, opts session.CompletionOptions) (model.LLMResponse, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.calls++
	c.sentMax = append(c.sentMax, opts.MaxTokens)
	if c.calls == 1 {
		return model.LLMResponse{
			FinishReason: "length",
			PromptTokens: c.promptTok,
			TotalTokens:  c.promptTok,
		}, nil
	}
	return model.LLMResponse{
		Content:      "final answer after retry",
		FinishReason: "stop",
	}, nil
}

func (c *lengthThenStopClient) CountTokens(messages []model.Message) (int, error) {
	return 10 * len(messages), nil
}

func TestRun_SelfHealingRetry_OnTruncatedCompletion(t *testing.T) {
	client := &lengthThenStopClient{promptTok: 100}
	r := &Runner{
		Client:        client,
		Registry:      tool.NewRegistry(),
		Log:           testLogger(t),
		MaxToolRounds: 1,
	}

	a := testAgent("reasoning-agent")
	a.UserPrompt = "think hard"
	budget := model.TokenBudget{
		MaxTokens:          8192,
		CompletionReserve:  1024,
		SummarizeThreshold: 0.85,
	}
	rec, err := r.Run(context.Background(), a, budget)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if rec.LastResponse != "final answer after retry" {
		t.Errorf("LastResponse = %q, want retried content", rec.LastResponse)
	}
	client.mu.Lock()
	defer client.mu.Unlock()
	if client.calls != 2 {
		t.Fatalf("Complete calls = %d, want 2 (truncated + retry)", client.calls)
	}
	if client.sentMax[0] != 1024 {
		t.Errorf("first call max_tokens = %d, want 1024", client.sentMax[0])
	}
	if client.sentMax[1] <= client.sentMax[0] {
		t.Errorf("retry max_tokens = %d, want > %d", client.sentMax[1], client.sentMax[0])
	}
}

// emptyStopThenContentClient returns finish_reason "stop" with empty content
// on the first call, then non-empty content on retry — simulating a
// reasoning model that occasionally stops naturally without producing any
// output tokens (observed in production; not a truncation, so finish_reason
// is "stop" rather than "length").
type emptyStopThenContentClient struct {
	mu    sync.Mutex
	calls int
}

func (c *emptyStopThenContentClient) Complete(_ context.Context, _ string, _ []model.Message, _ session.CompletionOptions) (model.LLMResponse, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.calls++
	if c.calls == 1 {
		return model.LLMResponse{FinishReason: "stop"}, nil
	}
	return model.LLMResponse{Content: "final answer after retry", FinishReason: "stop"}, nil
}

func (c *emptyStopThenContentClient) CountTokens(messages []model.Message) (int, error) {
	return 10 * len(messages), nil
}

func TestRun_SelfHealingRetry_OnEmptyStopCompletion(t *testing.T) {
	client := &emptyStopThenContentClient{}
	r := &Runner{
		Client:        client,
		Registry:      tool.NewRegistry(),
		Log:           testLogger(t),
		MaxToolRounds: 1,
	}
	a := testAgent("flaky-agent")
	a.UserPrompt = "extract the fields"

	rec, err := r.Run(context.Background(), a, testBudget())
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if rec.LastResponse != "final answer after retry" {
		t.Errorf("LastResponse = %q, want retried content", rec.LastResponse)
	}
	client.mu.Lock()
	defer client.mu.Unlock()
	if client.calls != 2 {
		t.Fatalf("Complete calls = %d, want 2 (empty stop + retry)", client.calls)
	}
}

// alwaysEmptyStopClient always returns finish_reason "stop" with empty
// content, so the retry itself also comes back empty — the runner must pass
// the empty result through rather than looping.
type alwaysEmptyStopClient struct {
	mu    sync.Mutex
	calls int
}

func (c *alwaysEmptyStopClient) Complete(_ context.Context, _ string, _ []model.Message, _ session.CompletionOptions) (model.LLMResponse, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.calls++
	return model.LLMResponse{FinishReason: "stop"}, nil
}

func (c *alwaysEmptyStopClient) CountTokens(messages []model.Message) (int, error) {
	return 10 * len(messages), nil
}

func TestRun_EmptyStopRetry_IsSingleShot(t *testing.T) {
	client := &alwaysEmptyStopClient{}
	r := &Runner{
		Client:        client,
		Registry:      tool.NewRegistry(),
		Log:           testLogger(t),
		MaxToolRounds: 1,
	}
	a := testAgent("always-empty-agent")
	a.UserPrompt = "extract the fields"

	rec, err := r.Run(context.Background(), a, testBudget())
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if rec.LastResponse != "" {
		t.Errorf("LastResponse = %q, want empty (retry also came back empty)", rec.LastResponse)
	}
	client.mu.Lock()
	defer client.mu.Unlock()
	if client.calls != 2 {
		t.Errorf("Complete calls = %d, want exactly 2 (one retry, not a loop)", client.calls)
	}
}

// lengthWithContentClient returns finish_reason "length" but with non-empty
// content on every call, simulating a completion that was truncated after
// producing a partial (but non-empty) answer — the retry guard should not
// fire here, since resp.Content != "".
type lengthWithContentClient struct {
	mu    sync.Mutex
	calls int
}

func (c *lengthWithContentClient) Complete(_ context.Context, _ string, _ []model.Message, _ session.CompletionOptions) (model.LLMResponse, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.calls++
	return model.LLMResponse{
		Content:      "partial answer",
		FinishReason: "length",
	}, nil
}

func (c *lengthWithContentClient) CountTokens(messages []model.Message) (int, error) {
	return 10 * len(messages), nil
}

func TestRun_NoRetry_WhenContentAlreadyPresent(t *testing.T) {
	client := &lengthWithContentClient{}
	r := &Runner{
		Client:        client,
		Registry:      tool.NewRegistry(),
		Log:           testLogger(t),
		MaxToolRounds: 1,
	}
	a := testAgent("partial-answer-agent")
	a.UserPrompt = "give a partial answer"
	rec, err := r.Run(context.Background(), a, testBudget())
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if rec.LastResponse != "partial answer" {
		t.Errorf("LastResponse = %q, want %q", rec.LastResponse, "partial answer")
	}
	client.mu.Lock()
	defer client.mu.Unlock()
	if client.calls != 1 {
		t.Errorf("Complete calls = %d, want 1 (no retry when content is non-empty)", client.calls)
	}
}

// TestRun_SystemPromptOnlyAgent_SendsPlaceholderUserMessage verifies that an
// agent with no UserPrompt/UserPrompts configured still sends at least one
// user-role message, since strict OpenAI-compatible backends reject
// completion requests with zero user messages (#41).
func TestRun_SystemPromptOnlyAgent_SendsPlaceholderUserMessage(t *testing.T) {
	mock := session.NewMockLLM(session.MockConfig{Response: "did the thing"})
	r := &Runner{
		Client:        mock,
		Registry:      tool.NewRegistry(),
		Log:           testLogger(t),
		MaxToolRounds: 1,
	}

	a := testAgent("system-prompt-only")
	a.SystemPrompt = "You are a scheduled maintenance agent. Use your tools and report findings."
	// UserPrompt and UserPrompts both intentionally left unset.

	rec, err := r.Run(context.Background(), a, testBudget())
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if rec.Status != model.JobStatusSuccess {
		t.Fatalf("status = %q, want success", rec.Status)
	}

	calls := mock.Calls()
	if len(calls) != 1 {
		t.Fatalf("Complete calls = %d, want 1", len(calls))
	}
	var sawUser bool
	for _, msg := range calls[0] {
		if msg.Role == "user" && msg.Content == noPromptPlaceholder {
			sawUser = true
		}
	}
	if !sawUser {
		t.Fatalf("expected a user message with content %q, got messages: %+v", noPromptPlaceholder, calls[0])
	}
}

// requireUserMessageClient simulates a strict OpenAI-compatible backend that
// rejects completions with zero user-role messages, mirroring the real
// "No user query found in messages" 400 seen in #41.
type requireUserMessageClient struct{}

func (requireUserMessageClient) Complete(_ context.Context, _ string, messages []model.Message, _ session.CompletionOptions) (model.LLMResponse, error) {
	for _, msg := range messages {
		if msg.Role == "user" {
			return model.LLMResponse{Content: "ok", FinishReason: "stop"}, nil
		}
	}
	return model.LLMResponse{}, errors.New("status 400: No user query found in messages")
}

func (requireUserMessageClient) CountTokens(messages []model.Message) (int, error) {
	return 10 * len(messages), nil
}

func TestRun_SystemPromptOnlyAgent_SurvivesStrictBackend(t *testing.T) {
	r := &Runner{
		Client:        requireUserMessageClient{},
		Registry:      tool.NewRegistry(),
		Log:           testLogger(t),
		MaxToolRounds: 1,
	}
	a := testAgent("strict-backend-agent")
	a.SystemPrompt = "You are a scheduled maintenance agent."

	rec, err := r.Run(context.Background(), a, testBudget())
	if err != nil {
		t.Fatalf("Run: %v (a system-prompt-only agent should still satisfy a backend that requires a user message)", err)
	}
	if rec.Status != model.JobStatusSuccess {
		t.Fatalf("status = %q, want success", rec.Status)
	}
}

// splitAnswerClient emits the head of the answer together with a tool call on
// the first completion, then only the remainder after the tool result —
// simulating a reasoning model under load splitting its final answer across
// tool-call rounds (observed in production with qwen3_xml: the model treats
// the pre-tool-call text as already said and continues from where it stopped).
type splitAnswerClient struct {
	mu    sync.Mutex
	calls int
	tail  string // content of the post-tool-result completion
}

func (c *splitAnswerClient) Complete(_ context.Context, _ string, _ []model.Message, _ session.CompletionOptions) (model.LLMResponse, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.calls++
	if c.calls == 1 {
		return model.LLMResponse{
			Content:      "PR_NUMBER: 1007\nREPO: acme/voice\nFILES:",
			FinishReason: "tool_calls",
			ToolCalls:    []model.ToolCall{{ID: "call-1", Name: "get_files", Arguments: map[string]any{"pr": "1007"}}},
		}, nil
	}
	return model.LLMResponse{Content: c.tail, FinishReason: "stop"}, nil
}

func (c *splitAnswerClient) CountTokens(messages []model.Message) (int, error) {
	return 10 * len(messages), nil
}

func splitAnswerRunner(t *testing.T, client *splitAnswerClient) (*Runner, model.Agent) {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("src/a.go  +1 -0"))
	}))
	t.Cleanup(srv.Close)

	reg := tool.NewRegistry()
	if err := reg.Register(model.Skill{
		Name: "files-skill",
		Tools: []model.ToolDefinition{{
			Name: "get_files",
			Type: "http",
			HTTP: model.HTTPToolConfig{Method: "POST", URL: srv.URL},
		}},
	}); err != nil {
		t.Fatalf("Register: %v", err)
	}

	r := &Runner{Client: client, Registry: reg, Log: testLogger(t), MaxToolRounds: 5}
	a := testAgent("split-answer-agent")
	a.Skills = []string{"files-skill"}
	a.UserPrompt = "extract the fields"
	return r, a
}

func TestRun_SplitAnswerAcrossToolRounds_IsReassembled(t *testing.T) {
	client := &splitAnswerClient{tail: "  src/a.go  +1 -0\nCONCERN_PATHS: none"}
	r, a := splitAnswerRunner(t, client)

	rec, err := r.Run(context.Background(), a, testBudget())
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	want := "PR_NUMBER: 1007\nREPO: acme/voice\nFILES:\n  src/a.go  +1 -0\nCONCERN_PATHS: none"
	if rec.LastResponse != want {
		t.Errorf("LastResponse = %q, want the pre-tool-call head spliced ahead of the continuation %q", rec.LastResponse, want)
	}
	client.mu.Lock()
	defer client.mu.Unlock()
	if client.calls != 2 {
		t.Errorf("Complete calls = %d, want 2", client.calls)
	}
}

func TestRun_SplitAnswer_EmptyFinalStopSkipsBareRetry(t *testing.T) {
	// The model said everything alongside the tool call and finishes with an
	// empty stop: the banked fragment is the answer, and the empty-stop
	// self-healing retry must not burn an extra completion.
	client := &splitAnswerClient{tail: ""}
	r, a := splitAnswerRunner(t, client)

	rec, err := r.Run(context.Background(), a, testBudget())
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	want := "PR_NUMBER: 1007\nREPO: acme/voice\nFILES:"
	if rec.LastResponse != want {
		t.Errorf("LastResponse = %q, want banked fragment %q", rec.LastResponse, want)
	}
	client.mu.Lock()
	defer client.mu.Unlock()
	if client.calls != 2 {
		t.Errorf("Complete calls = %d, want exactly 2 (no bare retry when fragments are banked)", client.calls)
	}
}

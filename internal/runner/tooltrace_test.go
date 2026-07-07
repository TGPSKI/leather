package runner

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/TGPSKI/leather/internal/model"
	"github.com/TGPSKI/leather/internal/session"
	"github.com/TGPSKI/leather/internal/tool"
)

// legacyTurn mirrors model.Turn's shape *before* ToolTrace/ToolCalls existed.
// Used to prove that persist_runs_detail: none produces byte-identical JSON
// to what the pre-change struct would have written.
type legacyTurn struct {
	Prompt           string `json:"prompt"`
	Response         string `json:"response"`
	PromptTokens     int    `json:"prompt_tokens,omitempty"`
	CompletionTokens int    `json:"completion_tokens,omitempty"`
	TotalTokens      int    `json:"total_tokens,omitempty"`
}

func toolTraceRegistry(t *testing.T, toolName, url string) *tool.Registry {
	t.Helper()
	reg := tool.NewRegistry()
	if err := reg.Register(model.Skill{
		Name: "trace-skill",
		Tools: []model.ToolDefinition{{
			Name: toolName,
			Type: "http",
			HTTP: model.HTTPToolConfig{Method: "GET", URL: url},
		}},
	}); err != nil {
		t.Fatalf("Register: %v", err)
	}
	return reg
}

// TestRunner_ToolTrace_NoneDetailByteIdentical verifies that
// PersistRunsDetail == "none" (the zero value / default) leaves
// Turn.ToolCalls nil, so the JSON encoding of the turn is byte-identical to
// the pre-ToolTrace Turn shape (legacyTurn) for the same field values.
func TestRunner_ToolTrace_NoneDetailByteIdentical(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	}))
	defer srv.Close()

	reg := toolTraceRegistry(t, "echo_tool", srv.URL)
	mock := session.NewMockLLM(session.MockConfig{
		Response: "final answer",
		ToolCallSequence: [][]model.ToolCall{
			{{ID: "call-1", Name: "echo_tool", Arguments: map[string]any{"input": "test"}}},
		},
	})

	r := &Runner{
		Client:        mock,
		Registry:      reg,
		Log:           testLogger(t),
		MaxToolRounds: 5,
		// PersistRunsDetail intentionally left as the zero value ("") to
		// exercise the default/legacy path.
	}
	a := testAgent("none-detail-agent")
	a.Skills = []string{"trace-skill"}
	a.UserPrompt = "use the echo tool"

	rec, err := r.Run(context.Background(), a, testBudget())
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(rec.Turns) != 1 {
		t.Fatalf("turns = %d, want 1", len(rec.Turns))
	}
	turn := rec.Turns[0]
	if turn.ToolCalls != nil {
		t.Fatalf("ToolCalls = %#v, want nil when PersistRunsDetail is not \"tools\"", turn.ToolCalls)
	}

	gotJSON, err := json.Marshal(turn)
	if err != nil {
		t.Fatalf("marshal turn: %v", err)
	}
	if strings.Contains(string(gotJSON), "tool_calls") {
		t.Fatalf("none-detail turn JSON unexpectedly contains tool_calls: %s", gotJSON)
	}

	legacy := legacyTurn{
		Prompt:           turn.Prompt,
		Response:         turn.Response,
		PromptTokens:     turn.PromptTokens,
		CompletionTokens: turn.CompletionTokens,
		TotalTokens:      turn.TotalTokens,
	}
	wantJSON, err := json.Marshal(legacy)
	if err != nil {
		t.Fatalf("marshal legacy turn: %v", err)
	}
	if string(gotJSON) != string(wantJSON) {
		t.Fatalf("turn JSON not byte-identical to legacy shape:\n got:  %s\n want: %s", gotJSON, wantJSON)
	}

	// Also exercise the explicit "none" value, not just the zero value.
	r.PersistRunsDetail = "none"
	mock2 := session.NewMockLLM(session.MockConfig{
		Response: "final answer",
		ToolCallSequence: [][]model.ToolCall{
			{{ID: "call-1", Name: "echo_tool", Arguments: map[string]any{"input": "test"}}},
		},
	})
	r.Client = mock2
	rec2, err := r.Run(context.Background(), a, testBudget())
	if err != nil {
		t.Fatalf("Run (explicit none): %v", err)
	}
	if rec2.Turns[0].ToolCalls != nil {
		t.Fatalf("explicit none: ToolCalls = %#v, want nil", rec2.Turns[0].ToolCalls)
	}
}

// TestRunner_ToolTrace_ToolsDetailPopulatesTraces verifies that
// PersistRunsDetail == "tools" populates Turn.ToolCalls with one ToolTrace
// per tool invocation, in call order, with a positive duration.
func TestRunner_ToolTrace_ToolsDetailPopulatesTraces(t *testing.T) {
	srvA := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		time.Sleep(2 * time.Millisecond)
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("result-A"))
	}))
	defer srvA.Close()
	srvB := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		time.Sleep(2 * time.Millisecond)
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("result-B"))
	}))
	defer srvB.Close()

	reg := tool.NewRegistry()
	if err := reg.Register(model.Skill{
		Name: "trace-skill",
		Tools: []model.ToolDefinition{
			{Name: "tool_a", Type: "http", HTTP: model.HTTPToolConfig{Method: "GET", URL: srvA.URL}},
			{Name: "tool_b", Type: "http", HTTP: model.HTTPToolConfig{Method: "GET", URL: srvB.URL}},
		},
	}); err != nil {
		t.Fatalf("Register: %v", err)
	}

	mock := session.NewMockLLM(session.MockConfig{
		Response: "final answer",
		ToolCallSequence: [][]model.ToolCall{
			{{ID: "call-1", Name: "tool_a", Arguments: map[string]any{"x": 1}}},
			{{ID: "call-2", Name: "tool_b", Arguments: map[string]any{"y": 2}}},
		},
	})

	r := &Runner{
		Client:            mock,
		Registry:          reg,
		Log:               testLogger(t),
		MaxToolRounds:     5,
		PersistRunsDetail: "tools",
	}
	a := testAgent("tools-detail-agent")
	a.Skills = []string{"trace-skill"}
	a.UserPrompt = "use both tools"

	rec, err := r.Run(context.Background(), a, testBudget())
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(rec.Turns) != 1 {
		t.Fatalf("turns = %d, want 1", len(rec.Turns))
	}
	traces := rec.Turns[0].ToolCalls
	if len(traces) != 2 {
		t.Fatalf("traces = %d, want 2: %#v", len(traces), traces)
	}
	if traces[0].Name != "tool_a" || traces[1].Name != "tool_b" {
		t.Fatalf("trace order = [%s, %s], want [tool_a, tool_b]", traces[0].Name, traces[1].Name)
	}
	if traces[0].Content != "result-A" || traces[1].Content != "result-B" {
		t.Fatalf("trace content = [%q, %q], want [result-A, result-B]", traces[0].Content, traces[1].Content)
	}
	if traces[0].DurationMs <= 0 || traces[1].DurationMs <= 0 {
		t.Fatalf("trace durations = [%d, %d], want > 0", traces[0].DurationMs, traces[1].DurationMs)
	}
	if !strings.Contains(traces[0].Args, `"x":1`) {
		t.Fatalf("trace[0].Args = %q, want to contain the call arguments", traces[0].Args)
	}
	if traces[0].Error != "" || traces[1].Error != "" {
		t.Fatalf("unexpected trace errors: %q, %q", traces[0].Error, traces[1].Error)
	}
}

// TestRunner_ToolTrace_CapsEnforced verifies the per-field byte cap is
// enforced exactly at the boundary: content of exactly cap bytes passes
// through unmodified, content of cap+1 bytes is truncated with the
// "…[capped]" marker appended.
func TestRunner_ToolTrace_CapsEnforced(t *testing.T) {
	const cap0 = 10
	exact := strings.Repeat("a", cap0)
	over := strings.Repeat("b", cap0+1)

	newSrv := func(body string) *httptest.Server {
		return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(body))
		}))
	}
	srvExact := newSrv(exact)
	defer srvExact.Close()
	srvOver := newSrv(over)
	defer srvOver.Close()

	reg := tool.NewRegistry()
	if err := reg.Register(model.Skill{
		Name: "cap-skill",
		Tools: []model.ToolDefinition{
			{Name: "tool_exact", Type: "http", HTTP: model.HTTPToolConfig{Method: "GET", URL: srvExact.URL}},
			{Name: "tool_over", Type: "http", HTTP: model.HTTPToolConfig{Method: "GET", URL: srvOver.URL}},
		},
	}); err != nil {
		t.Fatalf("Register: %v", err)
	}

	mock := session.NewMockLLM(session.MockConfig{
		Response: "done",
		ToolCallSequence: [][]model.ToolCall{
			{{ID: "call-1", Name: "tool_exact", Arguments: map[string]any{}}},
			{{ID: "call-2", Name: "tool_over", Arguments: map[string]any{}}},
		},
	})

	r := &Runner{
		Client:             mock,
		Registry:           reg,
		Log:                testLogger(t),
		MaxToolRounds:      5,
		PersistRunsDetail:  "tools",
		PersistRunsToolCap: cap0,
	}
	a := testAgent("cap-agent")
	a.Skills = []string{"cap-skill"}
	a.UserPrompt = "call both"

	rec, err := r.Run(context.Background(), a, testBudget())
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	traces := rec.Turns[0].ToolCalls
	if len(traces) != 2 {
		t.Fatalf("traces = %d, want 2", len(traces))
	}
	if traces[0].Content != exact {
		t.Fatalf("exact-cap content = %q (%d bytes), want unmodified %q", traces[0].Content, len(traces[0].Content), exact)
	}
	wantOver := over[:cap0] + "…[capped]"
	if traces[1].Content != wantOver {
		t.Fatalf("over-cap content = %q, want %q", traces[1].Content, wantOver)
	}
}

// TestRunner_ToolTrace_SecretRedaction verifies that a secret-bearing
// argument value (an Authorization header) is redacted before being
// persisted in the trace's Args field.
func TestRunner_ToolTrace_SecretRedaction(t *testing.T) {
	// Unreachable port: the call fails fast (connection refused), but Args
	// are captured before execution regardless of the outcome.
	reg := toolTraceRegistry(t, "auth_tool", "http://127.0.0.1:0/unreachable")

	mock := session.NewMockLLM(session.MockConfig{
		Response: "done",
		ToolCallSequence: [][]model.ToolCall{
			{{ID: "call-1", Name: "auth_tool", Arguments: map[string]any{
				"Authorization": "Bearer sk-super-secret-token",
			}}},
		},
	})

	r := &Runner{
		Client:            mock,
		Registry:          reg,
		Log:               testLogger(t),
		MaxToolRounds:     5,
		PersistRunsDetail: "tools",
	}
	a := testAgent("secret-agent")
	a.Skills = []string{"trace-skill"}
	a.UserPrompt = "call the auth tool"

	rec, err := r.Run(context.Background(), a, testBudget())
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	traces := rec.Turns[0].ToolCalls
	if len(traces) != 1 {
		t.Fatalf("traces = %d, want 1", len(traces))
	}
	if strings.Contains(traces[0].Args, "sk-super-secret-token") {
		t.Fatalf("trace args leaked the secret: %q", traces[0].Args)
	}
	if !strings.Contains(traces[0].Args, "[REDACTED]") {
		t.Fatalf("trace args not redacted: %q", traces[0].Args)
	}
}

// TestRunner_ToolTrace_FailedToolRecordsError verifies that a failed tool
// call records Error (non-empty) and leaves Content empty in the trace.
//
// This exercises the existing generic tool.Executor error path (HTTP dial
// failure -> result.Error set, runner continues without aborting the run),
// which predates plan 03 (MCP-specific isError propagation). Plan 03 adds a
// *typed* error for MCP tool-reported failures specifically; it does not
// change the fact that result.Error already reaches this trace capture
// point today, so this test passes standalone. If plan 03/04 land and
// change this code path in a way that breaks this assertion, that's a
// signal to revisit — not expected per the plan's own "independent" framing.
func TestRunner_ToolTrace_FailedToolRecordsError(t *testing.T) {
	reg := toolTraceRegistry(t, "failing_tool", "http://127.0.0.1:0/unreachable")

	mock := session.NewMockLLM(session.MockConfig{
		Response: "done",
		ToolCallSequence: [][]model.ToolCall{
			{{ID: "call-1", Name: "failing_tool", Arguments: map[string]any{}}},
		},
	})

	r := &Runner{
		Client:            mock,
		Registry:          reg,
		Log:               testLogger(t),
		MaxToolRounds:     5,
		PersistRunsDetail: "tools",
	}
	a := testAgent("failed-tool-agent")
	a.Skills = []string{"trace-skill"}
	a.UserPrompt = "call the failing tool"

	rec, err := r.Run(context.Background(), a, testBudget())
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	traces := rec.Turns[0].ToolCalls
	if len(traces) != 1 {
		t.Fatalf("traces = %d, want 1", len(traces))
	}
	if traces[0].Error == "" {
		t.Fatal("expected trace.Error to be populated for a failed tool call")
	}
	if traces[0].Content != "" {
		t.Fatalf("expected trace.Content to be empty on failure, got %q", traces[0].Content)
	}
}

// TestRunner_ToolTrace_ReplayedCallMarksReplayed verifies that a tool call
// skipped by the dedupe guard (identical name+args already completed
// successfully earlier in the run) is recorded with Replayed: true.
//
// This exercises the dedupe-hit branch in runner.go that predates plan 04
// (dedupe policy replay semantics) — the branch that produces the synthetic
// "already completed" result already exists today, so this test passes
// standalone. Plan 04 is expected to refine *when* replay happens, not
// remove the branch this test observes.
func TestRunner_ToolTrace_ReplayedCallMarksReplayed(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	}))
	defer srv.Close()

	reg := toolTraceRegistry(t, "repeat_tool", srv.URL)

	mock := session.NewMockLLM(session.MockConfig{
		Response: "done",
		ToolCallSequence: [][]model.ToolCall{
			{{ID: "call-1", Name: "repeat_tool", Arguments: map[string]any{"same": "args"}}},
			{{ID: "call-2", Name: "repeat_tool", Arguments: map[string]any{"same": "args"}}},
		},
	})

	r := &Runner{
		Client:            mock,
		Registry:          reg,
		Log:               testLogger(t),
		MaxToolRounds:     5,
		PersistRunsDetail: "tools",
	}
	a := testAgent("replay-agent")
	a.Skills = []string{"trace-skill"}
	a.UserPrompt = "call it twice"

	rec, err := r.Run(context.Background(), a, testBudget())
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	traces := rec.Turns[0].ToolCalls
	if len(traces) != 2 {
		t.Fatalf("traces = %d, want 2", len(traces))
	}
	if traces[0].Replayed {
		t.Fatal("first call should not be marked Replayed")
	}
	if !traces[1].Replayed {
		t.Fatal("second (duplicate) call should be marked Replayed: true")
	}
}

package cli

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/tgpski/leather/internal/logging"
	"github.com/tgpski/leather/internal/model"
	"github.com/tgpski/leather/internal/runner"
	"github.com/tgpski/leather/internal/session"
)

// writeFile is a test helper that creates a file with the given content.
func writeFile(path, content string) error {
	return os.WriteFile(path, []byte(content), 0600)
}

// --- RunVersion ---

func TestRunVersion_OutputContainsVersionAndCommit(t *testing.T) {
	var out bytes.Buffer
	code := RunVersion(nil, &out, io.Discard, "v1.2.3", "abc1234")
	if code != 0 {
		t.Errorf("exit code = %d, want 0", code)
	}
	got := out.String()
	if !strings.Contains(got, "v1.2.3") {
		t.Errorf("output %q missing version", got)
	}
	if !strings.Contains(got, "abc1234") {
		t.Errorf("output %q missing commit", got)
	}
}

// --- Run dispatch ---

func TestRun_NoArgs_PrintsUsage(t *testing.T) {
	var out bytes.Buffer
	code := Run(nil, &out, io.Discard, "dev", "none")
	if code != 0 {
		t.Errorf("exit code = %d, want 0", code)
	}
	if len(out.String()) == 0 {
		t.Error("expected usage text, got empty output")
	}
}

func TestRun_HelpVariants(t *testing.T) {
	for _, arg := range []string{"help", "--help", "-h"} {
		t.Run(arg, func(t *testing.T) {
			var out bytes.Buffer
			code := Run([]string{arg}, &out, io.Discard, "dev", "none")
			if code != 0 {
				t.Errorf("Run(%q) exit code = %d, want 0", arg, code)
			}
			if len(out.String()) == 0 {
				t.Errorf("Run(%q): expected usage text, got empty output", arg)
			}
		})
	}
}

func TestRun_UnknownCommand_ReturnsExitCode2(t *testing.T) {
	var errOut bytes.Buffer
	code := Run([]string{"bogus-command"}, io.Discard, &errOut, "dev", "none")
	if code != 2 {
		t.Errorf("exit code = %d, want 2", code)
	}
	if !strings.Contains(errOut.String(), "bogus-command") {
		t.Errorf("stderr %q missing unknown command name", errOut.String())
	}
}

func TestRun_Version_Dispatches(t *testing.T) {
	var out bytes.Buffer
	code := Run([]string{"version"}, &out, io.Discard, "v0.0.1", "deadbeef")
	if code != 0 {
		t.Errorf("exit code = %d, want 0", code)
	}
	if !strings.Contains(out.String(), "v0.0.1") {
		t.Errorf("output %q missing version", out.String())
	}
}

// --- formatJob ---

func TestFormatJob_NeverRun(t *testing.T) {
	j := model.Job{AgentName: "my-agent", Status: model.JobStatusPending}
	got := formatJob(j)
	if !strings.Contains(got, "my-agent") {
		t.Errorf("formatJob %q missing agent name", got)
	}
	if !strings.Contains(got, "never") {
		t.Errorf("formatJob %q missing 'never' for zero LastRun", got)
	}
	if !strings.Contains(got, "n/a") {
		t.Errorf("formatJob %q missing 'n/a' for zero NextRun", got)
	}
}

func TestFormatJob_WithTimes(t *testing.T) {
	j := model.Job{
		AgentName: "sched-agent",
		Status:    model.JobStatusSuccess,
		LastRun:   1000000,
		NextRun:   2000000,
		RunCount:  7,
	}
	got := formatJob(j)
	if !strings.Contains(got, "sched-agent") {
		t.Errorf("formatJob %q missing agent name", got)
	}
	if !strings.Contains(got, "7") {
		t.Errorf("formatJob %q missing run count", got)
	}
	if !strings.Contains(got, "success") {
		t.Errorf("formatJob %q missing status", got)
	}
}

// --- resolveAgent ---

func TestResolveAgent_FillsFromConfig(t *testing.T) {
	cfg := model.Config{
		Model:       "llama3",
		Temperature: 0.7,
		LLMTimeout:  60 * time.Second,
	}
	a := model.Agent{Name: "test-agent"} // all zero fields
	got := resolveAgent(cfg, a)
	if got.Model != "llama3" {
		t.Errorf("Model = %q, want llama3", got.Model)
	}
	if got.Temperature != 0.7 {
		t.Errorf("Temperature = %v, want 0.7", got.Temperature)
	}
	if got.Timeout != 60*time.Second {
		t.Errorf("Timeout = %v, want 60s", got.Timeout)
	}
}

func TestResolveAgent_AgentFieldsNotOverwritten(t *testing.T) {
	cfg := model.Config{
		Model:       "llama3",
		Temperature: 0.7,
		LLMTimeout:  60 * time.Second,
	}
	a := model.Agent{
		Name:        "test-agent",
		Model:       "mistral",
		Temperature: 0.1,
		Timeout:     10 * time.Second,
	}
	got := resolveAgent(cfg, a)
	if got.Model != "mistral" {
		t.Errorf("Model = %q, want mistral (agent value should not be overwritten)", got.Model)
	}
	if got.Temperature != 0.1 {
		t.Errorf("Temperature = %v, want 0.1", got.Temperature)
	}
	if got.Timeout != 10*time.Second {
		t.Errorf("Timeout = %v, want 10s", got.Timeout)
	}
}

func TestResolveAgent_MergesDefaultToolsets(t *testing.T) {
	cfg := model.Config{DefaultToolsets: []string{"global-read", "global-write"}}
	a := model.Agent{Name: "test-agent", Toolsets: []string{"global-read", "agent-write"}}
	got := resolveAgent(cfg, a)
	want := []string{"global-read", "global-write", "agent-write"}
	if len(got.Toolsets) != len(want) {
		t.Fatalf("Toolsets len = %d, want %d (%v)", len(got.Toolsets), len(want), got.Toolsets)
	}
	for i := range want {
		if got.Toolsets[i] != want[i] {
			t.Fatalf("Toolsets[%d] = %q, want %q", i, got.Toolsets[i], want[i])
		}
	}
}

// --- resolveTokenBudget ---

func TestResolveTokenBudget_DefaultsFromConfig(t *testing.T) {
	cfg := model.Config{
		MaxTokens:          8192,
		CompletionReserve:  1024,
		SummarizeThreshold: 0.85,
	}
	a := model.Agent{Name: "test"}
	got := resolveTokenBudget(cfg, a)
	if got.MaxTokens != 8192 {
		t.Errorf("MaxTokens = %d, want 8192", got.MaxTokens)
	}
	if got.CompletionReserve != 1024 {
		t.Errorf("CompletionReserve = %d, want 1024", got.CompletionReserve)
	}
}

func TestResolveTokenBudget_AgentOverridesMaxTokens(t *testing.T) {
	cfg := model.Config{
		MaxTokens:          8192,
		CompletionReserve:  1024,
		SummarizeThreshold: 0.85,
	}
	a := model.Agent{Name: "test", MaxTokens: 4096}
	got := resolveTokenBudget(cfg, a)
	if got.MaxTokens != 4096 {
		t.Errorf("MaxTokens = %d, want 4096 (agent override)", got.MaxTokens)
	}
	if got.CompletionReserve != 1024 {
		t.Errorf("CompletionReserve = %d, want 1024 (unchanged)", got.CompletionReserve)
	}
}

// --- buildLogger ---

func TestBuildLogger_TextFormat(t *testing.T) {
	var buf bytes.Buffer
	cfg := model.Config{LogFormat: "text", LogLevel: model.LogLevelInfo}
	log := buildLogger(cfg, &buf)
	if log == nil {
		t.Fatal("buildLogger returned nil")
	}
	log.Info("ping")
	if !strings.Contains(buf.String(), "ping") {
		t.Errorf("expected 'ping' in text output, got %q", buf.String())
	}
}

func TestBuildLogger_JSONFormat(t *testing.T) {
	var buf bytes.Buffer
	cfg := model.Config{LogFormat: "json", LogLevel: model.LogLevelInfo}
	log := buildLogger(cfg, &buf)
	if log == nil {
		t.Fatal("buildLogger returned nil")
	}
	log.Info("ping")
	got := buf.String()
	if !strings.Contains(got, `"msg"`) {
		t.Errorf("expected JSON msg field in output, got %q", got)
	}
	if !strings.Contains(got, "ping") {
		t.Errorf("expected 'ping' in JSON output, got %q", got)
	}
}

// --- executeAgent ---

func TestExecuteAgent_Success(t *testing.T) {
	ctx := context.Background()
	client := session.NewMockLLM(session.MockConfig{
		Response:         "agent reply",
		TokensPerMessage: 5,
	})
	log := logging.NewWithWriter("test", model.LogLevelError, io.Discard, false)
	budget := model.TokenBudget{
		MaxTokens:          8192,
		CompletionReserve:  1024,
		SummarizeThreshold: 0.85,
	}
	a := model.Agent{
		Name:         "test-agent",
		Model:        "test-model",
		SystemPrompt: "You are a test agent.",
		UserPrompt:   "Say hello.",
		Timeout:      5 * time.Second,
	}
	resp, err := executeAgent(ctx, a, budget, client, log)
	if err != nil {
		t.Fatalf("executeAgent: %v", err)
	}
	if resp.Content != "agent reply" {
		t.Errorf("Content = %q, want %q", resp.Content, "agent reply")
	}
}

func TestExecuteAgent_LLMError(t *testing.T) {
	ctx := context.Background()
	client := session.NewMockLLM(session.MockConfig{
		Err: errors.New("mock LLM failure"),
	})
	log := logging.NewWithWriter("test", model.LogLevelError, io.Discard, false)
	budget := model.TokenBudget{
		MaxTokens:          8192,
		CompletionReserve:  1024,
		SummarizeThreshold: 0.85,
	}
	a := model.Agent{
		Name:    "fail-agent",
		Model:   "test-model",
		Timeout: 5 * time.Second,
	}
	_, err := executeAgent(ctx, a, budget, client, log)
	if err == nil {
		t.Fatal("expected error from LLM failure, got nil")
	}
	if !strings.Contains(err.Error(), "fail-agent") {
		t.Errorf("error %q missing agent name", err.Error())
	}
}

func TestExecuteAgent_NoPrompts(t *testing.T) {
	// An agent with no system or user prompt should still complete successfully.
	ctx := context.Background()
	client := session.NewMockLLM(session.MockConfig{Response: "empty prompt ok"})
	log := logging.NewWithWriter("test", model.LogLevelError, io.Discard, false)
	budget := model.TokenBudget{
		MaxTokens:          8192,
		CompletionReserve:  1024,
		SummarizeThreshold: 0.85,
	}
	a := model.Agent{
		Name:    "empty-agent",
		Model:   "test-model",
		Timeout: 5 * time.Second,
	}
	resp, err := executeAgent(ctx, a, budget, client, log)
	if err != nil {
		t.Fatalf("executeAgent with no prompts: %v", err)
	}
	if resp.Content != "empty prompt ok" {
		t.Errorf("Content = %q, want %q", resp.Content, "empty prompt ok")
	}
}

// --- RunValidate ---

func TestRunValidate_EmptyDir_ExitsZero(t *testing.T) {
	dir := t.TempDir()
	var out, errOut bytes.Buffer
	code := RunValidate([]string{"--agent-dir", dir}, &out, &errOut)
	if code != 0 {
		t.Errorf("exit code = %d, want 0 for empty agent dir; stderr: %s", code, errOut.String())
	}
}

// --- RunStatus ---

func TestRunStatus_EmptyStateDir_ExitsZero(t *testing.T) {
	dir := t.TempDir()
	var out, errOut bytes.Buffer
	code := RunStatus([]string{"--agent-dir", dir, "--state-dir", dir}, &out, &errOut)
	if code != 0 {
		t.Errorf("exit code = %d, want 0 for empty state dir; stderr: %s", code, errOut.String())
	}
	if !strings.Contains(out.String(), "no persisted job records") {
		t.Errorf("output %q missing expected empty-state message", out.String())
	}
}

// --- printTurn ---

func TestPrintTurn_BasicOutput(t *testing.T) {
	var buf bytes.Buffer
	a := model.Agent{Name: "my-agent", UserPrompt: "Hello?"}
	resp := model.LLMResponse{Content: "World!", PromptTokens: 5, CompletionTokens: 3, TotalTokens: 8}
	printTurn(&buf, a, resp, "messages", false, false)
	got := buf.String()
	if !strings.Contains(got, "agent") {
		t.Errorf("printTurn output %q missing agent label", got)
	}
	if !strings.Contains(got, "user") || !strings.Contains(got, "Hello?") {
		t.Errorf("printTurn output %q missing user prompt", got)
	}
	if !strings.Contains(got, "my-agent") {
		t.Errorf("printTurn output %q missing agent name", got)
	}
	if !strings.Contains(got, "World!") {
		t.Errorf("printTurn output %q missing response content", got)
	}
}

func TestPrintTurn_WithStats(t *testing.T) {
	var buf bytes.Buffer
	a := model.Agent{Name: "stats-agent"}
	resp := model.LLMResponse{Content: "ok", PromptTokens: 10, CompletionTokens: 5, TotalTokens: 15}
	printTurn(&buf, a, resp, "messages", true, false)
	got := buf.String()
	if !strings.Contains(got, "15") {
		t.Errorf("printTurn with stats %q missing total token count", got)
	}
	if !strings.Contains(got, "tokens") {
		t.Errorf("printTurn with stats %q missing 'tokens' label", got)
	}
	if !strings.Contains(got, "┆") {
		t.Errorf("printTurn with stats %q missing continuation rail for tokens", got)
	}
}

func TestPrintTurn_NoUserPrompt(t *testing.T) {
	var buf bytes.Buffer
	a := model.Agent{Name: "quiet-agent"} // no UserPrompt
	resp := model.LLMResponse{Content: "silent reply"}
	printTurn(&buf, a, resp, "messages", false, false)
	got := buf.String()
	if !strings.Contains(got, "silent reply") {
		t.Errorf("printTurn output %q missing response content", got)
	}
}

func TestPrintTurn_AllModeSuppressesUserPrompt(t *testing.T) {
	var buf bytes.Buffer
	a := model.Agent{Name: "my-agent", UserPrompt: "Hello?"}
	resp := model.LLMResponse{Content: "World!"}
	printTurn(&buf, a, resp, "all", false, false)
	got := buf.String()
	if strings.Contains(got, "Hello?") {
		t.Errorf("printTurn all-mode output %q unexpectedly included user prompt", got)
	}
	if !strings.Contains(got, "World!") {
		t.Errorf("printTurn all-mode output %q missing response content", got)
	}
}

func TestPrintTurn_MultilineUsesContinuationRail(t *testing.T) {
	var buf bytes.Buffer
	a := model.Agent{Name: "go-release-prep", UserPrompt: "Call git_log_since\nThen categorize commits."}
	resp := model.LLMResponse{Content: "Categories:\n- Documentation\n- CI/CD"}
	printTurn(&buf, a, resp, "messages", false, false)
	got := buf.String()
	if !strings.Contains(got, "user") {
		t.Errorf("printTurn multiline output %q missing user label", got)
	}
	if !strings.Contains(got, "┆") {
		t.Errorf("printTurn multiline output %q missing continuation rail", got)
	}
	if !strings.Contains(got, "Then categorize commits.") {
		t.Errorf("printTurn multiline output %q missing continued user line", got)
	}
	if !strings.Contains(got, "Categories:") || !strings.Contains(got, "- CI/CD") {
		t.Errorf("printTurn multiline output %q missing response lines", got)
	}
}

func TestPrintProgress_UserMultilineUsesContinuationRail(t *testing.T) {
	var buf bytes.Buffer
	printProgress(&buf, runner.ProgressEvent{Kind: "user", Prompt: "Call git_log_since\nThen categorize commits."})
	got := buf.String()
	if !strings.Contains(got, "user") {
		t.Errorf("printProgress output %q missing user label", got)
	}
	if !strings.Contains(got, "Then categorize commits.") {
		t.Errorf("printProgress output %q missing continued line", got)
	}
	if !strings.Contains(got, "┆") {
		t.Errorf("printProgress output %q missing continuation rail", got)
	}
}

func TestPrintProgress_RightAlignsLabelAndRail(t *testing.T) {
	t.Setenv("NO_COLOR", "1")
	t.Setenv("TERM", "xterm")
	var buf bytes.Buffer
	printProgress(&buf, runner.ProgressEvent{Kind: "user", Prompt: "line one\nline two"})
	got := buf.String()
	if !strings.Contains(got, "]      user  line one") {
		t.Errorf("printProgress output %q missing right-aligned user label", got)
	}
	if !strings.Contains(got, "      ┆  line two") {
		t.Errorf("printProgress output %q missing right-aligned continuation rail", got)
	}
}

func TestPrintProgress_CallFormatsArgsVertically(t *testing.T) {
	var buf bytes.Buffer
	printProgress(&buf, runner.ProgressEvent{
		Kind:     "call",
		ToolType: "mcp",
		Tool:     "git_checkout_b",
		Args:     `{"branch":"release/v0.2.1-prep","path":"/tmp/repo"}`,
		Round:    0,
	})
	got := buf.String()
	if !strings.Contains(got, "⚙ mcp") {
		t.Errorf("printProgress call output %q missing tool label", got)
	}
	if !strings.Contains(got, "git_checkout_b") {
		t.Errorf("printProgress call output %q missing tool name", got)
	}
	if !strings.Contains(got, "branch: release/v0.2.1-prep") {
		t.Errorf("printProgress call output %q missing branch arg", got)
	}
	if !strings.Contains(got, "path: /tmp/repo") {
		t.Errorf("printProgress call output %q missing path arg", got)
	}
	if !strings.Contains(got, "round: 1") {
		t.Errorf("printProgress call output %q missing round line", got)
	}
}

func TestPrintProgress_ResultUsesBodyStatus(t *testing.T) {
	var buf bytes.Buffer
	printProgress(&buf, runner.ProgressEvent{Kind: "result", Tool: "git_checkout_b", ResultBytes: 60})
	got := buf.String()
	if !strings.Contains(got, "result") {
		t.Errorf("printProgress result output %q missing result label", got)
	}
	if !strings.Contains(got, "✓ success") {
		t.Errorf("printProgress result output %q missing success state in body", got)
	}
	if strings.Contains(got, "✓ result") {
		t.Errorf("printProgress result output %q still has success state in label column", got)
	}
}

func TestPrettyWriteBlock_WrapsLongBodyUnderRail(t *testing.T) {
	t.Setenv("NO_COLOR", "1")
	t.Setenv("COLUMNS", "56")
	var buf bytes.Buffer
	prettyWriteEntry(&buf, "10:10:52", prettyPadLabel("agent"), []string{
		"abcdefghijklmnopqrstuvwxyz0123456789ABCDEFGHIJKLMNOPQRSTUVWXYZ",
	})
	got := strings.TrimRight(buf.String(), "\n")
	lines := strings.Split(got, "\n")
	if len(lines) < 2 {
		t.Fatalf("pretty output line count = %d, want wrapped continuation; output=%q", len(lines), got)
	}
	for i, line := range lines {
		if prettyVisibleWidth(line) > 56 {
			t.Fatalf("line %d width = %d, want <= 56: %q", i, prettyVisibleWidth(line), line)
		}
	}
	for _, line := range lines[1:] {
		if !strings.Contains(line, "┆") {
			t.Fatalf("wrapped continuation line missing rail: %q in output %q", line, got)
		}
	}
}

func TestPrettyWrapANSI_PreservesStyleAcrossWrappedChunks(t *testing.T) {
	t.Setenv("NO_COLOR", "")
	t.Setenv("TERM", "xterm-256color")
	chunks := prettyWrapANSI(dim("abcdefghijklmnopqrstuvwxyz"), 10)
	if len(chunks) != 3 {
		t.Fatalf("chunk count = %d, want 3: %#v", len(chunks), chunks)
	}
	for i, chunk := range chunks {
		if prettyVisibleWidth(chunk) > 10 {
			t.Fatalf("chunk %d visible width = %d, want <= 10: %q", i, prettyVisibleWidth(chunk), chunk)
		}
		if !strings.HasPrefix(chunk, ansiDim) {
			t.Fatalf("chunk %d missing dim prefix: %q", i, chunk)
		}
		if !strings.HasSuffix(chunk, ansiReset) {
			t.Fatalf("chunk %d missing reset suffix: %q", i, chunk)
		}
	}
}

func TestRenderPrettyTimeline_InterleavesAgentAfterResult(t *testing.T) {
	var buf bytes.Buffer
	a := model.Agent{Name: "go-release-prep"}
	base := time.Date(2026, time.May, 19, 2, 34, 40, 0, time.UTC)
	events := []prettyRecordedEvent{
		{at: base, event: runner.ProgressEvent{Kind: "user", Prompt: "Prompt one"}},
		{at: base.Add(2 * time.Second), event: runner.ProgressEvent{Kind: "call", ToolType: "mcp", Tool: "git_tag_list", Round: 0}},
		{at: base.Add(3 * time.Second), event: runner.ProgressEvent{Kind: "result", Tool: "git_tag_list", ResultBytes: 14}},
		{at: base.Add(5 * time.Second), event: runner.ProgressEvent{Kind: "user", Prompt: "Prompt two"}},
	}
	rec := model.RunRecord{Time: model.RunTime{StartTs: base.Unix(), DurationMs: 7000}, Turns: []model.Turn{
		{Prompt: "Prompt one", Response: "Response one", PromptTokens: 10, CompletionTokens: 5, TotalTokens: 15},
		{Prompt: "Prompt two", Response: "Response two", PromptTokens: 12, CompletionTokens: 6, TotalTokens: 18},
	}}
	renderPrettyTimeline(&buf, a, rec, events, false, false)
	got := buf.String()
	userOne := strings.Index(got, "Prompt one")
	result := strings.Index(got, "git_tag_list")
	agentOne := strings.Index(got, "Response one")
	userTwo := strings.Index(got, "Prompt two")
	agentTwo := strings.LastIndex(got, "Response two")
	if userOne < 0 || result <= userOne || agentOne <= result || userTwo <= agentOne || agentTwo <= userTwo {
		t.Errorf("timeline order incorrect: %q", got)
	}
}

func TestRenderPrettyTimeline_UsesRecordedTimestamps(t *testing.T) {
	var buf bytes.Buffer
	a := model.Agent{Name: "go-release-prep"}
	base := time.Date(2026, time.May, 19, 2, 34, 40, 0, time.UTC)
	events := []prettyRecordedEvent{
		{at: base, event: runner.ProgressEvent{Kind: "user", Prompt: "Prompt one"}},
		{at: base.Add(4 * time.Second), event: runner.ProgressEvent{Kind: "result", Tool: "git_tag_list", ResultBytes: 14}},
	}
	rec := model.RunRecord{Time: model.RunTime{StartTs: base.Unix(), DurationMs: 6000}, Turns: []model.Turn{{Prompt: "Prompt one", Response: "Response one"}}}
	renderPrettyTimeline(&buf, a, rec, events, false, false)
	got := buf.String()
	if !strings.Contains(got, "[02:34:40]") {
		t.Errorf("timeline output %q missing first recorded timestamp", got)
	}
	if !strings.Contains(got, "[02:34:44]") {
		t.Errorf("timeline output %q missing later recorded timestamp", got)
	}
}

// --- buildHTTPClient ---

func TestBuildHTTPClient_ReturnsNonNil(t *testing.T) {
	cfg := model.Config{LLMEndpoint: "http://localhost:11434", LLMTimeout: 30 * time.Second}
	client := buildHTTPClient(cfg)
	if client == nil {
		t.Fatal("buildHTTPClient returned nil")
	}
}

// --- ANSI helpers ---

func TestYellow_WithNoColor(t *testing.T) {
	// When NO_COLOR is set, yellow() must return the bare string without ANSI codes.
	t.Setenv("NO_COLOR", "1")
	t.Setenv("TERM", "xterm")
	got := yellow("warn")
	if got != "warn" {
		t.Errorf("yellow() with NO_COLOR = %q, want bare string %q", got, "warn")
	}
}

func TestYellow_WithColor(t *testing.T) {
	// When color is enabled, yellow() must wrap the string with ANSI codes.
	t.Setenv("NO_COLOR", "")
	t.Setenv("TERM", "xterm-256color")
	got := yellow("warn")
	if got == "warn" {
		t.Errorf("yellow() with color enabled returned bare string; expected ANSI-wrapped output")
	}
	if !strings.Contains(got, "warn") {
		t.Errorf("yellow() %q missing original text", got)
	}
}

// --- RunOnce error paths ---

func TestRunOnce_NoArgs_RequiresAgentFile(t *testing.T) {
	var errOut bytes.Buffer
	code := RunOnce(nil, io.Discard, &errOut)
	if code != 2 {
		t.Errorf("exit code = %d, want 2", code)
	}
	if !strings.Contains(errOut.String(), "requires a path") {
		t.Errorf("stderr %q missing usage hint", errOut.String())
	}
}

func TestRunOnce_NonExistentFile_ReturnsError(t *testing.T) {
	dir := t.TempDir()
	var errOut bytes.Buffer
	code := RunOnce([]string{"--model", "llama3", dir + "/nonexistent.agent.md"}, io.Discard, &errOut)
	if code != 1 {
		t.Errorf("exit code = %d, want 1", code)
	}
}

func TestRunOnce_AgentWithoutModel_ReturnsError(t *testing.T) {
	// Write a minimal agent.md with no model set.
	dir := t.TempDir()
	agentFile := dir + "/test.agent.md"
	if err := writeFile(agentFile, "---\nname: test\n---\nSystem prompt.\n"); err != nil {
		t.Fatal(err)
	}
	var errOut bytes.Buffer
	// No --model flag → model must come from file or config; neither has it.
	code := RunOnce([]string{"--config", dir + "/none.yaml", agentFile}, io.Discard, &errOut)
	if code != 1 {
		t.Errorf("exit code = %d, want 1; stderr: %s", code, errOut.String())
	}
	if !strings.Contains(errOut.String(), "model") {
		t.Errorf("stderr %q missing 'model' hint", errOut.String())
	}
}

// --- RunChat error paths ---

func TestRunChat_MissingModel_ReturnsError(t *testing.T) {
	// No --model flag and no config → must report error.
	dir := t.TempDir()
	var errOut bytes.Buffer
	code := RunChat([]string{"--config", dir + "/none.yaml"}, strings.NewReader(""), io.Discard, &errOut)
	if code != 1 {
		t.Errorf("exit code = %d, want 1; stderr: %s", code, errOut.String())
	}
	if !strings.Contains(errOut.String(), "model") {
		t.Errorf("stderr %q missing 'model' hint", errOut.String())
	}
}

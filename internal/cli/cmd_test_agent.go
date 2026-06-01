package cli

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/tgpski/leather/internal/agent"
	"github.com/tgpski/leather/internal/logging"
	"github.com/tgpski/leather/internal/model"
	"github.com/tgpski/leather/internal/runner"
	"github.com/tgpski/leather/internal/session"
	"github.com/tgpski/leather/internal/tool"
)

// toolResponseFlag is a repeatable --tool-response flag that accumulates name=text pairs.
// Reserved for future use; values are accepted without error but not currently applied.
type toolResponseFlag struct {
	m map[string]string
}

func (f *toolResponseFlag) String() string { return "" }

func (f *toolResponseFlag) Set(s string) error {
	idx := strings.IndexByte(s, '=')
	if idx < 0 {
		return fmt.Errorf("expected name=text, got %q", s)
	}
	if f.m == nil {
		f.m = make(map[string]string)
	}
	f.m[s[:idx]] = s[idx+1:]
	return nil
}

// RunTestAgent implements the "leather test-agent" subcommand.
// It loads an agent definition, runs it using a MockLLM (no real LLM calls),
// prints the turn transcript, and exits 0 on success, 1 on error, 2 on usage error.
//
// Usage: leather test-agent <agent-file> [flags]
//
//	--lifecycle <file>            apply a *.lifecycle.yaml before running
//	--mock-response <text>        LLM response text (default: "mock response")
//	--tool-response <name>=<text> tool name to response text (repeatable, reserved for future use)
//	--max-tokens <n>              token budget (default 8192)
func RunTestAgent(args []string, stdout, stderr io.Writer) int {
	fs := newFlagSet("test-agent", stderr)

	var (
		lifecycleFile string
		mockResponse  string
		maxTokens     int
		toolResp      toolResponseFlag
	)
	fs.StringVar(&lifecycleFile, "lifecycle", "", "apply a *.lifecycle.yaml before running")
	fs.StringVar(&mockResponse, "mock-response", "mock response", "LLM response text")
	fs.IntVar(&maxTokens, "max-tokens", 8192, "token budget")
	// --tool-response: repeatable flag, reserved for future tool mock support.
	fs.Var(&toolResp, "tool-response", "tool name=response text mapping (repeatable, reserved for future use)")

	if !parseFlags(fs, args) {
		return 2
	}

	if fs.NArg() < 1 {
		fmt.Fprintln(stderr, "leather test-agent: requires a path to an *.agent.md file")
		return 2
	}

	agentFile, err := filepath.Abs(fs.Arg(0))
	if err != nil {
		fmt.Fprintf(stderr, "leather test-agent: %v\n", err)
		return 1
	}

	a, err := agent.LoadFile(agentFile)
	if err != nil {
		fmt.Fprintf(stderr, "leather test-agent: %v\n", err)
		return 1
	}

	if lifecycleFile != "" {
		a, err = applyTestLifecycle(a, lifecycleFile)
		if err != nil {
			fmt.Fprintf(stderr, "leather test-agent: %v\n", err)
			return 1
		}
	}

	// Ensure required runtime fields have sensible defaults for mock execution.
	if a.Model == "" {
		a.Model = "mock"
	}
	if a.Timeout == 0 {
		a.Timeout = 30 * time.Second
	}

	log := logging.NewWithWriter("test-agent", model.LogLevelError, stderr, false)

	emptyReg, err := tool.Load("")
	if err != nil {
		fmt.Fprintf(stderr, "leather test-agent: build tool registry: %v\n", err)
		return 1
	}

	mock := session.NewMockLLM(session.MockConfig{Response: mockResponse})
	r := &runner.Runner{
		Client:        mock,
		Registry:      emptyReg,
		Log:           log,
		MaxToolRounds: 5,
	}

	budget := model.TokenBudget{
		MaxTokens:          maxTokens,
		CompletionReserve:  1024,
		SummarizeThreshold: 0.85,
	}

	rec, err := r.Run(context.Background(), a, budget)
	if err != nil {
		fmt.Fprintf(stderr, "leather test-agent: run failed: %v\n", err)
		return 1
	}

	fmt.Fprintf(stdout, "--- test-agent: %s ---\n", rec.AgentName)
	for i, t := range rec.Turns {
		fmt.Fprintf(stdout, "\n[turn %d]\n", i+1)
		fmt.Fprintf(stdout, "user: %s\n", t.Prompt)
		fmt.Fprintf(stdout, "assistant: %s\n", t.Response)
	}
	fmt.Fprintln(stdout)

	if rec.Status == model.JobStatusError {
		fmt.Fprintf(stdout, "status: error\nerror: %s\n", rec.Error)
		fmt.Fprintf(stdout, "tokens: prompt=%d response=%d total=%d\n",
			rec.Tokens.Prompt, rec.Tokens.Response, rec.Tokens.Total)
		fmt.Fprintf(stdout, "duration: %dms\n", rec.Time.DurationMs)
		return 1
	}

	fmt.Fprintf(stdout, "status: success\n")
	fmt.Fprintf(stdout, "tokens: prompt=%d response=%d total=%d\n",
		rec.Tokens.Prompt, rec.Tokens.Response, rec.Tokens.Total)
	fmt.Fprintf(stdout, "duration: %dms\n", rec.Time.DurationMs)
	return 0
}

// applyTestLifecycle copies the agent source file and the lifecycle file into a
// temporary directory, calls agent.LoadDir to merge them, and returns the first
// resolved agent. The temporary directory is cleaned up before returning.
func applyTestLifecycle(a model.Agent, lifecycleFile string) (model.Agent, error) {
	tmpDir, err := os.MkdirTemp("", "leather-test-agent-*")
	if err != nil {
		return model.Agent{}, fmt.Errorf("cmd_test_agent/applyTestLifecycle: create temp dir: %w", err)
	}
	defer os.RemoveAll(tmpDir)

	// Copy the agent file, preserving its base name so agent.LoadDir can discover it.
	agentData, err := os.ReadFile(a.SourcePath)
	if err != nil {
		return model.Agent{}, fmt.Errorf("cmd_test_agent/applyTestLifecycle: read agent file: %w", err)
	}
	if err := os.WriteFile(filepath.Join(tmpDir, filepath.Base(a.SourcePath)), agentData, 0600); err != nil {
		return model.Agent{}, fmt.Errorf("cmd_test_agent/applyTestLifecycle: write agent file: %w", err)
	}

	// Copy the lifecycle file, preserving its base name.
	lcData, err := os.ReadFile(lifecycleFile)
	if err != nil {
		return model.Agent{}, fmt.Errorf("cmd_test_agent/applyTestLifecycle: read lifecycle file: %w", err)
	}
	if err := os.WriteFile(filepath.Join(tmpDir, filepath.Base(lifecycleFile)), lcData, 0600); err != nil {
		return model.Agent{}, fmt.Errorf("cmd_test_agent/applyTestLifecycle: write lifecycle file: %w", err)
	}

	agents, errs := agent.LoadDir(tmpDir)
	if len(errs) > 0 {
		return model.Agent{}, fmt.Errorf("cmd_test_agent/applyTestLifecycle: %v", errs[0])
	}
	if len(agents) == 0 {
		return model.Agent{}, fmt.Errorf("cmd_test_agent/applyTestLifecycle: no agents loaded from lifecycle dir")
	}
	return agents[0], nil
}

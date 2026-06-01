package cli

import (
	"strings"
	"testing"
	"time"

	"github.com/tgpski/leather/internal/model"
	"github.com/tgpski/leather/internal/session"
)

// testChatCfg returns a minimal Config suitable for runChatLoop tests.
func testChatCfg() model.Config {
	return model.Config{
		Model:              "test-model",
		LLMTimeout:         5 * time.Second,
		MaxTokens:          8192,
		CompletionReserve:  1024,
		SummarizeThreshold: 0.85,
		Temperature:        0.7,
	}
}

func testChatBudget(cfg model.Config) model.TokenBudget {
	return model.TokenBudget{
		MaxTokens:          cfg.MaxTokens,
		CompletionReserve:  cfg.CompletionReserve,
		SummarizeThreshold: cfg.SummarizeThreshold,
	}
}

var chatTests = []struct {
	name       string
	input      string
	response   string
	system     string
	stats      bool
	dev        bool
	wantCode   int
	wantOut    []string
	wantAbsent []string
}{
	{
		name:     "basic turn then quit",
		input:    "say hi\n/quit\n",
		response: "hello from model",
		wantCode: 0,
		wantOut:  []string{"hello from model", "bye"},
	},
	{
		name:     "slash quit exits cleanly",
		input:    "/quit\n",
		wantCode: 0,
		wantOut:  []string{"bye"},
	},
	{
		name:     "slash exit exits cleanly",
		input:    "/exit\n",
		wantCode: 0,
		wantOut:  []string{"bye"},
	},
	{
		name:     "slash stats shows ctx info",
		input:    "/stats\n/quit\n",
		wantCode: 0,
		wantOut:  []string{"ctx_used=", "ctx_remaining=", "messages="},
	},
	{
		name:     "slash reset clears context",
		input:    "hello\n/reset\n/quit\n",
		response: "ok",
		wantCode: 0,
		wantOut:  []string{"session reset"},
	},
	{
		name:     "slash help shows commands",
		input:    "/help\n/quit\n",
		wantCode: 0,
		wantOut:  []string{"/reset", "/stats", "/quit"},
	},
	{
		name:     "stats flag shows token counts",
		input:    "question\n/quit\n",
		response: "answer",
		stats:    true,
		wantCode: 0,
		wantOut:  []string{"prompt=", "completion=", "total=", "ctx_used="},
	},
	{
		name:     "dev flag shows ctx after turn",
		input:    "hello\n/quit\n",
		response: "hi",
		dev:      true,
		wantCode: 0,
		wantOut:  []string{"ctx_used=", "ctx_remaining="},
	},
	{
		name:     "system prompt is accepted without error",
		input:    "/quit\n",
		system:   "You are a test assistant.",
		wantCode: 0,
		wantOut:  []string{"bye"},
	},
	{
		name:       "stats absent when flag not set",
		input:      "hi\n/quit\n",
		response:   "hello",
		stats:      false,
		wantCode:   0,
		wantAbsent: []string{"prompt="},
	},
	{
		name:     "slash show prints context window",
		input:    "hello\n/show\n/quit\n",
		response: "hi",
		wantCode: 0,
		wantOut:  []string{"context window", "user", "assistant"},
	},
	{
		name:     "empty lines are skipped",
		input:    "\n\n\n/quit\n",
		wantCode: 0,
		wantOut:  []string{"bye"},
	},
	{
		name:     "eof exits cleanly",
		input:    "", // EOF immediately
		wantCode: 0,
	},
}

func TestWrapLines(t *testing.T) {
	cases := []struct {
		text  string
		width int
		want  []string
	}{
		{"short", 72, []string{"short"}},
		{"", 72, []string{""}},
		{"hello world foo", 10, []string{"hello", "world foo"}},
		// width=5: "a b c d e" breaks at last space in first 5 chars → "a b" + "c d e"
		{"a b c d e", 5, []string{"a b", "c d e"}},
		// no space within width: hard-cut at width boundary
		{"nospaces_longword", 8, []string{"nospaces", "_longwor", "d"}},
		{"line1\nline2", 72, []string{"line1", "line2"}},
	}
	for _, tc := range cases {
		got := wrapLines(tc.text, tc.width)
		if len(got) != len(tc.want) {
			t.Errorf("wrapLines(%q, %d) = %v, want %v", tc.text, tc.width, got, tc.want)
			continue
		}
		for i := range got {
			if got[i] != tc.want[i] {
				t.Errorf("wrapLines(%q, %d)[%d] = %q, want %q", tc.text, tc.width, i, got[i], tc.want[i])
			}
		}
	}
}

func TestRunChatLoop(t *testing.T) {
	for _, tt := range chatTests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := testChatCfg()
			cfg.Stats = tt.stats
			budget := testChatBudget(cfg)

			response := tt.response
			if response == "" {
				response = "mock response"
			}
			mock := session.NewMockLLM(session.MockConfig{Response: response})

			var out, errOut strings.Builder
			code := runChatLoop(cfg, budget, "bot", tt.system, tt.dev,
				mock, strings.NewReader(tt.input), &out, &errOut)

			if code != tt.wantCode {
				t.Fatalf("exit code = %d, want %d\nstdout: %s\nstderr: %s",
					code, tt.wantCode, out.String(), errOut.String())
			}
			for _, want := range tt.wantOut {
				if !strings.Contains(out.String(), want) {
					t.Errorf("output missing %q\nfull output:\n%s", want, out.String())
				}
			}
			for _, absent := range tt.wantAbsent {
				if strings.Contains(out.String(), absent) {
					t.Errorf("output should not contain %q\nfull output:\n%s", absent, out.String())
				}
			}
		})
	}
}

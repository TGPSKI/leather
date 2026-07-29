package session

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/TGPSKI/leather/internal/model"
)

func writeFixture(t *testing.T, lines string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "fixture.jsonl")
	if err := os.WriteFile(path, []byte(lines), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestFixtureClient_ReplaysInOrderIncludingToolCalls(t *testing.T) {
	path := writeFixture(t, `
# comment lines and blanks are allowed
{"finish_reason":"tool_calls","tool_calls":[{"id":"c1","name":"git_log","arguments":{"ref":"HEAD"}}]}
{"content":"done","finish_reason":"stop","completion_tokens":2}
`)
	c, err := NewFixtureClient(path)
	if err != nil {
		t.Fatal(err)
	}

	first, err := c.Complete(context.Background(), "m", nil, CompletionOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if first.FinishReason != "tool_calls" || len(first.ToolCalls) != 1 || first.ToolCalls[0].Name != "git_log" {
		t.Fatalf("first response = %+v, want the recorded tool call", first)
	}

	second, err := c.Complete(context.Background(), "m", nil, CompletionOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if second.Content != "done" || second.FinishReason != "stop" {
		t.Fatalf("second response = %+v, want recorded final answer", second)
	}
}

func TestFixtureClient_ExhaustionFailsLoudly(t *testing.T) {
	path := writeFixture(t, `{"content":"only one","finish_reason":"stop"}`)
	c, err := NewFixtureClient(path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := c.Complete(context.Background(), "m", nil, CompletionOptions{}); err != nil {
		t.Fatal(err)
	}
	_, err = c.Complete(context.Background(), "m",
		[]model.Message{{Role: "user", Content: "the divergent prompt"}}, CompletionOptions{})
	if err == nil {
		t.Fatal("want exhaustion error, got nil")
	}
	if !strings.Contains(err.Error(), "exhausted at call 2") || !strings.Contains(err.Error(), "the divergent prompt") {
		t.Fatalf("exhaustion error should name call index and last message, got: %v", err)
	}
}

func TestFixtureClient_EmptyFileRejected(t *testing.T) {
	path := writeFixture(t, "# only a comment\n")
	if _, err := NewFixtureClient(path); err == nil {
		t.Fatal("want error for fixture with no records")
	}
}

func TestRecordingClient_RoundTripsThroughFixture(t *testing.T) {
	inner := NewMockLLM(MockConfig{
		Response: "recorded answer",
		ToolCallSequence: [][]model.ToolCall{
			{{ID: "c1", Name: "lookup", Arguments: map[string]any{"q": "x"}}},
		},
	})
	out := filepath.Join(t.TempDir(), "rec.jsonl")
	rec, err := NewRecordingClient(inner, out)
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ {
		if _, err := rec.Complete(context.Background(), "m", nil, CompletionOptions{}); err != nil {
			t.Fatal(err)
		}
	}
	if err := rec.Close(); err != nil {
		t.Fatal(err)
	}

	// The recording must replay identically.
	replay, err := NewFixtureClient(out)
	if err != nil {
		t.Fatal(err)
	}
	first, err := replay.Complete(context.Background(), "m", nil, CompletionOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if first.FinishReason != "tool_calls" || len(first.ToolCalls) != 1 || first.ToolCalls[0].Arguments["q"] != "x" {
		t.Fatalf("replayed first call = %+v, want recorded tool call", first)
	}
	second, err := replay.Complete(context.Background(), "m", nil, CompletionOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if second.Content != "recorded answer" {
		t.Fatalf("replayed second call = %+v, want recorded answer", second)
	}
}

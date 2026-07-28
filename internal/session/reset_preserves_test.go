package session

import (
	"context"
	"testing"

	"github.com/TGPSKI/leather/internal/model"
)

// Reset is what per-turn `clear: true` calls. It must drop the conversation while keeping
// the agent's identity, or a cleared turn would lose its instructions along with the blob
// it meant to discard.
func TestResetKeepsSystemDropsRest(t *testing.T) {
	mock := NewMockLLM(MockConfig{TokensPerMessage: 5})
	s := New(smallBudget(), "llama3", mock)
	ctx := context.Background()
	for _, m := range []model.Message{
		{Role: "system", Content: "you are the matcher"},
		{Role: "user", Content: "the issue"},
		{Role: "assistant", Content: "a tool call"},
		{Role: "tool", Content: "6000 bytes of catalog"},
	} {
		if err := s.Add(ctx, m); err != nil {
			t.Fatal(err)
		}
	}
	if got := len(s.Messages()); got != 4 {
		t.Fatalf("pre-reset messages = %d, want 4", got)
	}
	s.Reset()
	msgs := s.Messages()
	if len(msgs) != 1 {
		t.Fatalf("post-reset messages = %d, want 1", len(msgs))
	}
	if msgs[0].Role != "system" || msgs[0].Content != "you are the matcher" {
		t.Fatalf("system message not preserved: %+v", msgs[0])
	}
	used, _ := s.Usage()
	if used != msgs[0].Tokens {
		t.Fatalf("token accounting not reset: used=%d, system=%d", used, msgs[0].Tokens)
	}
}

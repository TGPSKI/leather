package session

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/tgpski/leather/internal/model"
)

func smallBudget() model.TokenBudget {
	return model.TokenBudget{
		MaxTokens:          100,
		CompletionReserve:  20,
		SummarizeThreshold: 0.5, // trigger at 50 tokens
	}
}

func TestSession_AddAndMessages(t *testing.T) {
	mock := NewMockLLM(MockConfig{TokensPerMessage: 5})
	sess := New(smallBudget(), "llama3", mock)

	msg := model.Message{Role: "user", Content: "hello"}
	if err := sess.Add(context.Background(), msg); err != nil {
		t.Fatalf("Add: %v", err)
	}

	msgs := sess.Messages()
	if len(msgs) != 1 {
		t.Fatalf("expected 1 message, got %d", len(msgs))
	}
	if msgs[0].Role != "user" || msgs[0].Content != "hello" {
		t.Errorf("message mismatch: %+v", msgs[0])
	}
}

func TestSession_UsageAccounting(t *testing.T) {
	mock := NewMockLLM(MockConfig{TokensPerMessage: 10})
	sess := New(smallBudget(), "llama3", mock)

	_ = sess.Add(context.Background(), model.Message{Role: "user", Content: "a"})
	used, remaining := sess.Usage()
	if used != 10 {
		t.Errorf("used = %d, want 10", used)
	}
	// remaining = MaxTokens(100) - CompletionReserve(20) - used(10) = 70
	if remaining != 70 {
		t.Errorf("remaining = %d, want 70", remaining)
	}
}

func TestSession_SummarizationTrigger(t *testing.T) {
	// TokensPerMessage=30, threshold=50 → two messages (60 tokens) will trigger.
	mock := NewMockLLM(MockConfig{
		TokensPerMessage: 30,
		Response:         "This is a summary.",
	})
	sess := New(smallBudget(), "llama3", mock)

	_ = sess.Add(context.Background(), model.Message{Role: "user", Content: "message one"})
	if err := sess.Add(context.Background(), model.Message{Role: "user", Content: "message two"}); err != nil {
		t.Fatalf("Add (trigger): %v", err)
	}

	// Summarization should have fired — mock should have been called.
	if mock.CallCount() == 0 {
		t.Error("expected summarization call, got none")
	}

	// After summarization, the message list should be collapsed.
	msgs := sess.Messages()
	for _, m := range msgs {
		if m.Summarized {
			return // found the summary message — test passes
		}
	}
	t.Errorf("no summarized message found after trigger; messages: %+v", msgs)
}

func TestSession_Reset_PreservesSystem(t *testing.T) {
	mock := NewMockLLM(MockConfig{TokensPerMessage: 5})
	sess := New(smallBudget(), "llama3", mock)

	sys := model.Message{Role: "system", Content: "You are a helpful assistant."}
	_ = sess.Add(context.Background(), sys)
	_ = sess.Add(context.Background(), model.Message{Role: "user", Content: "hello"})

	sess.Reset()

	msgs := sess.Messages()
	if len(msgs) != 1 {
		t.Fatalf("after Reset expected 1 message (system), got %d", len(msgs))
	}
	if msgs[0].Role != "system" {
		t.Errorf("preserved message role = %q, want system", msgs[0].Role)
	}
}

func TestSession_Reset_NoSystem(t *testing.T) {
	mock := NewMockLLM(MockConfig{TokensPerMessage: 5})
	sess := New(smallBudget(), "llama3", mock)

	_ = sess.Add(context.Background(), model.Message{Role: "user", Content: "hello"})
	sess.Reset()

	if msgs := sess.Messages(); len(msgs) != 0 {
		t.Errorf("after Reset (no system) expected 0 messages, got %d", len(msgs))
	}
	used, _ := sess.Usage()
	if used != 0 {
		t.Errorf("after Reset used = %d, want 0", used)
	}
}

func TestSession_SummarizationError(t *testing.T) {
	errLLM := NewMockLLM(MockConfig{
		TokensPerMessage: 30,
		Err:              errors.New("model unavailable"),
	})
	sess := New(smallBudget(), "llama3", errLLM)

	_ = sess.Add(context.Background(), model.Message{Role: "user", Content: "one"})
	err := sess.Add(context.Background(), model.Message{Role: "user", Content: "two"})
	if err == nil {
		t.Error("expected error from summarization failure, got nil")
	}
}

func TestSession_CompactLatestHidePage_ReplacesRawContent(t *testing.T) {
	mock := NewMockLLM(MockConfig{TokensPerMessage: 5})
	sess := New(smallBudget(), "llama3", mock)
	raw := "[HIDE id=hide_test source=cli page=1/2 bytes=100]\nraw page body\n[END page 1/2]"
	if err := sess.Add(context.Background(), model.Message{Role: "user", Content: raw}); err != nil {
		t.Fatalf("Add hide: %v", err)
	}
	if err := sess.Add(context.Background(), model.Message{Role: "assistant", Content: "page facts"}); err != nil {
		t.Fatalf("Add assistant: %v", err)
	}
	changed, err := sess.CompactLatestHidePage(context.Background(), "page facts")
	if err != nil {
		t.Fatalf("CompactLatestHidePage: %v", err)
	}
	if !changed {
		t.Fatal("expected hide compaction to occur")
	}
	msgs := sess.Messages()
	if len(msgs) != 1 {
		t.Fatalf("message count after compaction = %d, want 1", len(msgs))
	}
	if msgs[0].Role != "assistant" || msgs[0].Content != "page facts" {
		t.Fatalf("assistant summary should remain as the only message: %+v", msgs[0])
	}
}

func TestEstimateTokens(t *testing.T) {
	msgs := []model.Message{
		{Content: "hello"}, // 4 overhead + (5+3)/4 = 4+2 = 6
		{Content: ""},      // 4 overhead + 0 = 4
	}
	got := estimateTokens(msgs)
	// "hello" → 4 + 2 = 6; "" → 4 + 0 = 4; total = 10
	if got != 10 {
		t.Errorf("estimateTokens = %d, want 10", got)
	}
}

func TestSession_Snapshot(t *testing.T) {
	mock := NewMockLLM(MockConfig{TokensPerMessage: 5})
	sess := New(smallBudget(), "llama3", mock)

	_ = sess.Add(context.Background(), model.Message{Role: "user", Content: "hello"})
	_ = sess.Add(context.Background(), model.Message{Role: "assistant", Content: "hi"})

	meta := map[string]string{"agent": "test-agent", "job_id": "42"}
	snap := sess.Snapshot(meta)

	if len(snap.Messages) != 2 {
		t.Fatalf("Snapshot.Messages len = %d, want 2", len(snap.Messages))
	}
	if snap.UsedTokens != 10 {
		t.Errorf("Snapshot.UsedTokens = %d, want 10", snap.UsedTokens)
	}
	if snap.Metadata["agent"] != "test-agent" {
		t.Errorf("Snapshot.Metadata[agent] = %q, want test-agent", snap.Metadata["agent"])
	}

	// Snapshot is a copy — mutations do not affect the session.
	snap.Messages[0].Content = "mutated"
	live := sess.Messages()
	if live[0].Content == "mutated" {
		t.Error("Snapshot mutation affected live session messages")
	}
}

func TestSession_Snapshot_NilMetadata(t *testing.T) {
	mock := NewMockLLM(MockConfig{TokensPerMessage: 5})
	sess := New(smallBudget(), "llama3", mock)
	snap := sess.Snapshot(nil)
	if snap.Messages == nil {
		t.Error("Snapshot.Messages should be non-nil for empty session")
	}
	if snap.UsedTokens != 0 {
		t.Errorf("Snapshot.UsedTokens = %d, want 0", snap.UsedTokens)
	}
}

func TestHTTPClient_CompleteNormalizesTokenTotal(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"choices": [{"message": {"role": "assistant", "content": "ok"}, "finish_reason": "stop"}],
			"usage": {"prompt_tokens": 1215, "completion_tokens": 53, "total_tokens": 12}
		}`))
	}))
	defer srv.Close()

	client := NewHTTPClient(srv.URL, "", time.Second)
	resp, err := client.Complete(context.Background(), "test-model", []model.Message{{Role: "user", Content: "hello"}}, CompletionOptions{MaxTokens: 10})
	if err != nil {
		t.Fatalf("Complete: %v", err)
	}
	if resp.TotalTokens != 1268 {
		t.Fatalf("TotalTokens = %d, want 1268", resp.TotalTokens)
	}
}

// --- MockLLM.Calls ---

func TestMockLLM_Calls_RecordsInvocations(t *testing.T) {
	ctx := context.Background()
	mock := NewMockLLM(MockConfig{Response: "pong", TokensPerMessage: 3})

	msgs1 := []model.Message{{Role: "user", Content: "ping1"}}
	msgs2 := []model.Message{{Role: "user", Content: "ping2"}, {Role: "assistant", Content: "pong"}}

	if _, err := mock.Complete(ctx, "test-model", msgs1, CompletionOptions{}); err != nil {
		t.Fatalf("Complete 1: %v", err)
	}
	if _, err := mock.Complete(ctx, "test-model", msgs2, CompletionOptions{}); err != nil {
		t.Fatalf("Complete 2: %v", err)
	}

	calls := mock.Calls()
	if len(calls) != 2 {
		t.Fatalf("Calls() len = %d, want 2", len(calls))
	}
	if len(calls[0]) != 1 {
		t.Errorf("calls[0] len = %d, want 1", len(calls[0]))
	}
	if len(calls[1]) != 2 {
		t.Errorf("calls[1] len = %d, want 2", len(calls[1]))
	}
}

package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/tgpski/leather/internal/agent"
	"github.com/tgpski/leather/internal/cache"
	"github.com/tgpski/leather/internal/model"
	"github.com/tgpski/leather/internal/queue"
	"github.com/tgpski/leather/internal/scheduler"
	"github.com/tgpski/leather/internal/session"
	"github.com/tgpski/leather/internal/tool"
)

// BenchmarkCronParse measures schedule expression parsing via the scheduler.
func BenchmarkCronParse(b *testing.B) {
	b.ReportAllocs()
	sched := scheduler.New(scheduler.Options{})
	noop := func(_ context.Context, _ model.Job) error { return nil }
	for i := 0; i < b.N; i++ {
		name := "bench-job"
		_ = sched.Register(name, "0 9 * * *", noop)
	}
}

// BenchmarkAgentLoadDir measures agent directory scanning and parsing.
func BenchmarkAgentLoadDir(b *testing.B) {
	b.ReportAllocs()
	dir := b.TempDir()
	// Write 10 agent files with paired lifecycle files.
	for i := 0; i < 10; i++ {
		mdPath := filepath.Join(dir, fmt.Sprintf("bench-agent-%d.agent.md", i))
		lyPath := filepath.Join(dir, fmt.Sprintf("bench-agent-%d.lifecycle.yaml", i))
		if err := os.WriteFile(mdPath, []byte(fmt.Sprintf("---\nname: bench-agent-%d\n---\nSystem prompt.\n", i)), 0600); err != nil {
			b.Fatalf("WriteFile %s: %v", mdPath, err)
		}
		if err := os.WriteFile(lyPath, []byte(fmt.Sprintf("agent: bench-agent-%d\nschedule: \"0 * * * *\"\nmodel: llama3\n", i)), 0600); err != nil {
			b.Fatalf("WriteFile %s: %v", lyPath, err)
		}
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = agent.LoadDir(dir)
	}
}

// BenchmarkSessionAdd measures the hot path of adding messages to a session.
func BenchmarkSessionAdd(b *testing.B) {
	b.ReportAllocs()
	budget := model.TokenBudget{
		MaxTokens:          8192,
		CompletionReserve:  1024,
		SummarizeThreshold: 0.85,
	}
	client := session.NewMockLLM(session.MockConfig{TokensPerMessage: 10})
	sess := session.New(budget, "llama3", client)
	msg := model.Message{Role: "user", Content: "benchmark message payload"}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = sess.Add(context.Background(), msg)
	}
}

// BenchmarkCacheAgentRunKey measures SHA-256 key computation for cache lookup.
func BenchmarkCacheAgentRunKey(b *testing.B) {
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = cache.AgentRunKey("my-agent", "system prompt text", "summarize the news today", "llama3")
	}
}

// BenchmarkCacheGetSet measures a cache round-trip (Set then Get).
func BenchmarkCacheGetSet(b *testing.B) {
	b.ReportAllocs()
	dir := b.TempDir()
	fc, err := cache.NewFileCache(dir)
	if err != nil {
		b.Fatalf("NewFileCache: %v", err)
	}
	key := cache.AgentRunKey("bench-agent", "system prompt", "user prompt", "llama3")
	content := "cached response content for benchmark"
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = fc.Set(key, content, 0)
		_, _ = fc.Get(key)
	}
}

// BenchmarkToolLoad measures skill YAML loading from a directory.
func BenchmarkToolLoad(b *testing.B) {
	b.ReportAllocs()
	dir := b.TempDir()
	skillYAML := `
name: bench-skill
tools:
  - name: bench_tool
    type: http
    http:
      method: GET
      url: https://example.com/bench
`
	for i := 0; i < 5; i++ {
		path := filepath.Join(dir, fmt.Sprintf("skill-%d.skill.yaml", i))
		src := fmt.Sprintf("name: skill-%d\ntools:\n  - name: tool_%d\n    type: http\n    http:\n      method: GET\n      url: https://example.com/%d\n", i, i, i)
		if err := os.WriteFile(path, []byte(src), 0600); err != nil {
			b.Fatalf("WriteFile %s: %v", path, err)
		}
	}
	_ = skillYAML
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = tool.Load(dir)
	}
}

// BenchmarkQueueEnqueueDequeue measures a single enqueue+dequeue round-trip,
// keeping the queue size bounded to avoid unbounded file growth.
func BenchmarkQueueEnqueueDequeue(b *testing.B) {
	b.ReportAllocs()
	dir := b.TempDir()
	mgr := queue.NewManager(dir)
	q, err := mgr.Get("bench-queue")
	if err != nil {
		b.Fatalf("Get: %v", err)
	}
	item := model.QueueItem{
		ID:         "bench-item",
		AgentName:  "bench-agent",
		Payload:    map[string]any{"content": "bench payload"},
		EnqueuedAt: 1000000,
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = q.Enqueue(item)
		_, _, _ = q.Dequeue()
	}
}

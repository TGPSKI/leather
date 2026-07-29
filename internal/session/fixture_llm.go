package session

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"sync"

	"github.com/TGPSKI/leather/internal/model"
)

// fixtureRecord is the JSONL wire form of one recorded completion. It is the
// contract between --llm-record output and --llm-fixture input; keep the
// field set in sync with model.LLMResponse.
type fixtureRecord struct {
	Content          string           `json:"content,omitempty"`
	FinishReason     string           `json:"finish_reason"`
	PromptTokens     int              `json:"prompt_tokens,omitempty"`
	CompletionTokens int              `json:"completion_tokens,omitempty"`
	TotalTokens      int              `json:"total_tokens,omitempty"`
	ToolCalls        []model.ToolCall `json:"tool_calls,omitempty"`
}

func (r fixtureRecord) toResponse() model.LLMResponse {
	return model.LLMResponse{
		Content:          r.Content,
		FinishReason:     r.FinishReason,
		PromptTokens:     r.PromptTokens,
		CompletionTokens: r.CompletionTokens,
		TotalTokens:      r.TotalTokens,
		ToolCalls:        r.ToolCalls,
	}
}

// FixtureClient is an LLMClient that replays completions from a JSONL file,
// one line per Complete call, in order. It makes a full pipeline runnable
// with no model behind it — example smoke tests, CI wiring proofs — and
// fails loudly (with the call's position and last message) when the run
// diverges from the recording, instead of improvising.
//
// Replay is strictly sequential across the whole process; run fixture-backed
// configs with max_concurrent_jobs: 1 so call order is deterministic.
type FixtureClient struct {
	path    string
	records []fixtureRecord

	mu  sync.Mutex
	idx int
}

// NewFixtureClient loads a JSONL fixture file. Blank lines and lines starting
// with # are skipped so fixtures can carry comments.
func NewFixtureClient(path string) (*FixtureClient, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("session/NewFixtureClient: %w", err)
	}
	var records []fixtureRecord
	for i, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		var r fixtureRecord
		if err := json.Unmarshal([]byte(line), &r); err != nil {
			return nil, fmt.Errorf("session/NewFixtureClient: %s:%d: %w", path, i+1, err)
		}
		if r.FinishReason == "" {
			r.FinishReason = "stop"
		}
		records = append(records, r)
	}
	if len(records) == 0 {
		return nil, fmt.Errorf("session/NewFixtureClient: %s: no fixture records", path)
	}
	return &FixtureClient{path: path, records: records}, nil
}

// Complete returns the next recorded response. An exhausted fixture is an
// error: the run made more model calls than the recording, which means the
// wiring changed — the failure names the call index and the last message so
// the divergence is diagnosable.
func (c *FixtureClient) Complete(_ context.Context, _ string, messages []model.Message, _ CompletionOptions) (model.LLMResponse, error) {
	c.mu.Lock()
	idx := c.idx
	if idx < len(c.records) {
		c.idx++
	}
	c.mu.Unlock()

	if idx >= len(c.records) {
		last := ""
		if n := len(messages); n > 0 {
			last = messages[n-1].Content
			if len(last) > 120 {
				last = last[:120] + "…"
			}
		}
		return model.LLMResponse{}, fmt.Errorf("session/FixtureClient: fixture %s exhausted at call %d (last message: %q)", c.path, idx+1, last)
	}
	return c.records[idx].toResponse(), nil
}

// CountTokens estimates ~4 bytes per token, like HTTPClient's local estimate,
// so budget math behaves the same with and without a live model.
func (c *FixtureClient) CountTokens(messages []model.Message) (int, error) {
	total := 0
	for _, m := range messages {
		total += (len(m.Content) + 3) / 4
	}
	return total, nil
}

// RecordingClient wraps a live LLMClient and appends every successful
// completion to a JSONL file in FixtureClient's format, so a live run can be
// captured once (--llm-record) and replayed forever (--llm-fixture).
type RecordingClient struct {
	inner LLMClient

	mu sync.Mutex
	f  *os.File
}

// NewRecordingClient opens (truncating) the output file and wraps inner.
func NewRecordingClient(inner LLMClient, path string) (*RecordingClient, error) {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
	if err != nil {
		return nil, fmt.Errorf("session/NewRecordingClient: %w", err)
	}
	return &RecordingClient{inner: inner, f: f}, nil
}

// Complete delegates to the wrapped client and appends the response.
// Record-write failures fail the call: a silently incomplete recording would
// replay as a confusing mid-run divergence later.
func (c *RecordingClient) Complete(ctx context.Context, modelName string, messages []model.Message, opts CompletionOptions) (model.LLMResponse, error) {
	resp, err := c.inner.Complete(ctx, modelName, messages, opts)
	if err != nil {
		return resp, err
	}
	rec := fixtureRecord{
		Content:          resp.Content,
		FinishReason:     resp.FinishReason,
		PromptTokens:     resp.PromptTokens,
		CompletionTokens: resp.CompletionTokens,
		TotalTokens:      resp.TotalTokens,
		ToolCalls:        resp.ToolCalls,
	}
	line, err := json.Marshal(rec)
	if err != nil {
		return resp, fmt.Errorf("session/RecordingClient: marshal: %w", err)
	}
	c.mu.Lock()
	_, werr := c.f.Write(append(line, '\n'))
	c.mu.Unlock()
	if werr != nil {
		return resp, fmt.Errorf("session/RecordingClient: write: %w", werr)
	}
	return resp, nil
}

// CountTokens delegates to the wrapped client.
func (c *RecordingClient) CountTokens(messages []model.Message) (int, error) {
	return c.inner.CountTokens(messages)
}

// Close closes the recording file.
func (c *RecordingClient) Close() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.f.Close()
}

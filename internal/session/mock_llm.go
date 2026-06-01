package session

import (
	"context"
	"sync"

	"github.com/tgpski/leather/internal/model"
)

// MockConfig configures the behavior of a MockLLM.
type MockConfig struct {
	// Response is the content returned by Complete calls. Defaults to "mock response".
	Response string
	// TokensPerMessage is the fixed token count returned by CountTokens per message.
	// Defaults to 10.
	TokensPerMessage int
	// Err, if non-nil, is returned by Complete instead of a response.
	Err error
	// ToolCallSequence is consumed one entry per Complete call. When the current
	// index has a non-nil slice, those ToolCalls are returned with FinishReason
	// "tool_calls" before falling back to Response. Once the sequence is exhausted,
	// the normal Response is returned.
	ToolCallSequence [][]model.ToolCall
}

// MockLLM is a deterministic test double for LLMClient.
// It records all Complete calls so tests can assert on invocation counts and inputs.
type MockLLM struct {
	cfg     MockConfig
	mu      sync.Mutex
	calls   [][]model.Message
	toolIdx int
}

// NewMockLLM returns a MockLLM with the given configuration.
func NewMockLLM(cfg MockConfig) *MockLLM {
	if cfg.TokensPerMessage == 0 {
		cfg.TokensPerMessage = 10
	}
	return &MockLLM{cfg: cfg}
}

// Complete records the call and returns tool calls from the sequence (if available),
// then the fixed response or the configured error. It is safe for concurrent use.
func (m *MockLLM) Complete(_ context.Context, _ string, messages []model.Message, _ CompletionOptions) (model.LLMResponse, error) {
	m.mu.Lock()
	m.calls = append(m.calls, append([]model.Message(nil), messages...))
	idx := m.toolIdx
	if idx < len(m.cfg.ToolCallSequence) {
		m.toolIdx++
	}
	m.mu.Unlock()

	if m.cfg.Err != nil {
		return model.LLMResponse{}, m.cfg.Err
	}

	if idx < len(m.cfg.ToolCallSequence) && len(m.cfg.ToolCallSequence[idx]) > 0 {
		return model.LLMResponse{
			FinishReason: "tool_calls",
			ToolCalls:    m.cfg.ToolCallSequence[idx],
		}, nil
	}

	content := m.cfg.Response
	if content == "" {
		content = "mock response"
	}
	tokens := (len(content) + 3) / 4
	return model.LLMResponse{
		Content:          content,
		FinishReason:     "stop",
		CompletionTokens: tokens,
		TotalTokens:      tokens,
	}, nil
}

// CountTokens returns TokensPerMessage * len(messages) as a deterministic estimate.
func (m *MockLLM) CountTokens(messages []model.Message) (int, error) {
	return m.cfg.TokensPerMessage * len(messages), nil
}

// Calls returns a copy of all Complete invocation arguments received so far.
func (m *MockLLM) Calls() [][]model.Message {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([][]model.Message, len(m.calls))
	copy(out, m.calls)
	return out
}

// CallCount returns the number of Complete calls received.
func (m *MockLLM) CallCount() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.calls)
}

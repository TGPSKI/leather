// Package session manages the context window for a single agent execution.
// It tracks token budgets, triggers summarization, and wraps LLM communication.
package session

import (
	"context"
	"fmt"
	"strings"

	"github.com/tgpski/leather/internal/model"
)

// LLMClient is the interface leather uses to communicate with a model backend.
// Production code uses HTTPClient; tests use MockLLM.
type LLMClient interface {
	// Complete sends messages to the model and returns the generated response.
	Complete(ctx context.Context, modelName string, messages []model.Message, opts CompletionOptions) (model.LLMResponse, error)
	// CountTokens returns a token estimate for messages without making a completion call.
	CountTokens(messages []model.Message) (int, error)
}

// CompletionOptions carries per-call settings for a model request.
type CompletionOptions struct {
	MaxTokens   int
	Temperature float64
	// ExtraBody contains additional top-level fields merged into the API request
	// body verbatim. Use for model-specific parameters such as
	// chat_template_kwargs for Qwen3 thinking mode control.
	ExtraBody map[string]any
	// Tools is the list of tool definitions to make available to the model.
	// Empty means no tool calling.
	Tools []model.ToolDefinition
}

// Session manages the context window for a single agent execution.
type Session struct {
	budget   model.TokenBudget
	client   LLMClient
	model    string
	messages []model.Message
	used     int
}

// New returns a Session initialized with the given budget, model name, and LLM client.
func New(budget model.TokenBudget, modelName string, client LLMClient) *Session {
	return &Session{
		budget: budget,
		client: client,
		model:  modelName,
	}
}

// Add appends msg to the context window, counts its tokens, and triggers
// summarization if usage exceeds the configured threshold.
func (s *Session) Add(ctx context.Context, msg model.Message) error {
	tokens, err := s.client.CountTokens([]model.Message{msg})
	if err != nil {
		return fmt.Errorf("session/Add: count tokens: %w", err)
	}
	msg.Tokens = tokens
	s.messages = append(s.messages, msg)
	s.used += tokens

	threshold := int(float64(s.budget.MaxTokens) * s.budget.SummarizeThreshold)
	if s.used >= threshold {
		if err := s.summarize(ctx); err != nil {
			return fmt.Errorf("session/Add: summarize: %w", err)
		}
	}
	return nil
}

// Messages returns a copy of the current message list.
func (s *Session) Messages() []model.Message {
	out := make([]model.Message, len(s.messages))
	copy(out, s.messages)
	return out
}

// Usage returns the current token count and remaining capacity for completions.
func (s *Session) Usage() (used, remaining int) {
	remaining = s.budget.MaxTokens - s.budget.CompletionReserve - s.used
	if remaining < 0 {
		remaining = 0
	}
	return s.used, remaining
}

// Snapshot returns a point-in-time copy of the session's context window
// as a model.SessionContext. The returned value is safe to inspect after
// the session continues to accumulate messages.
func (s *Session) Snapshot(metadata map[string]string) model.SessionContext {
	msgs := make([]model.Message, len(s.messages))
	copy(msgs, s.messages)
	return model.SessionContext{
		Messages:   msgs,
		UsedTokens: s.used,
		Metadata:   metadata,
	}
}

// CompactLatestHidePage removes the most recent completed hide-reflection cycle,
// keeping only the assistant's page summary in the session. This is used by
// reflection-mode paging so prior page bodies and hide_next scaffolding do not
// remain in context after the model has already summarized that page.
func (s *Session) CompactLatestHidePage(ctx context.Context, summary string) (bool, error) {
	summary = strings.TrimSpace(summary)
	if summary == "" {
		return false, nil
	}
	summaryIdx := len(s.messages) - 1
	if summaryIdx < 0 || s.messages[summaryIdx].Role != "assistant" {
		return false, nil
	}
	idx := -1
	for i := summaryIdx - 1; i >= 0; i-- {
		msg := s.messages[i]
		if !isHidePageMessage(msg) {
			continue
		}
		idx = i
		break
	}
	if idx == -1 {
		return false, nil
	}
	start := idx
	for start > 0 && isHideNavigationScaffold(s.messages[start-1]) {
		start--
	}
	kept := make([]model.Message, 0, len(s.messages)-(summaryIdx-start))
	kept = append(kept, s.messages[:start]...)
	kept = append(kept, s.messages[summaryIdx:]...)
	s.messages = kept

	total := 0
	for _, msg := range s.messages {
		total += msg.Tokens
	}
	s.used = total
	threshold := int(float64(s.budget.MaxTokens) * s.budget.SummarizeThreshold)
	if s.used >= threshold {
		if err := s.summarize(ctx); err != nil {
			return false, fmt.Errorf("session/CompactLatestHidePage: summarize: %w", err)
		}
	}
	return true, nil
}

// Reset clears the context window. If the first message has role "system",
// it is preserved so the agent's identity is not lost.
func (s *Session) Reset() {
	if len(s.messages) > 0 && s.messages[0].Role == "system" {
		sys := s.messages[0]
		s.messages = []model.Message{sys}
		s.used = sys.Tokens
	} else {
		s.messages = s.messages[:0]
		s.used = 0
	}
}

// summarize collapses all non-system messages into a single summary message,
// reducing token usage while preserving conversational context.
func (s *Session) summarize(ctx context.Context) error {
	var systemMsg *model.Message
	var toSummarize []model.Message

	for i := range s.messages {
		if s.messages[i].Role == "system" {
			cp := s.messages[i]
			systemMsg = &cp
		} else {
			toSummarize = append(toSummarize, s.messages[i])
		}
	}
	if len(toSummarize) == 0 {
		return nil
	}

	var sb strings.Builder
	sb.WriteString("Summarize the following conversation in one concise paragraph, preserving key decisions and context:\n\n")
	for _, m := range toSummarize {
		sb.WriteString(m.Role)
		sb.WriteString(": ")
		sb.WriteString(m.Content)
		sb.WriteString("\n")
	}

	summaryReq := model.Message{Role: "user", Content: sb.String()}
	opts := CompletionOptions{
		MaxTokens:   s.budget.CompletionReserve,
		Temperature: 0.3,
	}
	resp, err := s.client.Complete(ctx, s.model, []model.Message{summaryReq}, opts)
	if err != nil {
		return fmt.Errorf("session/summarize: %w", err)
	}

	summaryMsg := model.Message{
		Role:       "assistant",
		Content:    "[Summary] " + resp.Content,
		Tokens:     resp.CompletionTokens,
		Summarized: true,
	}

	// Rebuild: optional system message followed by the summary.
	var rebuilt []model.Message
	if systemMsg != nil {
		rebuilt = append(rebuilt, *systemMsg)
	}
	rebuilt = append(rebuilt, summaryMsg)
	s.messages = rebuilt

	total := 0
	for _, m := range s.messages {
		total += m.Tokens
	}
	s.used = total
	return nil
}

func isHidePageMessage(msg model.Message) bool {
	content := strings.TrimSpace(msg.Content)
	if !strings.HasPrefix(content, "[HIDE ") {
		return false
	}
	return msg.Role == "user" || msg.Role == "tool"
}

func isHideNavigationScaffold(msg model.Message) bool {
	if msg.Role == "user" && strings.Contains(msg.Content, "Now call hide_next") {
		return true
	}
	if msg.Role != "assistant" {
		return false
	}
	for _, tc := range msg.ToolCalls {
		if strings.HasPrefix(tc.Name, "hide_") {
			return true
		}
	}
	return false
}

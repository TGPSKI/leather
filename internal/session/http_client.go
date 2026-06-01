package session

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/tgpski/leather/internal/model"
)

// HTTPClient implements LLMClient against any OpenAI-compatible endpoint.
type HTTPClient struct {
	endpoint string
	apiKey   string // optional bearer token; empty disables auth
	timeout  time.Duration
	http     *http.Client
}

// NewHTTPClient returns an HTTPClient targeting the given base URL.
// When apiKey is non-empty it is sent on every request as
// `Authorization: Bearer <apiKey>`. The key is never logged.
func NewHTTPClient(endpoint, apiKey string, timeout time.Duration) *HTTPClient {
	return &HTTPClient{
		endpoint: strings.TrimRight(endpoint, "/"),
		apiKey:   apiKey,
		timeout:  timeout,
		http:     &http.Client{Timeout: timeout},
	}
}

// Complete sends a chat completion request to the LLM endpoint and returns
// the parsed response.
func (c *HTTPClient) Complete(ctx context.Context, modelName string, messages []model.Message, opts CompletionOptions) (model.LLMResponse, error) {
	reqBody := map[string]any{
		"model":       modelName,
		"messages":    toAPIMessages(messages),
		"temperature": opts.Temperature,
		"max_tokens":  opts.MaxTokens,
	}
	if len(opts.Tools) > 0 {
		reqBody["tools"] = toAPITools(opts.Tools)
		reqBody["tool_choice"] = "auto"
	}
	for k, v := range opts.ExtraBody {
		reqBody[k] = v
	}

	body, err := json.Marshal(reqBody)
	if err != nil {
		return model.LLMResponse{}, fmt.Errorf("http_client/Complete: marshal request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.endpoint+"/v1/chat/completions", bytes.NewReader(body))
	if err != nil {
		return model.LLMResponse{}, fmt.Errorf("http_client/Complete: build request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	if c.apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+c.apiKey)
	}

	resp, err := c.http.Do(req)
	if err != nil {
		return model.LLMResponse{}, fmt.Errorf("http_client/Complete: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		snippet, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return model.LLMResponse{}, fmt.Errorf("http_client/Complete: status %d: %s", resp.StatusCode, snippet)
	}

	var apiResp struct {
		Choices []struct {
			Message struct {
				Role      string `json:"role"`
				Content   string `json:"content"`
				ToolCalls []struct {
					ID       string `json:"id"`
					Type     string `json:"type"`
					Function struct {
						Name      string `json:"name"`
						Arguments string `json:"arguments"`
					} `json:"function"`
				} `json:"tool_calls"`
			} `json:"message"`
			FinishReason string `json:"finish_reason"`
		} `json:"choices"`
		Usage struct {
			PromptTokens     int `json:"prompt_tokens"`
			CompletionTokens int `json:"completion_tokens"`
			TotalTokens      int `json:"total_tokens"`
		} `json:"usage"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&apiResp); err != nil {
		return model.LLMResponse{}, fmt.Errorf("http_client/Complete: decode response: %w", err)
	}
	if len(apiResp.Choices) == 0 {
		return model.LLMResponse{}, fmt.Errorf("http_client/Complete: no choices in response")
	}

	choice := apiResp.Choices[0]
	var toolCalls []model.ToolCall
	for _, tc := range choice.Message.ToolCalls {
		var args map[string]any
		if tc.Function.Arguments != "" {
			if err := json.Unmarshal([]byte(tc.Function.Arguments), &args); err != nil {
				args = map[string]any{"_raw": tc.Function.Arguments}
			}
		}
		toolCalls = append(toolCalls, model.ToolCall{
			ID:        tc.ID,
			Name:      tc.Function.Name,
			Arguments: args,
		})
	}

	totalTokens := apiResp.Usage.TotalTokens
	if sum := apiResp.Usage.PromptTokens + apiResp.Usage.CompletionTokens; totalTokens < sum {
		totalTokens = sum
	}

	return model.LLMResponse{
		Content:          choice.Message.Content,
		FinishReason:     choice.FinishReason,
		PromptTokens:     apiResp.Usage.PromptTokens,
		CompletionTokens: apiResp.Usage.CompletionTokens,
		TotalTokens:      totalTokens,
		ToolCalls:        toolCalls,
	}, nil
}

// CountTokens estimates the token count for messages using a character-based
// heuristic (~4 chars/token). For precise counts, a dedicated tokenizer
// endpoint would be needed.
func (c *HTTPClient) CountTokens(messages []model.Message) (int, error) {
	return estimateTokens(messages), nil
}

// toAPIMessages converts model.Message values to the OpenAI wire format.
// It handles assistant messages with tool calls and tool-role result messages.
func toAPIMessages(msgs []model.Message) []map[string]any {
	out := make([]map[string]any, len(msgs))
	for i, m := range msgs {
		switch m.Role {
		case "tool":
			out[i] = map[string]any{
				"role":         "tool",
				"tool_call_id": m.ToolCallID,
				"name":         m.ToolName,
				"content":      m.Content,
			}
		case "assistant":
			if len(m.ToolCalls) > 0 {
				tcs := make([]map[string]any, len(m.ToolCalls))
				for j, tc := range m.ToolCalls {
					args, _ := json.Marshal(tc.Arguments)
					tcs[j] = map[string]any{
						"id":   tc.ID,
						"type": "function",
						"function": map[string]any{
							"name":      tc.Name,
							"arguments": string(args),
						},
					}
				}
				out[i] = map[string]any{
					"role":       "assistant",
					"content":    m.Content,
					"tool_calls": tcs,
				}
			} else {
				out[i] = map[string]any{"role": m.Role, "content": m.Content}
			}
		default:
			out[i] = map[string]any{"role": m.Role, "content": m.Content}
		}
	}
	return out
}
func toAPITools(tools []model.ToolDefinition) []map[string]any {
	out := make([]map[string]any, len(tools))
	for i, t := range tools {
		params := map[string]any(t.Parameters)
		if params == nil {
			params = map[string]any{
				"type":                 "object",
				"additionalProperties": true,
			}
		}
		out[i] = map[string]any{
			"type": "function",
			"function": map[string]any{
				"name":        t.Name,
				"description": t.Description,
				"parameters":  params,
			},
		}
	}
	return out
}

// estimateTokens returns a rough token count: 4 overhead + (chars+3)/4 per message.
func estimateTokens(msgs []model.Message) int {
	total := 0
	for _, m := range msgs {
		total += 4 + (len(m.Content)+3)/4
	}
	return total
}

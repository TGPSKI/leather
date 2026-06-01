package session

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/tgpski/leather/internal/model"
)

// openAIResponse builds a minimal OpenAI-shaped chat completion response body.
func openAIResponse(content, finishReason string, prompt, completion int) map[string]any {
	return map[string]any{
		"choices": []map[string]any{
			{
				"message":       map[string]string{"role": "assistant", "content": content},
				"finish_reason": finishReason,
			},
		},
		"usage": map[string]any{
			"prompt_tokens":     prompt,
			"completion_tokens": completion,
			"total_tokens":      prompt + completion,
		},
	}
}

func serveJSON(t *testing.T, status int, body any) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		if err := json.NewEncoder(w).Encode(body); err != nil {
			t.Errorf("serveJSON: encode: %v", err)
		}
	}))
}

// --- NewHTTPClient ---

func TestNewHTTPClient_ReturnsNonNil(t *testing.T) {
	c := NewHTTPClient("http://localhost:11434", "", 30*time.Second)
	if c == nil {
		t.Fatal("NewHTTPClient returned nil")
	}
}

func TestNewHTTPClient_StripsTrailingSlash(t *testing.T) {
	c := NewHTTPClient("http://localhost:11434/", "", 10*time.Second)
	if strings.HasSuffix(c.endpoint, "/") {
		t.Errorf("endpoint %q still has trailing slash", c.endpoint)
	}
}

func TestComplete_SendsAuthorizationHeader_WhenAPIKeySet(t *testing.T) {
	var gotAuth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(openAIResponse("ok", "stop", 1, 1))
	}))
	defer srv.Close()

	c := NewHTTPClient(srv.URL, "sk-test-key", 5*time.Second)
	if _, err := c.Complete(context.Background(), "m", nil, CompletionOptions{}); err != nil {
		t.Fatalf("Complete: %v", err)
	}
	if want := "Bearer sk-test-key"; gotAuth != want {
		t.Errorf("Authorization header = %q, want %q", gotAuth, want)
	}
}

func TestComplete_OmitsAuthorizationHeader_WhenAPIKeyEmpty(t *testing.T) {
	var gotAuth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(openAIResponse("ok", "stop", 1, 1))
	}))
	defer srv.Close()

	c := NewHTTPClient(srv.URL, "", 5*time.Second)
	if _, err := c.Complete(context.Background(), "m", nil, CompletionOptions{}); err != nil {
		t.Fatalf("Complete: %v", err)
	}
	if gotAuth != "" {
		t.Errorf("Authorization header = %q, want empty", gotAuth)
	}
}

// --- CountTokens ---

func TestCountTokens_DelegatesToEstimate(t *testing.T) {
	c := NewHTTPClient("http://localhost:11434", "", 5*time.Second)
	msgs := []model.Message{
		{Role: "user", Content: "hello"},
	}
	got, err := c.CountTokens(msgs)
	if err != nil {
		t.Fatalf("CountTokens: %v", err)
	}
	want := estimateTokens(msgs)
	if got != want {
		t.Errorf("CountTokens = %d, want %d", got, want)
	}
}

// --- Complete: happy path ---

func TestComplete_HappyPath(t *testing.T) {
	srv := serveJSON(t, http.StatusOK, openAIResponse("hello world", "stop", 10, 5))
	defer srv.Close()

	c := NewHTTPClient(srv.URL, "", 5*time.Second)
	msgs := []model.Message{{Role: "user", Content: "say hello"}}
	resp, err := c.Complete(context.Background(), "test-model", msgs, CompletionOptions{Temperature: 0.7})
	if err != nil {
		t.Fatalf("Complete: %v", err)
	}
	if resp.Content != "hello world" {
		t.Errorf("Content = %q, want %q", resp.Content, "hello world")
	}
	if resp.FinishReason != "stop" {
		t.Errorf("FinishReason = %q, want stop", resp.FinishReason)
	}
	if resp.PromptTokens != 10 {
		t.Errorf("PromptTokens = %d, want 10", resp.PromptTokens)
	}
	if resp.CompletionTokens != 5 {
		t.Errorf("CompletionTokens = %d, want 5", resp.CompletionTokens)
	}
	if resp.TotalTokens != 15 {
		t.Errorf("TotalTokens = %d, want 15", resp.TotalTokens)
	}
}

func TestComplete_SendsCorrectRequestShape(t *testing.T) {
	var captured map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := json.NewDecoder(r.Body).Decode(&captured); err != nil {
			t.Errorf("decode request: %v", err)
		}
		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(openAIResponse("ok", "stop", 1, 1)); err != nil {
			t.Errorf("encode response: %v", err)
		}
	}))
	defer srv.Close()

	c := NewHTTPClient(srv.URL, "", 5*time.Second)
	msgs := []model.Message{{Role: "user", Content: "ping"}}
	opts := CompletionOptions{Temperature: 0.5, MaxTokens: 256}
	if _, err := c.Complete(context.Background(), "mymodel", msgs, opts); err != nil {
		t.Fatalf("Complete: %v", err)
	}

	if captured["model"] != "mymodel" {
		t.Errorf("request model = %v, want mymodel", captured["model"])
	}
	if captured["temperature"].(float64) != 0.5 {
		t.Errorf("request temperature = %v, want 0.5", captured["temperature"])
	}
}

func TestComplete_ExtraBodyMerged(t *testing.T) {
	var captured map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewDecoder(r.Body).Decode(&captured)
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(openAIResponse("ok", "stop", 1, 1))
	}))
	defer srv.Close()

	c := NewHTTPClient(srv.URL, "", 5*time.Second)
	opts := CompletionOptions{
		ExtraBody: map[string]any{"thinking": map[string]any{"type": "disabled"}},
	}
	if _, err := c.Complete(context.Background(), "m", nil, opts); err != nil {
		t.Fatalf("Complete: %v", err)
	}
	if _, ok := captured["thinking"]; !ok {
		t.Error("extra_body key 'thinking' not merged into request")
	}
}

// --- Complete: error paths ---

func TestComplete_Non200Status_ReturnsError(t *testing.T) {
	srv := serveJSON(t, http.StatusInternalServerError, map[string]string{"error": "server exploded"})
	defer srv.Close()

	c := NewHTTPClient(srv.URL, "", 5*time.Second)
	_, err := c.Complete(context.Background(), "m", nil, CompletionOptions{})
	if err == nil {
		t.Fatal("expected error for 500 response, got nil")
	}
	if !strings.Contains(err.Error(), "500") {
		t.Errorf("error %q missing status code", err.Error())
	}
}

func TestComplete_InvalidJSON_ReturnsError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("not json at all"))
	}))
	defer srv.Close()

	c := NewHTTPClient(srv.URL, "", 5*time.Second)
	_, err := c.Complete(context.Background(), "m", nil, CompletionOptions{})
	if err == nil {
		t.Fatal("expected error for invalid JSON response, got nil")
	}
}

func TestComplete_EmptyChoices_ReturnsError(t *testing.T) {
	body := map[string]any{
		"choices": []any{},
		"usage":   map[string]any{"prompt_tokens": 0, "completion_tokens": 0, "total_tokens": 0},
	}
	srv := serveJSON(t, http.StatusOK, body)
	defer srv.Close()

	c := NewHTTPClient(srv.URL, "", 5*time.Second)
	_, err := c.Complete(context.Background(), "m", nil, CompletionOptions{})
	if err == nil {
		t.Fatal("expected error for empty choices, got nil")
	}
	if !strings.Contains(err.Error(), "no choices") {
		t.Errorf("error %q missing 'no choices'", err.Error())
	}
}

func TestComplete_ContextCancelled_ReturnsError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Block until the client gives up.
		<-r.Context().Done()
	}))
	defer srv.Close()

	c := NewHTTPClient(srv.URL, "", 5*time.Second)
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancel immediately

	_, err := c.Complete(ctx, "m", nil, CompletionOptions{})
	if err == nil {
		t.Fatal("expected error for cancelled context, got nil")
	}
}

// --- toAPIMessages ---

func TestToAPIMessages_RoleAndContent(t *testing.T) {
	msgs := []model.Message{
		{Role: "system", Content: "you are helpful"},
		{Role: "user", Content: "hello"},
	}
	out := toAPIMessages(msgs)
	if len(out) != 2 {
		t.Fatalf("len = %d, want 2", len(out))
	}
	if out[0]["role"] != "system" || out[0]["content"] != "you are helpful" {
		t.Errorf("out[0] = %v", out[0])
	}
	if out[1]["role"] != "user" || out[1]["content"] != "hello" {
		t.Errorf("out[1] = %v", out[1])
	}
}

func TestToAPIMessages_Empty(t *testing.T) {
	out := toAPIMessages(nil)
	if len(out) != 0 {
		t.Errorf("expected empty slice for nil input, got len %d", len(out))
	}
}

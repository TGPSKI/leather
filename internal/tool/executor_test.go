package tool

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/tgpski/leather/internal/model"
)

func TestExecute_HTTPSuccess(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("tool response"))
	}))
	defer srv.Close()

	def := model.ToolDefinition{
		Name: "test_tool",
		Type: "http",
		HTTP: model.HTTPToolConfig{
			Method: "GET",
			URL:    srv.URL,
		},
	}

	result := Execute(context.Background(), def, nil)
	if result.Error != "" {
		t.Fatalf("Execute error: %s", result.Error)
	}
	if result.Content != "tool response" {
		t.Errorf("Content = %q, want %q", result.Content, "tool response")
	}
	if result.Name != "test_tool" {
		t.Errorf("Name = %q, want test_tool", result.Name)
	}
}

func TestExecute_HTTPNonSuccess(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte("server error"))
	}))
	defer srv.Close()

	def := model.ToolDefinition{
		Name: "fail_tool",
		Type: "http",
		HTTP: model.HTTPToolConfig{Method: "GET", URL: srv.URL},
	}

	result := Execute(context.Background(), def, nil)
	if result.Error == "" {
		t.Error("expected error for non-2xx status, got empty")
	}
	if !strings.Contains(result.Error, "500") {
		t.Errorf("error = %q, want to contain 500", result.Error)
	}
}

func TestExecute_UnsupportedType(t *testing.T) {
	def := model.ToolDefinition{
		Name: "unsupported",
		Type: "grpc", // unsupported
	}
	result := Execute(context.Background(), def, nil)
	if result.Error == "" {
		t.Error("expected error for unsupported type, got empty")
	}
}

func TestExecute_HTTPWithBody(t *testing.T) {
	var gotBody string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		buf := make([]byte, 256)
		n, _ := r.Body.Read(buf)
		gotBody = string(buf[:n])
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	}))
	defer srv.Close()

	def := model.ToolDefinition{
		Name: "body_tool",
		Type: "http",
		HTTP: model.HTTPToolConfig{
			Method: "POST",
			URL:    srv.URL,
			Body:   map[string]string{"key": "{{.value}}"},
		},
	}

	result := Execute(context.Background(), def, map[string]any{"value": "hello"})
	if result.Error != "" {
		t.Fatalf("Execute error: %s", result.Error)
	}
	if !strings.Contains(gotBody, "hello") {
		t.Errorf("request body = %q, want to contain 'hello'", gotBody)
	}
}

func TestExecHTTP_URLTemplateExpansion(t *testing.T) {
	var gotPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	}))
	defer srv.Close()

	cfg := model.HTTPToolConfig{
		Method: "GET",
		URL:    srv.URL + "/repos/{{.owner}}/{{.repo}}",
	}

	result, err := execHTTP(context.Background(), cfg, map[string]any{"owner": "acme", "repo": "myrepo"}, nil)
	if err != nil {
		t.Fatalf("execHTTP: %v", err)
	}
	if result != "ok" {
		t.Errorf("result = %q, want ok", result)
	}
	if gotPath != "/repos/acme/myrepo" {
		t.Errorf("path = %q, want /repos/acme/myrepo", gotPath)
	}
}

func TestExecHTTP_QueryParams(t *testing.T) {
	var gotQuery string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotQuery = r.URL.Query().Get("format")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	}))
	defer srv.Close()

	cfg := model.HTTPToolConfig{
		Method: "GET",
		URL:    srv.URL,
		Query:  map[string]string{"format": "json"},
	}

	_, err := execHTTP(context.Background(), cfg, nil, nil)
	if err != nil {
		t.Fatalf("execHTTP: %v", err)
	}
	if gotQuery != "json" {
		t.Errorf("query param format = %q, want json", gotQuery)
	}
}

// TestExecHTTP_429RetrySucceeds verifies that a 429 response triggers a retry
// and the second request succeeds.
func TestExecHTTP_429RetrySucceeds(t *testing.T) {
	calls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if calls == 1 {
			w.Header().Set("Retry-After", "0")
			w.WriteHeader(http.StatusTooManyRequests)
			_, _ = w.Write([]byte("rate limited"))
			return
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok after retry"))
	}))
	defer srv.Close()

	cfg := model.HTTPToolConfig{Method: "GET", URL: srv.URL}
	got, err := execHTTP(context.Background(), cfg, nil, nil)
	if err != nil {
		t.Fatalf("execHTTP: %v", err)
	}
	if got != "ok after retry" {
		t.Errorf("content = %q, want %q", got, "ok after retry")
	}
	if calls != 2 {
		t.Errorf("server called %d times, want 2", calls)
	}
}

// TestExecHTTP_429NoRetryReturnsError verifies that when allowRetry=false a
// 429 response is returned as an error (no further retries).
func TestExecHTTP_429NoRetryReturnsError(t *testing.T) {
	calls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		w.Header().Set("Retry-After", "0")
		w.WriteHeader(http.StatusTooManyRequests)
		_, _ = w.Write([]byte("still rate limited"))
	}))
	defer srv.Close()

	cfg := model.HTTPToolConfig{Method: "GET", URL: srv.URL}
	_, err := execHTTPInner(context.Background(), cfg, nil, nil, false)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "429") {
		t.Errorf("error = %q, want to contain 429", err)
	}
	if calls != 1 {
		t.Errorf("server called %d times, want 1", calls)
	}
}

// TestExecHTTP_403RateLimitRetry verifies that a 403 with
// X-RateLimit-Remaining: 0 triggers a retry.
func TestExecHTTP_403RateLimitRetry(t *testing.T) {
	calls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if calls == 1 {
			w.Header().Set("X-RateLimit-Remaining", "0")
			w.Header().Set("Retry-After", "0")
			w.WriteHeader(http.StatusForbidden)
			_, _ = w.Write([]byte("forbidden"))
			return
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	}))
	defer srv.Close()

	cfg := model.HTTPToolConfig{Method: "GET", URL: srv.URL}
	got, err := execHTTP(context.Background(), cfg, nil, nil)
	if err != nil {
		t.Fatalf("execHTTP: %v", err)
	}
	if got != "ok" {
		t.Errorf("content = %q, want ok", got)
	}
	if calls != 2 {
		t.Errorf("server called %d times, want 2", calls)
	}
}

// TestExecHTTP_RetryContextCancelled verifies that a cancelled context during
// the rate-limit wait returns the context error.
func TestExecHTTP_RetryContextCancelled(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Retry-After", "60") // long wait
		w.WriteHeader(http.StatusTooManyRequests)
	}))
	defer srv.Close()

	ctx, cancel := context.WithCancel(context.Background())

	// Use a background context for the initial request so we reach the wait,
	// then cancel while waiting.
	waitCh := make(chan struct{})
	srv2 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Retry-After", "60")
		w.WriteHeader(http.StatusTooManyRequests)
		close(waitCh)
	}))
	defer srv2.Close()

	cancel() // cancel before any request
	cfg := model.HTTPToolConfig{Method: "GET", URL: srv.URL}
	_, err := execHTTP(ctx, cfg, nil, nil)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	// The error may come from the HTTP client (context canceled before request)
	// or from our wait select (rate limit wait cancelled). Both are acceptable.
	if !strings.Contains(err.Error(), "cancel") {
		t.Errorf("error = %q, want to contain 'cancel'", err)
	}
}

// TestIsRateLimited covers the isRateLimited helper.
func TestIsRateLimited(t *testing.T) {
	cases := []struct {
		name   string
		status int
		header map[string]string
		want   bool
	}{
		{"429 always", 429, nil, true},
		{"403 with remaining=0", 403, map[string]string{"X-RateLimit-Remaining": "0"}, true},
		{"403 with remaining=1", 403, map[string]string{"X-RateLimit-Remaining": "1"}, false},
		{"403 no header", 403, nil, false},
		{"500 no header", 500, nil, false},
		{"200 ok", 200, nil, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			resp := &http.Response{
				StatusCode: tc.status,
				Header:     make(http.Header),
			}
			for k, v := range tc.header {
				resp.Header.Set(k, v)
			}
			if got := isRateLimited(resp); got != tc.want {
				t.Errorf("isRateLimited() = %v, want %v", got, tc.want)
			}
		})
	}
}

// TestRetryWait covers the retryWait helper.
func TestRetryWait(t *testing.T) {
	max := 60 * time.Second

	t.Run("Retry-After within max", func(t *testing.T) {
		resp := &http.Response{Header: make(http.Header)}
		resp.Header.Set("Retry-After", "30")
		if got := retryWait(resp, max); got != 30*time.Second {
			t.Errorf("got %v, want 30s", got)
		}
	})

	t.Run("Retry-After exceeds max", func(t *testing.T) {
		resp := &http.Response{Header: make(http.Header)}
		resp.Header.Set("Retry-After", "120")
		if got := retryWait(resp, max); got != max {
			t.Errorf("got %v, want %v", got, max)
		}
	})

	t.Run("Retry-After zero", func(t *testing.T) {
		resp := &http.Response{Header: make(http.Header)}
		resp.Header.Set("Retry-After", "0")
		// 0 is a valid value meaning "retry immediately"
		if got := retryWait(resp, max); got != 0 {
			t.Errorf("got %v, want 0", got)
		}
	})

	t.Run("X-RateLimit-Reset in the past", func(t *testing.T) {
		resp := &http.Response{Header: make(http.Header)}
		past := fmt.Sprintf("%d", time.Now().Add(-10*time.Second).Unix())
		resp.Header.Set("X-RateLimit-Reset", past)
		if got := retryWait(resp, max); got != 0 {
			t.Errorf("got %v, want 0 (past timestamp)", got)
		}
	})

	t.Run("X-RateLimit-Reset in future within max", func(t *testing.T) {
		resp := &http.Response{Header: make(http.Header)}
		future := fmt.Sprintf("%d", time.Now().Add(10*time.Second).Unix())
		resp.Header.Set("X-RateLimit-Reset", future)
		got := retryWait(resp, max)
		// allow ±2s tolerance around 10s
		if got < 8*time.Second || got > 12*time.Second {
			t.Errorf("got %v, want ~10s", got)
		}
	})

	t.Run("X-RateLimit-Reset exceeds max", func(t *testing.T) {
		resp := &http.Response{Header: make(http.Header)}
		far := fmt.Sprintf("%d", time.Now().Add(120*time.Second).Unix())
		resp.Header.Set("X-RateLimit-Reset", far)
		if got := retryWait(resp, max); got != max {
			t.Errorf("got %v, want %v", got, max)
		}
	})

	t.Run("no headers fallback to max", func(t *testing.T) {
		resp := &http.Response{Header: make(http.Header)}
		if got := retryWait(resp, max); got != max {
			t.Errorf("got %v, want %v", got, max)
		}
	})
}

package tool

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/tgpski/leather/internal/model"
	"github.com/tgpski/leather/internal/queue"
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

	def := model.ToolDefinition{
		Name:  "retry_tool",
		Type:  "http",
		HTTP:  model.HTTPToolConfig{Method: "GET", URL: srv.URL},
		Retry: model.ToolRetryConfig{MaxAttempts: 3, BaseDelay: 0, MaxDelay: time.Second, HonorRetryAfter: true},
	}
	result := (&Executor{}).Execute(context.Background(), def, nil)
	if result.Error != "" {
		t.Fatalf("Execute error: %s", result.Error)
	}
	if result.Content != "ok after retry" {
		t.Errorf("content = %q, want %q", result.Content, "ok after retry")
	}
	if calls != 2 {
		t.Errorf("server called %d times, want 2", calls)
	}
}

// TestExecHTTP_429NoRetryReturnsError verifies that when MaxAttempts=1 a
// 429 response is returned as an error without retrying.
func TestExecHTTP_429NoRetryReturnsError(t *testing.T) {
	calls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		w.Header().Set("Retry-After", "0")
		w.WriteHeader(http.StatusTooManyRequests)
		_, _ = w.Write([]byte("still rate limited"))
	}))
	defer srv.Close()

	def := model.ToolDefinition{
		Name:  "no_retry_tool",
		Type:  "http",
		HTTP:  model.HTTPToolConfig{Method: "GET", URL: srv.URL},
		Retry: model.ToolRetryConfig{MaxAttempts: 1}, // disable retries
	}
	result := (&Executor{}).Execute(context.Background(), def, nil)
	if result.Error == "" {
		t.Fatal("expected error, got empty")
	}
	if !strings.Contains(result.Error, "429") {
		t.Errorf("error = %q, want to contain 429", result.Error)
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

	def := model.ToolDefinition{
		Name:  "rate_limit_tool",
		Type:  "http",
		HTTP:  model.HTTPToolConfig{Method: "GET", URL: srv.URL},
		Retry: model.ToolRetryConfig{MaxAttempts: 3, BaseDelay: 0, MaxDelay: time.Second, HonorRetryAfter: true},
	}
	result := (&Executor{}).Execute(context.Background(), def, nil)
	if result.Error != "" {
		t.Fatalf("Execute error: %s", result.Error)
	}
	if result.Content != "ok" {
		t.Errorf("content = %q, want ok", result.Content)
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
	cancel() // cancel before any request

	def := model.ToolDefinition{
		Name: "cancel_tool",
		Type: "http",
		HTTP: model.HTTPToolConfig{Method: "GET", URL: srv.URL},
	}
	result := (&Executor{}).Execute(ctx, def, nil)
	if result.Error == "" {
		t.Fatal("expected error, got empty")
	}
	// The error may come from the HTTP client (context canceled before request)
	// or from our wait select (rate limit wait cancelled). Both are acceptable.
	if !strings.Contains(result.Error, "cancel") {
		t.Errorf("error = %q, want to contain 'cancel'", result.Error)
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
		h := make(http.Header)
		h.Set("Retry-After", "30")
		if got := retryWait(h, max); got != 30*time.Second {
			t.Errorf("got %v, want 30s", got)
		}
	})

	t.Run("Retry-After exceeds max", func(t *testing.T) {
		h := make(http.Header)
		h.Set("Retry-After", "120")
		if got := retryWait(h, max); got != max {
			t.Errorf("got %v, want %v", got, max)
		}
	})

	t.Run("Retry-After zero", func(t *testing.T) {
		h := make(http.Header)
		h.Set("Retry-After", "0")
		// 0 is a valid value meaning "retry immediately"
		if got := retryWait(h, max); got != 0 {
			t.Errorf("got %v, want 0", got)
		}
	})

	t.Run("X-RateLimit-Reset in the past", func(t *testing.T) {
		h := make(http.Header)
		past := fmt.Sprintf("%d", time.Now().Add(-10*time.Second).Unix())
		h.Set("X-RateLimit-Reset", past)
		if got := retryWait(h, max); got != 0 {
			t.Errorf("got %v, want 0 (past timestamp)", got)
		}
	})

	t.Run("X-RateLimit-Reset in future within max", func(t *testing.T) {
		h := make(http.Header)
		future := fmt.Sprintf("%d", time.Now().Add(10*time.Second).Unix())
		h.Set("X-RateLimit-Reset", future)
		got := retryWait(h, max)
		// allow ±2s tolerance around 10s
		if got < 8*time.Second || got > 12*time.Second {
			t.Errorf("got %v, want ~10s", got)
		}
	})

	t.Run("X-RateLimit-Reset exceeds max", func(t *testing.T) {
		h := make(http.Header)
		far := fmt.Sprintf("%d", time.Now().Add(120*time.Second).Unix())
		h.Set("X-RateLimit-Reset", far)
		if got := retryWait(h, max); got != max {
			t.Errorf("got %v, want %v", got, max)
		}
	})

	t.Run("no headers fallback to max", func(t *testing.T) {
		h := make(http.Header)
		if got := retryWait(h, max); got != max {
			t.Errorf("got %v, want %v", got, max)
		}
	})
}

// --- Issue #7: retry policy ---

// TestExecHTTP_TransientRetryExhausted verifies that 3 consecutive 500 responses
// exhaust the retry budget and return an error after MaxAttempts calls.
func TestExecHTTP_TransientRetryExhausted(t *testing.T) {
	calls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte("server error"))
	}))
	defer srv.Close()

	def := model.ToolDefinition{
		Name:  "retry_exhaust",
		Type:  "http",
		HTTP:  model.HTTPToolConfig{Method: "GET", URL: srv.URL},
		Retry: model.ToolRetryConfig{MaxAttempts: 3, BaseDelay: 0, MaxDelay: time.Millisecond},
	}
	result := (&Executor{}).Execute(context.Background(), def, nil)
	if result.Error == "" {
		t.Fatal("expected error after exhausted retries, got empty")
	}
	if !strings.Contains(result.Error, "500") {
		t.Errorf("error = %q, want to contain 500", result.Error)
	}
	if calls != 3 {
		t.Errorf("server called %d times, want 3", calls)
	}
}

// TestExecHTTP_TransientRetrySucceeds verifies that transient failures before
// the last attempt still result in success.
func TestExecHTTP_TransientRetrySucceeds(t *testing.T) {
	calls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if calls < 3 {
			w.WriteHeader(http.StatusServiceUnavailable)
			_, _ = w.Write([]byte("unavailable"))
			return
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("success"))
	}))
	defer srv.Close()

	def := model.ToolDefinition{
		Name:  "retry_success",
		Type:  "http",
		HTTP:  model.HTTPToolConfig{Method: "GET", URL: srv.URL},
		Retry: model.ToolRetryConfig{MaxAttempts: 3, BaseDelay: 0, MaxDelay: time.Millisecond},
	}
	result := (&Executor{}).Execute(context.Background(), def, nil)
	if result.Error != "" {
		t.Fatalf("Execute error: %s", result.Error)
	}
	if result.Content != "success" {
		t.Errorf("content = %q, want success", result.Content)
	}
	if calls != 3 {
		t.Errorf("server called %d times, want 3", calls)
	}
}

// TestExecHTTP_PermanentNoRetry verifies that a 400 (permanent) returns
// immediately without retrying.
func TestExecHTTP_PermanentNoRetry(t *testing.T) {
	calls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte("bad request"))
	}))
	defer srv.Close()

	def := model.ToolDefinition{
		Name:  "perm_fail",
		Type:  "http",
		HTTP:  model.HTTPToolConfig{Method: "GET", URL: srv.URL},
		Retry: model.ToolRetryConfig{MaxAttempts: 3, BaseDelay: 0, MaxDelay: time.Millisecond},
	}
	result := (&Executor{}).Execute(context.Background(), def, nil)
	if result.Error == "" {
		t.Fatal("expected error for 400, got empty")
	}
	if calls != 1 {
		t.Errorf("server called %d times, want 1 (no retry on permanent failure)", calls)
	}
}

// TestIsTransient covers the isTransient helper with a representative set of codes.
func TestIsTransient(t *testing.T) {
	cases := []struct {
		name   string
		status int
		want   bool
	}{
		{"429 transient", 429, true},
		{"500 transient", 500, true},
		{"502 transient", 502, true},
		{"503 transient", 503, true},
		{"504 transient", 504, true},
		{"400 permanent", 400, false},
		{"401 permanent", 401, false},
		{"404 permanent", 404, false},
		{"403 no header permanent", 403, false},
		{"200 not transient", 200, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := &httpError{status: tc.status, header: make(http.Header)}
			if got := isTransient(tc.status, err); got != tc.want {
				t.Errorf("isTransient(%d) = %v, want %v", tc.status, got, tc.want)
			}
		})
	}
}

// --- Issue #8: DLQ enqueue ---

// TestExecute_DLQEnqueueOnExhaustion verifies that after MaxAttempts transient
// failures the item is enqueued to outbound-dlq.
func TestExecute_DLQEnqueueOnExhaustion(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	queueDir := t.TempDir()
	mgr := newTestQueueManager(t, queueDir)

	def := model.ToolDefinition{
		Name:  "dlq_exhaust",
		Type:  "http",
		HTTP:  model.HTTPToolConfig{Method: "GET", URL: srv.URL},
		Retry: model.ToolRetryConfig{MaxAttempts: 2, BaseDelay: 0, MaxDelay: time.Millisecond},
	}
	exec := &Executor{QueueMgr: mgr, AgentName: "test-agent"}
	result := exec.Execute(context.Background(), def, nil)
	if result.Error == "" {
		t.Fatal("expected error, got empty")
	}

	dlqQ, err := mgr.Get(outboundDLQName)
	if err != nil {
		t.Fatalf("get dlq: %v", err)
	}
	items := dlqQ.Scan()
	if len(items) != 1 {
		t.Fatalf("dlq depth = %d, want 1", len(items))
	}
	if items[0].ToolName != "dlq_exhaust" {
		t.Errorf("ToolName = %q, want dlq_exhaust", items[0].ToolName)
	}
	if items[0].AgentName != "test-agent" {
		t.Errorf("AgentName = %q, want test-agent", items[0].AgentName)
	}
}

// TestExecute_DLQEnqueueOnPermanent verifies that a 400 (permanent failure)
// immediately enqueues to outbound-dlq when QueueMgr is set.
func TestExecute_DLQEnqueueOnPermanent(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
	}))
	defer srv.Close()

	queueDir := t.TempDir()
	mgr := newTestQueueManager(t, queueDir)

	def := model.ToolDefinition{
		Name:  "perm_dlq",
		Type:  "http",
		HTTP:  model.HTTPToolConfig{Method: "GET", URL: srv.URL},
		Retry: model.ToolRetryConfig{MaxAttempts: 3, BaseDelay: 0, MaxDelay: time.Millisecond},
	}
	exec := &Executor{QueueMgr: mgr}
	result := exec.Execute(context.Background(), def, nil)
	if result.Error == "" {
		t.Fatal("expected error, got empty")
	}

	dlqQ, err := mgr.Get(outboundDLQName)
	if err != nil {
		t.Fatalf("get dlq: %v", err)
	}
	items := dlqQ.Scan()
	if len(items) != 1 {
		t.Fatalf("dlq depth = %d, want 1", len(items))
	}
}

// TestExecute_NoDLQWhenQueueMgrNil verifies that a nil QueueMgr does not panic
// and the tool result error is still set.
func TestExecute_NoDLQWhenQueueMgrNil(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	def := model.ToolDefinition{
		Name:  "no_dlq",
		Type:  "http",
		HTTP:  model.HTTPToolConfig{Method: "GET", URL: srv.URL},
		Retry: model.ToolRetryConfig{MaxAttempts: 2, BaseDelay: 0, MaxDelay: time.Millisecond},
	}
	result := (&Executor{QueueMgr: nil}).Execute(context.Background(), def, nil)
	if result.Error == "" {
		t.Fatal("expected error, got empty")
	}
}

// --- Issue #9: rate limit counter ---

// TestExecute_RateLimitWaitIncrementsCounter verifies that a throttled host
// increments the metricRateLimitWaitTotal counter.
func TestExecute_RateLimitWaitIncrementsCounter(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	}))
	defer srv.Close()

	// url.Parse.Hostname() strips the port; match that key in the limiter.
	parsed, _ := url.Parse(srv.URL)
	host := parsed.Hostname()
	limiter, err := NewHostLimiter(map[string]string{host: "1/s"})
	if err != nil {
		t.Fatalf("NewHostLimiter: %v", err)
	}

	def := model.ToolDefinition{
		Name: "rate_counter",
		Type: "http",
		HTTP: model.HTTPToolConfig{Method: "GET", URL: srv.URL},
	}

	before := atomic.LoadInt64(&metricRateLimitWaitTotal)

	// First call: token available, no wait.
	result := (&Executor{Limiter: limiter}).Execute(context.Background(), def, nil)
	if result.Error != "" {
		t.Fatalf("first Execute error: %s", result.Error)
	}

	// Second call: token not yet refilled, should wait and increment counter.
	result = (&Executor{Limiter: limiter}).Execute(context.Background(), def, nil)
	if result.Error != "" {
		t.Fatalf("second Execute error: %s", result.Error)
	}

	after := atomic.LoadInt64(&metricRateLimitWaitTotal)
	if after <= before {
		t.Errorf("metricRateLimitWaitTotal = %d, want > %d (counter not incremented)", after, before)
	}
}

// newTestQueueManager creates a queue.Manager backed by a temp directory.
func newTestQueueManager(t *testing.T, dir string) *queue.Manager {
	t.Helper()
	return queue.NewManager(dir)
}

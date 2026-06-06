package tool

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/rand"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/tgpski/leather/internal/ids"
	"github.com/tgpski/leather/internal/mcp"
	"github.com/tgpski/leather/internal/model"
	"github.com/tgpski/leather/internal/queue"
)

// toolClient is a shared HTTP client with a conservative timeout.
// Using a package-level client allows connection reuse while preventing
// tool calls from blocking indefinitely on unresponsive endpoints.
var toolClient = &http.Client{Timeout: 30 * time.Second}

// Package-level atomic metrics counters.
var (
	metricRetryTotal         int64 // incremented on each retry attempt
	metricBackoffTotal       int64 // incremented when a retry-after sleep occurs
	metricRateLimitWaitTotal int64 // incremented when HostLimiter.Wait blocks
)

// MetricSnapshot returns a point-in-time copy of the outbound tool counters.
func MetricSnapshot() (retryTotal, backoffTotal, rateLimitWaitTotal int64) {
	return atomic.LoadInt64(&metricRetryTotal),
		atomic.LoadInt64(&metricBackoffTotal),
		atomic.LoadInt64(&metricRateLimitWaitTotal)
}

const (
	outboundDLQName    = "outbound-dlq"
	defaultMaxAttempts = 3
	defaultBaseDelay   = 1 * time.Second
	defaultMaxDelay    = 30 * time.Second
)

// Executor dispatches tool calls to the appropriate backend.
// The zero value (all fields nil) handles http-type tools only.
type Executor struct {
	// MCP is the registry of running MCP server clients.
	// Nil means mcp-type tools are unavailable.
	MCP *mcp.Registry
	// QueueMgr, when non-nil, enables the outbound DLQ: permanent failures and
	// exhausted-retry failures are enqueued to the "outbound-dlq" queue.
	QueueMgr *queue.Manager
	// AgentName is propagated into outbound-DLQ items for traceability.
	AgentName string
	// Limiter, when non-nil, applies per-host token-bucket throttling before
	// every outbound HTTP or MCP-backed call (including retries).
	Limiter *HostLimiter
}

// Execute dispatches a ToolCall to the appropriate executor based on def.Type.
// Authentication header values are never logged.
func (e *Executor) Execute(ctx context.Context, def model.ToolDefinition, args map[string]any) model.ToolResult {
	result := model.ToolResult{Name: def.Name}
	var content string
	var execErr error

	retrycfg := resolvedRetry(def.Retry)

	switch def.Type {
	case "http", "":
		content, execErr = e.execHTTPWithRetry(ctx, def, args, retrycfg)
	case "mcp":
		if e.MCP == nil {
			execErr = fmt.Errorf("mcp tool %q: no MCP registry configured", def.Name)
		} else {
			content, execErr = e.execMCPWithRetry(ctx, def, args, retrycfg)
		}
	default:
		execErr = fmt.Errorf("unsupported tool type %q", def.Type)
	}

	if execErr != nil {
		result.Error = execErr.Error()
	} else {
		result.Content = content
		if def.OutputFile != "" {
			// Reject absolute paths and paths with ".." components to prevent
			// tool definitions from writing outside the working directory.
			clean := filepath.Clean(def.OutputFile)
			if filepath.IsAbs(clean) || strings.HasPrefix(clean, ".."+string(filepath.Separator)) || clean == ".." {
				result.Error = fmt.Sprintf("tool/execute: OutputFile %q is not a safe relative path", def.OutputFile)
			} else {
				// Best-effort write; do not fail the tool call on I/O errors.
				_ = os.WriteFile(clean, []byte(content), 0600)
			}
		}
	}
	return result
}

// Execute dispatches a ToolCall without MCP support.
// Kept for backward compatibility; use Executor.Execute when MCP tools are needed.
func Execute(ctx context.Context, def model.ToolDefinition, args map[string]any) model.ToolResult {
	return (&Executor{}).Execute(ctx, def, args)
}

// resolvedRetry returns a ToolRetryConfig ready for use.
// When MaxAttempts is 0 (zero value / not configured), the retry policy is
// disabled and the legacy single-attempt path is used. Backoff fields default
// when MaxAttempts > 0 and the fields are not explicitly set.
func resolvedRetry(r model.ToolRetryConfig) model.ToolRetryConfig {
	if r.MaxAttempts == 0 {
		// Not configured: single attempt, no retry. Legacy behaviour.
		return model.ToolRetryConfig{MaxAttempts: 1}
	}
	if r.BaseDelay == 0 {
		r.BaseDelay = defaultBaseDelay
	}
	if r.MaxDelay == 0 {
		r.MaxDelay = defaultMaxDelay
	}
	// HonorRetryAfter defaults to true when not set but MaxAttempts is configured.
	if !r.HonorRetryAfter {
		r.HonorRetryAfter = true
	}
	return r
}

// execHTTPWithRetry wraps execHTTPInner with the configured retry policy.
func (e *Executor) execHTTPWithRetry(ctx context.Context, def model.ToolDefinition, args map[string]any, retrycfg model.ToolRetryConfig) (string, error) {
	cfg := def.HTTP
	allowedEnv := def.AllowedEnv

	// Extract host for rate limiting.
	rawURL, err := expandTemplate(cfg.URL, args, allowedEnv)
	if err != nil {
		return "", fmt.Errorf("tool/execHTTP: expand url: %w", err)
	}
	host := ""
	if u, parseErr := url.Parse(rawURL); parseErr == nil {
		host = u.Hostname()
	}

	maxAttempts := retrycfg.MaxAttempts
	if maxAttempts < 1 {
		maxAttempts = 1
	}

	var lastErr error
	for attempt := 1; attempt <= maxAttempts; attempt++ {
		// Apply per-host rate limiting before the attempt.
		if e.Limiter != nil {
			waited, waitErr := e.Limiter.Wait(ctx, host)
			if waitErr != nil {
				return "", fmt.Errorf("tool/execHTTP: rate limit wait: %w", waitErr)
			}
			if waited {
				atomic.AddInt64(&metricRateLimitWaitTotal, 1)
			}
		}

		var content string
		content, lastErr = execHTTPInner(ctx, cfg, args, allowedEnv)
		if lastErr == nil {
			return content, nil
		}

		// Check if the error is transient and we have retries left.
		if attempt >= maxAttempts {
			break
		}

		statusCode := httpStatusFromErr(lastErr)
		if !isTransient(statusCode, lastErr) {
			// Permanent failure — don't retry.
			break
		}

		atomic.AddInt64(&metricRetryTotal, 1)

		// Compute backoff delay.
		delay := backoffDelay(attempt, retrycfg, lastErr)
		if delay > 0 {
			atomic.AddInt64(&metricBackoffTotal, 1)
			select {
			case <-time.After(delay):
			case <-ctx.Done():
				return "", fmt.Errorf("tool/execHTTP: retry wait cancelled: %w", ctx.Err())
			}
		}
	}

	// Enqueue to outbound-DLQ on failure (permanent or exhausted).
	e.enqueueDLQ(def, args, host, lastErr, maxAttempts)
	return "", lastErr
}

// execMCPWithRetry wraps execMCP with the configured retry policy.
func (e *Executor) execMCPWithRetry(ctx context.Context, def model.ToolDefinition, args map[string]any, retrycfg model.ToolRetryConfig) (string, error) {
	target := def.MCP.Server + "/" + def.MCP.Tool

	maxAttempts := retrycfg.MaxAttempts
	if maxAttempts < 1 {
		maxAttempts = 1
	}

	var lastErr error
	for attempt := 1; attempt <= maxAttempts; attempt++ {
		// Apply per-host rate limiting (keyed by MCP server name).
		if e.Limiter != nil {
			waited, waitErr := e.Limiter.Wait(ctx, def.MCP.Server)
			if waitErr != nil {
				return "", fmt.Errorf("tool/execMCP: rate limit wait: %w", waitErr)
			}
			if waited {
				atomic.AddInt64(&metricRateLimitWaitTotal, 1)
			}
		}

		var content string
		content, lastErr = execMCP(ctx, e.MCP, def.MCP, args)
		if lastErr == nil {
			return content, nil
		}

		if attempt >= maxAttempts {
			break
		}

		// MCP errors are treated as transient unless they indicate a missing server.
		if strings.Contains(lastErr.Error(), "not found in MCP registry") {
			break // permanent: server not configured
		}

		atomic.AddInt64(&metricRetryTotal, 1)

		delay := backoffDelay(attempt, retrycfg, lastErr)
		if delay > 0 {
			atomic.AddInt64(&metricBackoffTotal, 1)
			select {
			case <-time.After(delay):
			case <-ctx.Done():
				return "", fmt.Errorf("tool/execMCP: retry wait cancelled: %w", ctx.Err())
			}
		}
	}

	e.enqueueDLQ(def, args, target, lastErr, maxAttempts)
	return "", lastErr
}

// enqueueDLQ enqueues a failed tool call to the outbound-DLQ when QueueMgr is set.
// This is a best-effort side-effect; it does not affect the ToolResult.
func (e *Executor) enqueueDLQ(def model.ToolDefinition, args map[string]any, target string, lastErr error, attempts int) {
	if e.QueueMgr == nil {
		return
	}
	errStr := ""
	if lastErr != nil {
		errStr = lastErr.Error()
	}
	item := model.QueueItem{
		ID:         ids.TimestampHex("odlq"),
		AgentName:  e.AgentName,
		ToolName:   def.Name,
		ToolTarget: target,
		EnqueuedAt: time.Now().Unix(),
		Payload: map[string]any{
			"tool":    def.Name,
			"target":  target,
			"agent":   e.AgentName,
			"error":   errStr,
			"attempt": attempts,
			"args":    args,
		},
	}
	if enqErr := e.QueueMgr.Enqueue(outboundDLQName, item); enqErr != nil {
		// Non-fatal: DLQ enqueue failure is logged by the caller if needed.
		_ = enqErr
	}
}

// execMCP calls a named tool on a running MCP server and returns the text result.
func execMCP(ctx context.Context, reg *mcp.Registry, cfg model.MCPToolConfig, args map[string]any) (string, error) {
	client, ok := reg.Get(cfg.Server)
	if !ok {
		return "", fmt.Errorf("tool/execMCP: server %q not found in MCP registry", cfg.Server)
	}
	result, err := client.Call(ctx, cfg.Tool, args)
	if err != nil {
		return "", fmt.Errorf("tool/execMCP: %w", err)
	}
	return result, nil
}

// execHTTP performs a single HTTP tool call with no retry logic.
// Callers that want retry should use execHTTPWithRetry via the Executor.
func execHTTP(ctx context.Context, cfg model.HTTPToolConfig, args map[string]any, allowedEnv []string) (string, error) {
	return execHTTPInner(ctx, cfg, args, allowedEnv)
}

// execHTTPInner is the single-attempt HTTP implementation.
func execHTTPInner(ctx context.Context, cfg model.HTTPToolConfig, args map[string]any, allowedEnv []string) (string, error) {
	// Expand the URL template.
	rawURL, err := expandTemplate(cfg.URL, args, allowedEnv)
	if err != nil {
		return "", fmt.Errorf("tool/execHTTP: expand url: %w", err)
	}

	// Append query parameters.
	if len(cfg.Query) > 0 {
		u, err := url.Parse(rawURL)
		if err != nil {
			return "", fmt.Errorf("tool/execHTTP: parse url: %w", err)
		}
		q := u.Query()
		for k, v := range cfg.Query {
			expanded, err := expandTemplate(v, args, allowedEnv)
			if err != nil {
				return "", fmt.Errorf("tool/execHTTP: expand query param %q: %w", k, err)
			}
			q.Set(k, expanded)
		}
		u.RawQuery = q.Encode()
		rawURL = u.String()
	}

	// Build the request body from Body map (serialized to JSON).
	var bodyReader io.Reader
	if len(cfg.Body) > 0 {
		bodyMap := make(map[string]string, len(cfg.Body))
		for k, v := range cfg.Body {
			expanded, err := expandTemplate(v, args, allowedEnv)
			if err != nil {
				return "", fmt.Errorf("tool/execHTTP: expand body field %q: %w", k, err)
			}
			bodyMap[k] = expanded
		}
		bodyBytes, err := json.Marshal(bodyMap)
		if err != nil {
			return "", fmt.Errorf("tool/execHTTP: marshal body: %w", err)
		}
		bodyReader = bytes.NewReader(bodyBytes)
	}

	method := strings.ToUpper(cfg.Method)
	if method == "" {
		method = "GET"
	}

	req, err := http.NewRequestWithContext(ctx, method, rawURL, bodyReader)
	if err != nil {
		return "", fmt.Errorf("tool/execHTTP: build request: %w", err)
	}
	if len(cfg.Body) > 0 {
		req.Header.Set("Content-Type", "application/json")
	}

	// Set headers — auth values are expanded but never logged.
	for k, v := range cfg.Headers {
		expanded, err := expandTemplate(v, args, allowedEnv)
		if err != nil {
			return "", fmt.Errorf("tool/execHTTP: expand header %q: %w", k, err)
		}
		req.Header.Set(k, expanded)
	}

	resp, err := toolClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("tool/execHTTP: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20)) // 1 MB max
	if err != nil {
		return "", fmt.Errorf("tool/execHTTP: read response: %w", err)
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		snippet := body
		if len(snippet) > 256 {
			snippet = snippet[:256]
		}
		return "", &httpError{status: resp.StatusCode, body: string(snippet), header: resp.Header}
	}

	return string(body), nil
}

// httpError carries the HTTP status code and body snippet so callers can
// inspect the status without reparsing the error string.
type httpError struct {
	status int
	body   string
	header http.Header
}

func (e *httpError) Error() string {
	return fmt.Sprintf("tool/execHTTP: status %d: %s", e.status, e.body)
}

// httpStatusFromErr returns the HTTP status code embedded in an httpError, or 0.
func httpStatusFromErr(err error) int {
	var he *httpError
	if errors.As(err, &he) {
		return he.status
	}
	return 0
}

// isTransient reports whether the error or HTTP status represents a condition
// that may resolve on retry (server-side overload, network blip, rate limit).
// Permanent failures (auth errors, bad requests) return false.
// A 403 with X-RateLimit-Remaining: 0 (GitHub-style quota exhaustion) is also
// treated as transient since it resolves once the quota resets.
func isTransient(statusCode int, err error) bool {
	if statusCode != 0 {
		switch statusCode {
		case 429, 500, 502, 503, 504:
			return true
		default:
			// 403 + rate-limit header is transient (quota exhaustion, not auth failure).
			var he *httpError
			if errors.As(err, &he) && he.status == 403 &&
				he.header != nil && he.header.Get("X-RateLimit-Remaining") == "0" {
				return true
			}
			return false
		}
	}
	if err == nil {
		return false
	}
	// Network-level transient errors.
	if os.IsTimeout(err) {
		return true
	}
	if errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) {
		return true
	}
	if errors.Is(err, syscall.ECONNRESET) || errors.Is(err, syscall.ECONNREFUSED) {
		return true
	}
	// url.Error wraps net errors (includes timeouts).
	var ue *url.Error
	if errors.As(err, &ue) {
		return os.IsTimeout(ue.Err) || errors.Is(ue.Err, io.EOF) ||
			errors.Is(ue.Err, syscall.ECONNRESET) || errors.Is(ue.Err, syscall.ECONNREFUSED)
	}
	return false
}

// backoffDelay computes how long to wait before the next attempt.
// attempt is 1-indexed (attempt=1 means the first failure; next attempt is #2).
func backoffDelay(attempt int, cfg model.ToolRetryConfig, err error) time.Duration {
	// If Retry-After header is present and honored, use it.
	if cfg.HonorRetryAfter {
		var he *httpError
		if errors.As(err, &he) && he.header != nil {
			wait := retryWait(he.header, cfg.MaxDelay)
			return wait
		}
	}
	// Exponential backoff: BaseDelay * 2^(attempt-1), capped at MaxDelay.
	exp := attempt - 1
	if exp > 30 {
		exp = 30
	}
	delay := cfg.BaseDelay
	for i := 0; i < exp; i++ {
		delay *= 2
		if delay > cfg.MaxDelay {
			delay = cfg.MaxDelay
			break
		}
	}
	// Add up to 10% jitter.
	jitter := time.Duration(rand.Int63n(int64(delay/10) + 1)) //nolint:gosec
	return delay + jitter
}

// isRateLimited reports whether resp indicates a rate-limit condition:
// 429 (Too Many Requests) or 403 with X-RateLimit-Remaining: 0.
func isRateLimited(resp *http.Response) bool {
	if resp.StatusCode == 429 {
		return true
	}
	return resp.StatusCode == 403 && resp.Header.Get("X-RateLimit-Remaining") == "0"
}

// retryWait returns how long to wait before retrying, capped at max.
// It reads Retry-After (seconds) first, then X-RateLimit-Reset (Unix timestamp).
// Falls back to max if neither header is present or parseable.
func retryWait(header http.Header, max time.Duration) time.Duration {
	if v := header.Get("Retry-After"); v != "" {
		if secs, err := strconv.Atoi(v); err == nil && secs >= 0 {
			d := time.Duration(secs) * time.Second
			if d < max {
				return d
			}
			return max
		}
	}
	if v := header.Get("X-RateLimit-Reset"); v != "" {
		if ts, err := strconv.ParseInt(v, 10, 64); err == nil {
			d := time.Until(time.Unix(ts, 0))
			if d > 0 && d < max {
				return d
			}
			if d <= 0 {
				return 0
			}
			return max
		}
	}
	return max
}

// expandTemplate replaces {{env:VAR}} and {{.argName}} placeholders in s.
// {{env:VAR}} is replaced with os.Getenv(VAR). When allowedEnv is non-nil,
// only variables in the allowlist are permitted; others produce an error.
// {{.argName}} is replaced with the string representation of args[argName].
func expandTemplate(s string, args map[string]any, allowedEnv []string) (string, error) {
	var buf strings.Builder
	remaining := s

	for {
		start := strings.Index(remaining, "{{")
		if start < 0 {
			buf.WriteString(remaining)
			break
		}
		buf.WriteString(remaining[:start])
		remaining = remaining[start+2:]

		end := strings.Index(remaining, "}}")
		if end < 0 {
			return "", fmt.Errorf("unclosed template expression in %q", s)
		}
		expr := strings.TrimSpace(remaining[:end])
		remaining = remaining[end+2:]

		if strings.HasPrefix(expr, "env:") {
			varName := strings.TrimPrefix(expr, "env:")
			if allowedEnv != nil && !envAllowed(varName, allowedEnv) {
				return "", fmt.Errorf("tool/expandTemplate: env var %q is not in the tool's allowed_env list", varName)
			}
			buf.WriteString(os.Getenv(varName))
		} else if strings.HasPrefix(expr, ".") {
			argName := strings.TrimPrefix(expr, ".")
			if v, ok := args[argName]; ok {
				_, _ = fmt.Fprintf(&buf, "%v", v)
			}
			// Missing arg → empty string (not an error; model may omit optional args).
		} else {
			return "", fmt.Errorf("unrecognized template expression %q", expr)
		}
	}
	return buf.String(), nil
}

// envAllowed reports whether varName appears in the allowlist.
func envAllowed(varName string, allowedEnv []string) bool {
	for _, v := range allowedEnv {
		if v == varName {
			return true
		}
	}
	return false
}

package tool

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/tgpski/leather/internal/mcp"
	"github.com/tgpski/leather/internal/model"
)

// toolClient is a shared HTTP client with a conservative timeout.
// Using a package-level client allows connection reuse while preventing
// tool calls from blocking indefinitely on unresponsive endpoints.
var toolClient = &http.Client{Timeout: 30 * time.Second}

// Executor dispatches tool calls to the appropriate backend.
// The zero value (all fields nil) handles http-type tools only.
type Executor struct {
	// MCP is the registry of running MCP server clients.
	// Nil means mcp-type tools are unavailable.
	MCP *mcp.Registry
}

// Execute dispatches a ToolCall to the appropriate executor based on def.Type.
// Authentication header values are never logged.
func (e *Executor) Execute(ctx context.Context, def model.ToolDefinition, args map[string]any) model.ToolResult {
	result := model.ToolResult{Name: def.Name}
	var content string
	var execErr error

	switch def.Type {
	case "http", "":
		content, execErr = execHTTP(ctx, def.HTTP, args, def.AllowedEnv)
	case "mcp":
		if e.MCP == nil {
			execErr = fmt.Errorf("mcp tool %q: no MCP registry configured", def.Name)
		} else {
			content, execErr = execMCP(ctx, e.MCP, def.MCP, args)
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

// execHTTP performs an HTTP tool call by expanding templates, building the
// request, and returning the response body as a string. On a rate-limit
// response (429 or 403 with X-RateLimit-Remaining: 0) it waits up to 60 s
// and retries exactly once.
func execHTTP(ctx context.Context, cfg model.HTTPToolConfig, args map[string]any, allowedEnv []string) (string, error) {
	return execHTTPInner(ctx, cfg, args, allowedEnv, true)
}

// execHTTPInner is the implementation of execHTTP. allowRetry controls whether
// a rate-limited response triggers a single retry attempt.
func execHTTPInner(ctx context.Context, cfg model.HTTPToolConfig, args map[string]any, allowedEnv []string, allowRetry bool) (string, error) {
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

	if allowRetry && isRateLimited(resp) {
		wait := retryWait(resp, 60*time.Second)
		select {
		case <-time.After(wait):
		case <-ctx.Done():
			return "", fmt.Errorf("tool/execHTTP: rate limit wait cancelled: %w", ctx.Err())
		}
		return execHTTPInner(ctx, cfg, args, allowedEnv, false)
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		snippet := body
		if len(snippet) > 256 {
			snippet = snippet[:256]
		}
		return "", fmt.Errorf("tool/execHTTP: status %d: %s", resp.StatusCode, snippet)
	}

	return string(body), nil
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
func retryWait(resp *http.Response, max time.Duration) time.Duration {
	if v := resp.Header.Get("Retry-After"); v != "" {
		if secs, err := strconv.Atoi(v); err == nil && secs >= 0 {
			d := time.Duration(secs) * time.Second
			if d < max {
				return d
			}
			return max
		}
	}
	if v := resp.Header.Get("X-RateLimit-Reset"); v != "" {
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

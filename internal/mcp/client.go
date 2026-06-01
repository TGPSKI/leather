package mcp

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/tgpski/leather/internal/model"
)

// Client manages a JSON-RPC 2.0 connection to a single MCP server process
// over the stdio transport. Calls are serialized: only one in-flight at a time.
type Client struct {
	cmd      *exec.Cmd
	enc      *json.Encoder
	dec      *json.Decoder
	closer   io.Closer     // stdin pipe; closed on Client.Close
	stderr   io.ReadCloser // child stderr pipe (T4.4)
	stderrWG sync.WaitGroup
	mu       sync.Mutex
	nextID   int64
	name     string                    // server name for error messages
	schemas  map[string]map[string]any // tool name → inputSchema from tools/list

	// poisoned is set when a readResponse times out. The decode goroutine
	// from that call may still be running against c.dec, so any subsequent
	// caller must refuse to use the connection — a concurrent decode would
	// race with the leftover goroutine. Caller must Close() and restart.
	poisoned bool
}

// ErrPoisoned is returned by Call after a previous request timed out. The
// connection is no longer safe to use; the caller must Close and recreate the
// Client.
var ErrPoisoned = fmt.Errorf("mcp: client poisoned by prior read timeout; recreate client")

// rpcRequest is a JSON-RPC 2.0 request or notification.
// When ID is nil the message is a notification (no response expected).
type rpcRequest struct {
	JSONRPC string `json:"jsonrpc"`
	ID      *int64 `json:"id,omitempty"`
	Method  string `json:"method"`
	Params  any    `json:"params,omitempty"`
}

// rpcResponse is a JSON-RPC 2.0 response.
type rpcResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      int64           `json:"id"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   *rpcError       `json:"error,omitempty"`
}

// rpcError is the JSON-RPC 2.0 error object.
type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

// Start launches the MCP server process described by cfg and runs the JSON-RPC
// 2.0 initialization handshake (initialize → notifications/initialized).
// ctx is used for the handshake timeout only; the process itself runs until
// Close is called. Do not pass a short-lived context — it must outlast the
// handshake but the process will keep running after ctx is cancelled.
func Start(ctx context.Context, cfg model.MCPServerConfig) (*Client, error) {
	parts := strings.Fields(cfg.Command)
	if len(parts) == 0 {
		return nil, fmt.Errorf("mcp/Start: empty command for server %q", cfg.Name)
	}
	// Use exec.Command (not CommandContext) so that cancelling the handshake
	// context does not kill the long-lived server process.
	cmd := exec.Command(parts[0], parts[1:]...)
	// T4.4: put child in its own process group so we can signal it and any
	// grandchildren cleanly on Close.
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	if len(cfg.Env) > 0 {
		resolved, err := resolveEnvVars(ctx, cfg.Env)
		if err != nil {
			return nil, fmt.Errorf("mcp/Start %s: %w", cfg.Name, err)
		}
		// Inherit the current process environment and append the resolved vars.
		cmd.Env = append(os.Environ(), resolved...)
	}
	return startCmd(ctx, cfg.Name, cmd)
}

// startCmd is the low-level constructor. It is unexported but accessible to
// in-package tests via package mcp.
func startCmd(ctx context.Context, name string, cmd *exec.Cmd) (*Client, error) {
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, fmt.Errorf("mcp/Start %s: stdin pipe: %w", name, err)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, fmt.Errorf("mcp/Start %s: stdout pipe: %w", name, err)
	}
	// T4.4: capture child stderr instead of inheriting (which would interleave
	// with our own logs). A goroutine forwards each line to our stderr with a
	// `mcp[<name>]:` prefix so failures are visible.
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return nil, fmt.Errorf("mcp/Start %s: stderr pipe: %w", name, err)
	}
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("mcp/Start %s: start process: %w", name, err)
	}
	c := &Client{
		cmd:    cmd,
		enc:    json.NewEncoder(stdin),
		dec:    json.NewDecoder(stdout),
		closer: stdin,
		stderr: stderr,
		name:   name,
	}
	c.stderrWG.Add(1)
	go func() {
		defer c.stderrWG.Done()
		scanner := bufio.NewScanner(stderr)
		scanner.Buffer(make([]byte, 0, 4096), 1<<20)
		for scanner.Scan() {
			fmt.Fprintf(os.Stderr, "mcp[%s]: %s\n", name, scanner.Text())
		}
	}()
	if err := c.initialize(ctx); err != nil {
		_ = cmd.Process.Kill()
		return nil, fmt.Errorf("mcp/Start %s: %w", name, err)
	}
	if err := c.fetchToolSchemas(ctx); err != nil {
		// Non-fatal: tool calls will still work; schemas just won't be sent to the LLM.
		_ = err
	}
	return c, nil
}

// initialize runs the MCP protocol handshake:
// 1. Send "initialize" request and wait for the response.
// 2. Send "notifications/initialized" notification.
func (c *Client) initialize(ctx context.Context) error {
	id := c.newID()

	type clientInfo struct {
		Name    string `json:"name"`
		Version string `json:"version"`
	}
	type initParams struct {
		ProtocolVersion string     `json:"protocolVersion"`
		Capabilities    struct{}   `json:"capabilities"`
		ClientInfo      clientInfo `json:"clientInfo"`
	}

	params := initParams{
		ProtocolVersion: "2024-11-05",
		ClientInfo:      clientInfo{Name: "leather", Version: "1"},
	}
	if err := c.enc.Encode(rpcRequest{
		JSONRPC: "2.0",
		ID:      &id,
		Method:  "initialize",
		Params:  params,
	}); err != nil {
		return fmt.Errorf("send initialize: %w", err)
	}

	if _, err := c.readResponse(ctx, id); err != nil {
		return fmt.Errorf("receive initialize response: %w", err)
	}

	// Send initialized notification — fire-and-forget (no ID, no response expected).
	if err := c.enc.Encode(rpcRequest{
		JSONRPC: "2.0",
		Method:  "notifications/initialized",
	}); err != nil {
		return fmt.Errorf("send initialized notification: %w", err)
	}
	return nil
}

// fetchToolSchemas calls tools/list on the server and caches each tool's inputSchema.
// Must be called after initialize and before concurrent use (mu not held).
func (c *Client) fetchToolSchemas(ctx context.Context) error {
	id := c.newID()
	if err := c.enc.Encode(rpcRequest{
		JSONRPC: "2.0",
		ID:      &id,
		Method:  "tools/list",
	}); err != nil {
		return fmt.Errorf("mcp/fetchToolSchemas %s: encode: %w", c.name, err)
	}
	raw, err := c.readResponse(ctx, id)
	if err != nil {
		return fmt.Errorf("mcp/fetchToolSchemas %s: %w", c.name, err)
	}
	var result struct {
		Tools []struct {
			Name        string         `json:"name"`
			InputSchema map[string]any `json:"inputSchema"`
		} `json:"tools"`
	}
	if err := json.Unmarshal(raw, &result); err != nil {
		return fmt.Errorf("mcp/fetchToolSchemas %s: unmarshal: %w", c.name, err)
	}
	schemas := make(map[string]map[string]any, len(result.Tools))
	for _, t := range result.Tools {
		if t.InputSchema != nil {
			schemas[t.Name] = t.InputSchema
		}
	}
	c.schemas = schemas
	return nil
}

// ToolSchema returns the JSON Schema for the named tool as reported by the server.
// Returns nil when the schema was not fetched or the tool is unknown.
func (c *Client) ToolSchema(tool string) map[string]any {
	if c.schemas == nil {
		return nil
	}
	return c.schemas[tool]
}

// Call invokes a named tool on the MCP server and returns the joined text content.
// Calls are serialized; only one may be in flight at a time.
func (c *Client) Call(ctx context.Context, toolName string, args map[string]any) (string, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.poisoned {
		return "", fmt.Errorf("mcp/Client.Call %s.%s: %w", c.name, toolName, ErrPoisoned)
	}

	id := c.newID()
	if err := c.enc.Encode(rpcRequest{
		JSONRPC: "2.0",
		ID:      &id,
		Method:  "tools/call",
		Params: map[string]any{
			"name":      toolName,
			"arguments": args,
		},
	}); err != nil {
		return "", fmt.Errorf("mcp/Client.Call %s.%s: encode: %w", c.name, toolName, err)
	}

	raw, err := c.readResponse(ctx, id)
	if err != nil {
		return "", fmt.Errorf("mcp/Client.Call %s.%s: %w", c.name, toolName, err)
	}
	return extractTextContent(raw), nil
}

// extractTextContent parses an MCP tools/call result and returns the joined text.
// Falls back to the raw JSON string when the result is not in the expected format.
func extractTextContent(raw json.RawMessage) string {
	var result struct {
		Content []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"content"`
	}
	if err := json.Unmarshal(raw, &result); err != nil || len(result.Content) == 0 {
		return string(raw)
	}
	var sb strings.Builder
	for _, item := range result.Content {
		if item.Type == "text" {
			if sb.Len() > 0 {
				sb.WriteByte('\n')
			}
			sb.WriteString(item.Text)
		}
	}
	if sb.Len() == 0 {
		return string(raw)
	}
	return sb.String()
}

// readResponse reads the next JSON-RPC 2.0 response from the server and
// verifies that its ID matches expectedID. A goroutine is used so that
// context cancellation is respected even when the decoder is blocking.
//
// On ctx timeout the decode goroutine is left running against c.dec. To
// prevent a future call from spawning a concurrent decoder (which would
// race on c.dec), the client is marked poisoned and subsequent Calls return
// ErrPoisoned. The stdin pipe is also closed to encourage the server to exit
// so the leftover decoder receives EOF promptly.
func (c *Client) readResponse(ctx context.Context, expectedID int64) (json.RawMessage, error) {
	type decodeResult struct {
		resp rpcResponse
		err  error
	}
	ch := make(chan decodeResult, 1)
	go func() {
		var resp rpcResponse
		err := c.dec.Decode(&resp)
		ch <- decodeResult{resp, err}
	}()
	select {
	case <-ctx.Done():
		c.poisoned = true
		if c.closer != nil {
			_ = c.closer.Close()
		}
		return nil, ctx.Err()
	case r := <-ch:
		if r.err != nil {
			return nil, r.err
		}
		if r.resp.ID != expectedID {
			return nil, fmt.Errorf("response ID mismatch: got %d, want %d", r.resp.ID, expectedID)
		}
		if r.resp.Error != nil {
			return nil, fmt.Errorf("server error %d: %s", r.resp.Error.Code, r.resp.Error.Message)
		}
		return r.resp.Result, nil
	}
}

// Close terminates the MCP server process group. It first closes stdin
// (giving the server a chance to exit cleanly), then SIGTERMs the process
// group, waits up to 5 s for the process to exit, and only escalates to
// SIGKILL if necessary. Errors are best-effort.
func (c *Client) Close() error {
	if c.closer != nil {
		_ = c.closer.Close()
	}
	if c.cmd == nil || c.cmd.Process == nil {
		return nil
	}
	pid := c.cmd.Process.Pid
	// Negative PID targets the process group (T4.4).
	_ = syscall.Kill(-pid, syscall.SIGTERM)
	done := make(chan error, 1)
	go func() { done <- c.cmd.Wait() }()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		_ = syscall.Kill(-pid, syscall.SIGKILL)
		<-done
	}
	c.stderrWG.Wait()
	return nil
}

// newID returns the next monotonically increasing request ID.
// Must be called while holding c.mu (or before concurrent access begins).
func (c *Client) newID() int64 {
	c.nextID++
	return c.nextID
}

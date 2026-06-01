// shell-mcp is a stdlib-only MCP stdio server that exposes fast CLI tools as
// model-callable tools. Tool definitions live in a JSON config file, so any
// binary on $PATH can be wired up without recompiling.
//
// Usage:
//
//	shell-mcp [/path/to/shell-tools.json]
//	SHELL_MCP_CONFIG=/path/to/shell-tools.json shell-mcp
//
// Config is resolved in order: first positional arg, then SHELL_MCP_CONFIG
// env var, then ~/.leather/shell-tools.json.
//
// MCP protocol: JSON-RPC 2.0 over stdin/stdout (newline-delimited JSON).
package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// defaultOutputCap is the byte ceiling applied to every tool result.
// Keeps individual tool calls within a safe fraction of the 8192-token budget.
const defaultOutputCap = 4000

// toolDef describes a single executable tool in the JSON config.
type toolDef struct {
	// Name is the MCP tool name (snake_case).
	Name string `json:"name"`
	// Description is shown to the model in tools/list.
	Description string `json:"description"`
	// Command is the executable name (looked up via PATH).
	Command string `json:"command"`
	// Args is the argument list; {{key}} placeholders are substituted at call time.
	Args []string `json:"args"`
	// Required lists argument keys that must be present in every call.
	Required []string `json:"required,omitempty"`
	// Defaults provides fallback values for optional argument keys.
	Defaults map[string]string `json:"defaults,omitempty"`
	// Optional, when true, returns a graceful message if Command is not on PATH.
	Optional bool `json:"optional,omitempty"`
}

// config is the root of the JSON config file.
type config struct {
	// OutputCapBytes overrides defaultOutputCap when set to a positive value.
	OutputCapBytes int `json:"output_cap_bytes,omitempty"`
	// Tools is the ordered list of tool definitions.
	Tools []toolDef `json:"tools"`
}

// rpcRequest is an incoming JSON-RPC 2.0 request or notification.
type rpcRequest struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      *int64          `json:"id"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params"`
}

// rpcResponse is an outgoing JSON-RPC 2.0 response.
type rpcResponse struct {
	JSONRPC string  `json:"jsonrpc"`
	ID      int64   `json:"id"`
	Result  any     `json:"result,omitempty"`
	Error   *rpcErr `json:"error,omitempty"`
}

// rpcErr is the JSON-RPC 2.0 error object.
type rpcErr struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

// server holds the loaded config and a name→def index built at startup.
type server struct {
	cfg       config
	byName    map[string]*toolDef
	outputCap int
}

func main() {
	cfgPath, err := resolveConfigPath()
	if err != nil {
		fmt.Fprintln(os.Stderr, "shell-mcp:", err)
		os.Exit(1)
	}

	raw, err := os.ReadFile(cfgPath)
	if err != nil {
		fmt.Fprintln(os.Stderr, "shell-mcp: read config:", err)
		os.Exit(1)
	}

	var cfg config
	if err := json.Unmarshal(raw, &cfg); err != nil {
		fmt.Fprintln(os.Stderr, "shell-mcp: parse config:", err)
		os.Exit(1)
	}

	s := &server{
		cfg:       cfg,
		byName:    make(map[string]*toolDef, len(cfg.Tools)),
		outputCap: cfg.OutputCapBytes,
	}
	if s.outputCap <= 0 {
		s.outputCap = defaultOutputCap
	}
	for i := range cfg.Tools {
		s.byName[cfg.Tools[i].Name] = &cfg.Tools[i]
	}

	s.serve()
}

// serve runs the JSON-RPC 2.0 stdin/stdout loop until EOF or error.
func (s *server) serve() {
	enc := json.NewEncoder(os.Stdout)
	scanner := bufio.NewScanner(os.Stdin)
	scanner.Buffer(make([]byte, 1<<20), 1<<20)

	for scanner.Scan() {
		line := bytes.TrimSpace(scanner.Bytes())
		if len(line) == 0 {
			continue
		}
		var req rpcRequest
		if err := json.Unmarshal(line, &req); err != nil {
			continue // skip malformed frames
		}
		if req.ID == nil {
			// Notification (e.g., notifications/initialized) — no response.
			continue
		}
		_ = enc.Encode(s.dispatch(req))
	}
}

// dispatch routes an incoming request to the appropriate handler.
func (s *server) dispatch(req rpcRequest) rpcResponse {
	switch req.Method {
	case "initialize":
		return s.respond(*req.ID, map[string]any{
			"protocolVersion": "2024-11-05",
			"capabilities":    map[string]any{"tools": map[string]any{}},
			"serverInfo":      map[string]any{"name": "shell-mcp", "version": "1"},
		})
	case "tools/list":
		return s.respond(*req.ID, map[string]any{"tools": s.toolList()})
	case "tools/call":
		return s.handleCall(req)
	default:
		return s.errResp(*req.ID, -32601, "method not found: "+req.Method)
	}
}

// toolList returns the MCP tool descriptors for all configured tools.
func (s *server) toolList() []map[string]any {
	out := make([]map[string]any, 0, len(s.cfg.Tools))
	for _, t := range s.cfg.Tools {
		props := map[string]any{}
		for _, req := range t.Required {
			props[req] = map[string]any{"type": "string"}
		}
		for k := range t.Defaults {
			if _, exists := props[k]; !exists {
				props[k] = map[string]any{"type": "string"}
			}
		}
		out = append(out, map[string]any{
			"name":        t.Name,
			"description": t.Description,
			"inputSchema": map[string]any{
				"type":       "object",
				"properties": props,
				"required":   t.Required,
			},
		})
	}
	return out
}

// handleCall executes the requested tool and returns an MCP content response.
func (s *server) handleCall(req rpcRequest) rpcResponse {
	var params struct {
		Name      string         `json:"name"`
		Arguments map[string]any `json:"arguments"`
	}
	if err := json.Unmarshal(req.Params, &params); err != nil {
		return s.errResp(*req.ID, -32600, "invalid params: "+err.Error())
	}

	def, ok := s.byName[params.Name]
	if !ok {
		return s.errResp(*req.ID, -32602, "unknown tool: "+params.Name)
	}

	text, err := s.execute(def, params.Arguments)
	if err != nil {
		text = "error: " + err.Error()
	}
	return s.respond(*req.ID, map[string]any{
		"content": []map[string]any{{"type": "text", "text": text}},
	})
}

// execute runs a tool definition with the given call arguments.
func (s *server) execute(def *toolDef, callArgs map[string]any) (string, error) {
	// Merge defaults under call args (call args win).
	merged := make(map[string]string, len(def.Defaults)+len(callArgs))
	for k, v := range def.Defaults {
		merged[k] = v
	}
	for k, v := range callArgs {
		merged[k] = fmt.Sprintf("%v", v)
	}

	// Substitute {{key}} placeholders in each argument.
	args := make([]string, len(def.Args))
	for i, tmpl := range def.Args {
		args[i] = applyVars(tmpl, merged)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, def.Command, args...)
	out, err := cmd.Output()
	if err != nil {
		if def.Optional && isNotFound(err) {
			return fmt.Sprintf("%s not installed — install it to use %s", def.Command, def.Name), nil
		}
		if ee, ok := err.(*exec.ExitError); ok && len(ee.Stderr) > 0 {
			return "", fmt.Errorf("exit %d: %s", ee.ExitCode(), strings.TrimSpace(string(ee.Stderr)))
		}
		return "", err
	}

	return capOutput(out, s.outputCap), nil
}

// applyVars replaces {{key}} occurrences in s with values from vars.
func applyVars(s string, vars map[string]string) string {
	for k, v := range vars {
		s = strings.ReplaceAll(s, "{{"+k+"}}", v)
	}
	return s
}

// capOutput truncates output at cap bytes, trimming to the last newline.
func capOutput(out []byte, cap int) string {
	if len(out) > cap {
		out = out[:cap]
		if i := bytes.LastIndexByte(out, '\n'); i >= 0 {
			out = out[:i+1]
		}
		out = append(out, "\n[output capped]\n"...)
	}
	return string(out)
}

// isNotFound returns true if the error indicates the executable was not found.
func isNotFound(err error) bool {
	if ee, ok := err.(*exec.ExitError); ok {
		_ = ee
		return false
	}
	return strings.Contains(err.Error(), "executable file not found") ||
		strings.Contains(err.Error(), "no such file or directory")
}

// resolveConfigPath returns the config file path from args, env, or default.
func resolveConfigPath() (string, error) {
	if len(os.Args) > 1 {
		return os.Args[1], nil
	}
	if v := os.Getenv("SHELL_MCP_CONFIG"); v != "" {
		return v, nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("no config path given and cannot find home dir: %w", err)
	}
	p := filepath.Join(home, ".leather", "shell-tools.json")
	if _, err := os.Stat(p); err != nil {
		return "", fmt.Errorf("no config path given and default %s not found", p)
	}
	return p, nil
}

// respond constructs a successful JSON-RPC 2.0 response.
func (s *server) respond(id int64, result any) rpcResponse {
	return rpcResponse{JSONRPC: "2.0", ID: id, Result: result}
}

// errResp constructs a JSON-RPC 2.0 error response.
func (s *server) errResp(id int64, code int, msg string) rpcResponse {
	return rpcResponse{JSONRPC: "2.0", ID: id, Error: &rpcErr{Code: code, Message: msg}}
}

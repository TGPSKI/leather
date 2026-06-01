package mcp

import (
	"context"
	"encoding/json"
	"io"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/tgpski/leather/internal/model"
)

// TestMain allows the test binary to be re-invoked as a fake MCP server.
// When LEATHER_MCP_TEST_SERVER=1, the process serves fake MCP responses on
// stdin/stdout until stdin closes, then exits 0. All real tests run normally
// in the absence of that env var.
func TestMain(m *testing.M) {
	if os.Getenv("LEATHER_MCP_TEST_SERVER") == "1" {
		runFakeMCPServer(os.Stdin, os.Stdout)
		os.Exit(0)
	}
	os.Exit(m.Run())
}

// runFakeMCPServer responds to initialize and tools/call JSON-RPC 2.0 messages.
func runFakeMCPServer(r io.Reader, w io.Writer) {
	dec := json.NewDecoder(r)
	enc := json.NewEncoder(w)
	for {
		var req map[string]json.RawMessage
		if err := dec.Decode(&req); err != nil {
			return // stdin closed or EOF
		}
		method := strings.Trim(string(req["method"]), `"`)

		var id int64
		if idRaw, ok := req["id"]; ok {
			_ = json.Unmarshal(idRaw, &id)
		}

		switch method {
		case "initialize":
			_ = enc.Encode(map[string]any{
				"jsonrpc": "2.0",
				"id":      id,
				"result": map[string]any{
					"protocolVersion": "2024-11-05",
					"capabilities":    map[string]any{"tools": map[string]any{}},
					"serverInfo":      map[string]any{"name": "fake-mcp", "version": "0.1"},
				},
			})
		case "notifications/initialized":
			// Notification — no response.
		case "tools/list":
			_ = enc.Encode(map[string]any{
				"jsonrpc": "2.0",
				"id":      id,
				"result":  map[string]any{"tools": []any{}},
			})
		case "tools/call":
			var params struct {
				Name string `json:"name"`
			}
			_ = json.Unmarshal(req["params"], &params)
			_ = enc.Encode(map[string]any{
				"jsonrpc": "2.0",
				"id":      id,
				"result": map[string]any{
					"content": []any{
						map[string]any{"type": "text", "text": "result for " + params.Name},
					},
				},
			})
		}
	}
}

// fakeMCPCmd returns an exec.Cmd that re-runs the current test binary as the
// fake MCP server. It matches no actual tests (-test.run=^$) so m.Run()
// exits immediately before TestMain restores normal test-runner control.
func fakeMCPCmd() *exec.Cmd {
	cmd := exec.Command(os.Args[0], "-test.run=^$")
	cmd.Env = append(os.Environ(), "LEATHER_MCP_TEST_SERVER=1")
	return cmd
}

// --- Loader tests ---

func TestLoadServers_NotExist(t *testing.T) {
	cfgs, err := LoadServers("/does/not/exist.yaml")
	if err != nil {
		t.Fatalf("unexpected error for missing file: %v", err)
	}
	if len(cfgs) != 0 {
		t.Errorf("expected empty slice, got %v", cfgs)
	}
}

func TestParseServersYAML(t *testing.T) {
	cases := []struct {
		name    string
		src     string
		want    []model.MCPServerConfig
		wantErr bool
	}{
		{
			name: "single server default transport",
			src: `servers:
  - name: skeptic
    command: skeptic mcp
`,
			want: []model.MCPServerConfig{
				{Name: "skeptic", Command: "skeptic mcp", Transport: "stdio"},
			},
		},
		{
			name: "explicit transport",
			src: `servers:
  - name: skeptic
    command: skeptic mcp
    transport: stdio
`,
			want: []model.MCPServerConfig{
				{Name: "skeptic", Command: "skeptic mcp", Transport: "stdio"},
			},
		},
		{
			name: "two servers",
			src: `servers:
  - name: server-a
    command: server-a serve
  - name: server-b
    command: /usr/local/bin/server-b --mcp
`,
			want: []model.MCPServerConfig{
				{Name: "server-a", Command: "server-a serve", Transport: "stdio"},
				{Name: "server-b", Command: "/usr/local/bin/server-b --mcp", Transport: "stdio"},
			},
		},
		{
			name:    "missing name",
			src:     "servers:\n  - command: skeptic mcp\n",
			wantErr: true,
		},
		{
			name:    "missing command",
			src:     "servers:\n  - name: skeptic\n",
			wantErr: true,
		},
		{
			name: "comments and blank lines ignored",
			src: `# MCP servers

servers:
  # uses stdio
  - name: skeptic
    command: skeptic mcp
`,
			want: []model.MCPServerConfig{
				{Name: "skeptic", Command: "skeptic mcp", Transport: "stdio"},
			},
		},
		{
			name:    "empty file",
			src:     "",
			want:    nil,
			wantErr: false,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := parseServersYAML(tc.src)
			if (err != nil) != tc.wantErr {
				t.Fatalf("wantErr=%v, got err=%v", tc.wantErr, err)
			}
			if tc.wantErr {
				return
			}
			if len(got) != len(tc.want) {
				t.Fatalf("len=%d, want %d; got %v", len(got), len(tc.want), got)
			}
			for i, g := range got {
				w := tc.want[i]
				if g.Name != w.Name || g.Command != w.Command || g.Transport != w.Transport {
					t.Errorf("[%d] got %+v, want %+v", i, g, w)
				}
			}
		})
	}
}

// --- Client / Registry tests ---

func TestMCPClient_Initialize(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	c, err := startCmd(ctx, "fake", fakeMCPCmd())
	if err != nil {
		t.Fatalf("startCmd: %v", err)
	}
	defer c.Close()
}

func TestMCPClient_Call(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	c, err := startCmd(ctx, "fake", fakeMCPCmd())
	if err != nil {
		t.Fatalf("startCmd: %v", err)
	}
	defer c.Close()

	result, err := c.Call(ctx, "scan_repo", map[string]any{"path": "/repo"})
	if err != nil {
		t.Fatalf("Call: %v", err)
	}
	if result != "result for scan_repo" {
		t.Errorf("Call result = %q, want %q", result, "result for scan_repo")
	}
}

func TestMCPClient_MultipleCallsSerialised(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	c, err := startCmd(ctx, "fake", fakeMCPCmd())
	if err != nil {
		t.Fatalf("startCmd: %v", err)
	}
	defer c.Close()

	for i, toolName := range []string{"tool_a", "tool_b", "tool_c"} {
		result, err := c.Call(ctx, toolName, nil)
		if err != nil {
			t.Fatalf("call %d: %v", i, err)
		}
		want := "result for " + toolName
		if result != want {
			t.Errorf("call %d: got %q, want %q", i, result, want)
		}
	}
}

func TestExtractTextContent(t *testing.T) {
	cases := []struct {
		name string
		raw  string
		want string
	}{
		{
			name: "single text item",
			raw:  `{"content":[{"type":"text","text":"hello"}]}`,
			want: "hello",
		},
		{
			name: "multiple text items joined",
			raw:  `{"content":[{"type":"text","text":"a"},{"type":"text","text":"b"}]}`,
			want: "a\nb",
		},
		{
			name: "non-text items skipped",
			raw:  `{"content":[{"type":"image","url":"x"},{"type":"text","text":"hi"}]}`,
			want: "hi",
		},
		{
			name: "empty content falls back to raw",
			raw:  `{"content":[]}`,
			want: `{"content":[]}`,
		},
		{
			name: "unparseable falls back to raw",
			raw:  `"just a string"`,
			want: `"just a string"`,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := extractTextContent(json.RawMessage(tc.raw))
			if got != tc.want {
				t.Errorf("got %q, want %q", got, tc.want)
			}
		})
	}
}

func TestRegistry_GetUnknown(t *testing.T) {
	reg := NewRegistry(nil, nil)
	_, ok := reg.Get("nonexistent")
	if ok {
		t.Error("expected Get to return false for unknown server")
	}
}

// fakeServerConfig returns an MCPServerConfig that re-runs the current test
// binary as the fake MCP server.
func fakeServerConfig(name string) model.MCPServerConfig {
	// Use the `env` command to inject LEATHER_MCP_TEST_SERVER=1 so that the
	// re-invoked test binary enters the fake-server path in TestMain.
	return model.MCPServerConfig{
		Name:      name,
		Command:   "env LEATHER_MCP_TEST_SERVER=1 " + os.Args[0] + " -test.run ^$",
		Transport: "stdio",
	}
}

// badServerConfig returns an MCPServerConfig that always fails to start.
func badServerConfig(name string) model.MCPServerConfig {
	return model.MCPServerConfig{
		Name:      name,
		Command:   "/does/not/exist/binary",
		Transport: "stdio",
	}
}

func TestStartAll_AllOK(t *testing.T) {
	cfg := fakeServerConfig("srv-a")
	reg := NewRegistry([]model.MCPServerConfig{cfg}, nil)
	ctx := context.Background()
	if err := reg.StartAll(ctx); err != nil {
		t.Fatalf("StartAll with good server: %v", err)
	}
	reg.StopAll()
}

func TestStartAll_OneFails(t *testing.T) {
	good := fakeServerConfig("good")
	bad := badServerConfig("bad")
	reg := NewRegistry([]model.MCPServerConfig{good, bad}, nil)
	ctx := context.Background()
	// One server starts successfully → no error returned.
	err := reg.StartAll(ctx)
	if err != nil {
		t.Fatalf("StartAll with one good server should not error, got: %v", err)
	}
	// The good server should be accessible.
	if _, ok := reg.Get("good"); !ok {
		t.Error("expected good server to be registered")
	}
	reg.StopAll()
}

func TestStartAll_AllFail(t *testing.T) {
	bad1 := badServerConfig("bad1")
	bad2 := badServerConfig("bad2")
	reg := NewRegistry([]model.MCPServerConfig{bad1, bad2}, nil)
	ctx := context.Background()
	err := reg.StartAll(ctx)
	if err == nil {
		t.Fatal("StartAll with all failing servers should return error")
	}
}

func TestStartAll_EmptyConfig(t *testing.T) {
	reg := NewRegistry(nil, nil)
	ctx := context.Background()
	if err := reg.StartAll(ctx); err != nil {
		t.Fatalf("StartAll with no configs should return nil, got: %v", err)
	}
}

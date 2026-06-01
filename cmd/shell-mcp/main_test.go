package main

import (
	"bytes"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// --- helpers ---

// newTestServer builds a server from an in-process config, no file I/O.
func newTestServer(tools []toolDef, outputCap int) *server {
	s := &server{
		cfg:       config{Tools: tools},
		byName:    make(map[string]*toolDef, len(tools)),
		outputCap: outputCap,
	}
	if s.outputCap <= 0 {
		s.outputCap = defaultOutputCap
	}
	for i := range tools {
		s.byName[tools[i].Name] = &s.cfg.Tools[i]
	}
	return s
}

// initGitRepo creates a git repo with one commit in dir.
func initGitRepo(t *testing.T, dir string) {
	t.Helper()
	for _, args := range [][]string{
		{"init", "--initial-branch=main"},
		{"config", "user.email", "test@example.com"},
		{"config", "user.name", "Test"},
	} {
		c := exec.Command("git", append([]string{"-C", dir}, args...)...)
		if out, err := c.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	// Create a file and commit it.
	f := filepath.Join(dir, "hello.go")
	if err := os.WriteFile(f, []byte("package main\n"), 0600); err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{
		{"add", "."},
		{"commit", "-m", "init commit"},
	} {
		c := exec.Command("git", append([]string{"-C", dir}, args...)...)
		if out, err := c.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
}

// --- applyVars ---

func TestApplyVars(t *testing.T) {
	cases := []struct {
		tmpl string
		vars map[string]string
		want string
	}{
		{"git -C {{path}} log", map[string]string{"path": "/tmp/repo"}, "git -C /tmp/repo log"},
		{"{{a}} {{b}}", map[string]string{"a": "foo", "b": "bar"}, "foo bar"},
		{"no placeholders", map[string]string{"a": "x"}, "no placeholders"},
		{"{{missing}}", map[string]string{}, "{{missing}}"},
	}
	for _, tc := range cases {
		got := applyVars(tc.tmpl, tc.vars)
		if got != tc.want {
			t.Errorf("applyVars(%q) = %q, want %q", tc.tmpl, got, tc.want)
		}
	}
}

// --- capOutput ---

func TestCapOutput(t *testing.T) {
	short := []byte("hello\nworld\n")
	if got := capOutput(short, 100); got != string(short) {
		t.Errorf("short output modified: %q", got)
	}

	long := bytes.Repeat([]byte("x"), 5000)
	capped := capOutput(long, 4000)
	if len(capped) > 4050 { // allow for the [output capped] suffix
		t.Errorf("capped output too long: %d bytes", len(capped))
	}

	// Capping should include the marker.
	if !strings.Contains(capped, "[output capped]") {
		t.Error("capped output missing marker")
	}

	// Output with newlines should trim to the last newline before the cap.
	withNewlines := make([]byte, 4100)
	for i := range withNewlines {
		if i%80 == 79 {
			withNewlines[i] = '\n'
		} else {
			withNewlines[i] = 'a'
		}
	}
	capped2 := capOutput(withNewlines, 4000)
	if strings.HasPrefix(capped2, "\n") {
		t.Error("capped output starts with newline")
	}
}

// --- dispatch: initialize ---

func TestDispatchInitialize(t *testing.T) {
	s := newTestServer(nil, 0)
	id := int64(1)
	resp := s.dispatch(rpcRequest{JSONRPC: "2.0", ID: &id, Method: "initialize"})
	if resp.Error != nil {
		t.Fatalf("initialize error: %v", resp.Error.Message)
	}
	b, _ := json.Marshal(resp.Result)
	if !bytes.Contains(b, []byte("2024-11-05")) {
		t.Errorf("initialize result missing protocolVersion: %s", b)
	}
	if !bytes.Contains(b, []byte("shell-mcp")) {
		t.Errorf("initialize result missing server name: %s", b)
	}
}

// --- dispatch: tools/list ---

func TestDispatchToolsList(t *testing.T) {
	tools := []toolDef{
		{Name: "git_status", Description: "repo status", Command: "git",
			Args: []string{"-C", "{{path}}", "status", "--short"}, Required: []string{"path"}},
		{Name: "rg_search", Description: "search", Command: "rg",
			Args: []string{"{{pattern}}", "{{path}}"}, Required: []string{"path", "pattern"}, Optional: true},
	}
	s := newTestServer(tools, 0)
	id := int64(2)
	resp := s.dispatch(rpcRequest{JSONRPC: "2.0", ID: &id, Method: "tools/list"})
	if resp.Error != nil {
		t.Fatalf("tools/list error: %v", resp.Error.Message)
	}
	b, _ := json.Marshal(resp.Result)
	if !bytes.Contains(b, []byte("git_status")) {
		t.Errorf("tools/list missing git_status: %s", b)
	}
	if !bytes.Contains(b, []byte("rg_search")) {
		t.Errorf("tools/list missing rg_search: %s", b)
	}
}

// --- dispatch: unknown method ---

func TestDispatchUnknownMethod(t *testing.T) {
	s := newTestServer(nil, 0)
	id := int64(3)
	resp := s.dispatch(rpcRequest{JSONRPC: "2.0", ID: &id, Method: "bogus/method"})
	if resp.Error == nil || resp.Error.Code != -32601 {
		t.Errorf("expected -32601 error, got %+v", resp.Error)
	}
}

// --- execute: git_status with real git ---

func TestExecuteGitStatus(t *testing.T) {
	dir := t.TempDir()
	initGitRepo(t, dir)

	def := &toolDef{
		Name:    "git_status",
		Command: "git",
		Args:    []string{"-C", "{{path}}", "status", "--short", "--branch"},
	}
	s := newTestServer(nil, 0)
	out, err := s.execute(def, map[string]any{"path": dir})
	if err != nil {
		t.Fatalf("execute git_status: %v", err)
	}
	if !strings.Contains(out, "main") {
		t.Errorf("git_status output missing branch name, got: %q", out)
	}
}

// --- execute: git_log with real git ---

func TestExecuteGitLog(t *testing.T) {
	dir := t.TempDir()
	initGitRepo(t, dir)

	def := &toolDef{
		Name:     "git_log",
		Command:  "git",
		Args:     []string{"-C", "{{path}}", "log", "--oneline", "-{{n}}"},
		Defaults: map[string]string{"n": "10"},
	}
	s := newTestServer(nil, 0)
	out, err := s.execute(def, map[string]any{"path": dir})
	if err != nil {
		t.Fatalf("execute git_log: %v", err)
	}
	if !strings.Contains(out, "init commit") {
		t.Errorf("git_log output missing commit message, got: %q", out)
	}
}

// --- execute: optional tool not installed ---

func TestExecuteOptionalMissing(t *testing.T) {
	def := &toolDef{
		Name:     "scc_summary",
		Command:  "scc-binary-that-does-not-exist",
		Args:     []string{"{{path}}"},
		Optional: true,
	}
	s := newTestServer(nil, 0)
	out, err := s.execute(def, map[string]any{"path": "."})
	if err != nil {
		t.Fatalf("optional missing tool should not error: %v", err)
	}
	if !strings.Contains(out, "not installed") {
		t.Errorf("expected 'not installed' message, got: %q", out)
	}
}

// --- execute: defaults merged under call args ---

func TestExecuteDefaultsMerge(t *testing.T) {
	dir := t.TempDir()
	initGitRepo(t, dir)

	def := &toolDef{
		Name:     "git_log",
		Command:  "git",
		Args:     []string{"-C", "{{path}}", "log", "--oneline", "-{{n}}"},
		Defaults: map[string]string{"n": "5"},
	}
	s := newTestServer(nil, 0)

	// Default n=5.
	out1, err := s.execute(def, map[string]any{"path": dir})
	if err != nil {
		t.Fatal(err)
	}

	// Override n=1.
	out2, err := s.execute(def, map[string]any{"path": dir, "n": "1"})
	if err != nil {
		t.Fatal(err)
	}
	// Both should contain the commit but out2 might be shorter.
	if !strings.Contains(out1, "init commit") || !strings.Contains(out2, "init commit") {
		t.Errorf("unexpected output: %q / %q", out1, out2)
	}
}

// --- resolveConfigPath: positional arg ---

func TestResolveConfigPathArg(t *testing.T) {
	orig := os.Args
	t.Cleanup(func() { os.Args = orig })

	tmp := filepath.Join(t.TempDir(), "tools.json")
	if err := os.WriteFile(tmp, []byte("{}"), 0600); err != nil {
		t.Fatal(err)
	}
	os.Args = []string{"shell-mcp", tmp}

	got, err := resolveConfigPath()
	if err != nil {
		t.Fatalf("resolveConfigPath: %v", err)
	}
	if got != tmp {
		t.Errorf("got %q, want %q", got, tmp)
	}
}

// --- resolveConfigPath: env var ---

func TestResolveConfigPathEnv(t *testing.T) {
	orig := os.Args
	t.Cleanup(func() { os.Args = orig })
	os.Args = []string{"shell-mcp"}

	tmp := filepath.Join(t.TempDir(), "tools.json")
	if err := os.WriteFile(tmp, []byte("{}"), 0600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("SHELL_MCP_CONFIG", tmp)

	got, err := resolveConfigPath()
	if err != nil {
		t.Fatalf("resolveConfigPath: %v", err)
	}
	if got != tmp {
		t.Errorf("got %q, want %q", got, tmp)
	}
}

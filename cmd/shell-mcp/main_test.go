package main

import (
	"bytes"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
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

// --- execute: pattern-constrained arguments ---

func TestExecutePatternValidation(t *testing.T) {
	def := &toolDef{
		Name:     "echo_pr",
		Command:  "echo",
		Args:     []string{"{{pr_number}}"},
		Required: []string{"pr_number"},
		Patterns: map[string]string{"pr_number": "^[0-9]+$"},
	}
	s := newTestServer(nil, 0)

	// A real value passes and the command runs.
	out, err := s.execute(def, map[string]any{"pr_number": "1027"})
	if err != nil {
		t.Fatalf("valid value rejected: %v", err)
	}
	if !strings.Contains(out, "1027") {
		t.Errorf("output = %q, want it to contain 1027", out)
	}

	// A literal placeholder (a flaky model regurgitating its prompt
	// template) is rejected before the command runs.
	if _, err := s.execute(def, map[string]any{"pr_number": "<number>"}); err == nil {
		t.Error("placeholder value passed pattern validation, want rejection")
	}

	// A missing argument validates as "" and is rejected by an anchored pattern.
	if _, err := s.execute(def, map[string]any{}); err == nil {
		t.Error("missing value passed pattern validation, want rejection")
	}
}

// --- execute: per-tool output_cap_bytes override ---

// TestExecuteOutputCapBytesOverride verifies a tool with output_cap_bytes set
// uses its own cap instead of the server default, and a tool without the
// field still falls back to the server default (4000 bytes).
func TestExecuteOutputCapBytesOverride(t *testing.T) {
	// A 4200-byte output line (well past the 4000-byte server default) so
	// we can distinguish "capped at 4000" from "not capped".
	big := strings.Repeat("x", 4200)

	withOverride := &toolDef{
		Name:           "big_output_override",
		Command:        "echo",
		Args:           []string{"-n", big},
		OutputCapBytes: 8000,
	}
	withoutOverride := &toolDef{
		Name:    "big_output_default",
		Command: "echo",
		Args:    []string{"-n", big},
	}
	s := newTestServer(nil, 0)

	out, err := s.execute(withOverride, map[string]any{})
	if err != nil {
		t.Fatalf("execute withOverride: %v", err)
	}
	if strings.Contains(out, "[output capped]") {
		t.Errorf("output_cap_bytes=8000 tool was truncated at %d bytes, want untruncated: %q", len(out), out)
	}
	if len(out) != len(big) {
		t.Errorf("withOverride output len = %d, want %d (untruncated)", len(out), len(big))
	}

	out2, err := s.execute(withoutOverride, map[string]any{})
	if err != nil {
		t.Fatalf("execute withoutOverride: %v", err)
	}
	if !strings.Contains(out2, "[output capped]") {
		t.Errorf("tool without output_cap_bytes should still cap at server default, got: %q", out2)
	}
}

// --- execute: required argument enforcement ---

// TestExecuteRequiredArgMissing verifies a call omitting a required argument
// is rejected before the command runs — regression coverage for required
// keys being advertised in the schema but never enforced, which let a call
// through with a literal "{{placeholder}}" substituted into the command.
func TestExecuteRequiredArgMissing(t *testing.T) {
	dir := t.TempDir()
	sentinel := filepath.Join(dir, "ran")
	def := &toolDef{
		Name:     "touch_sentinel",
		Command:  "sh",
		Args:     []string{"-c", "touch " + sentinel + "; echo {{who}}"},
		Required: []string{"who"},
	}
	s := newTestServer(nil, 0)

	out, err := s.execute(def, map[string]any{})
	if err == nil {
		t.Fatalf("expected error for missing required arg, got output: %q", out)
	}
	if !strings.Contains(err.Error(), "who") {
		t.Errorf("error = %q, want it to name the missing key %q", err.Error(), "who")
	}
	if _, statErr := os.Stat(sentinel); statErr == nil {
		t.Error("command executed despite missing required argument (sentinel file was created)")
	}

	// Supplying the required arg lets it run.
	out2, err := s.execute(def, map[string]any{"who": "world"})
	if err != nil {
		t.Fatalf("execute with required arg supplied: %v", err)
	}
	if !strings.Contains(out2, "world") {
		t.Errorf("output = %q, want it to contain %q", out2, "world")
	}
	if _, statErr := os.Stat(sentinel); statErr != nil {
		t.Error("command did not execute despite required argument being supplied")
	}
}

// TestHandleCallRequiredArgMissingIsError verifies the missing-required-arg
// rejection surfaces through tools/call with the same isError:true shape
// used for other execution failures.
func TestHandleCallRequiredArgMissingIsError(t *testing.T) {
	tools := []toolDef{
		{Name: "needs_arg", Command: "echo", Args: []string{"{{who}}"}, Required: []string{"who"}},
	}
	s := newTestServer(tools, 0)
	id := int64(20)
	resp := s.dispatch(rpcRequest{
		JSONRPC: "2.0", ID: &id, Method: "tools/call",
		Params: mustJSON(t, map[string]any{"name": "needs_arg", "arguments": map[string]any{}}),
	})
	if resp.Error != nil {
		t.Fatalf("unexpected JSON-RPC error: %v", resp.Error.Message)
	}
	b, _ := json.Marshal(resp.Result)
	var result map[string]any
	if err := json.Unmarshal(b, &result); err != nil {
		t.Fatalf("unmarshal result: %v", err)
	}
	if isErr, ok := result["isError"].(bool); !ok || !isErr {
		t.Fatalf("expected isError: true, got result: %s", b)
	}
	content := result["content"].([]any)
	text := content[0].(map[string]any)["text"].(string)
	if !strings.HasPrefix(text, "error: ") || !strings.Contains(text, "who") {
		t.Errorf("text = %q, want error: prefix naming missing key %q", text, "who")
	}
}

func TestToolListAdvertisesPatterns(t *testing.T) {
	s := newTestServer([]toolDef{{
		Name:     "post_pr_comment",
		Command:  "echo",
		Args:     []string{"{{pr_number}}"},
		Required: []string{"pr_number"},
		Patterns: map[string]string{"pr_number": "^[0-9]+$"},
	}}, 0)
	tools := s.toolList()
	if len(tools) != 1 {
		t.Fatalf("toolList len = %d, want 1", len(tools))
	}
	schema := tools[0]["inputSchema"].(map[string]any)
	props := schema["properties"].(map[string]any)
	prop, ok := props["pr_number"].(map[string]any)
	if !ok {
		t.Fatal("pr_number property missing from inputSchema")
	}
	if prop["pattern"] != "^[0-9]+$" {
		t.Errorf("pr_number pattern = %v, want ^[0-9]+$", prop["pattern"])
	}
}

func TestExampleShellToolsJSON_PatternsCompileAndAreDeclared(t *testing.T) {
	matches, err := filepath.Glob("../../examples/*/shell-tools.json")
	if err != nil {
		t.Fatalf("glob: %v", err)
	}
	for _, path := range matches {
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("%s: read: %v", path, err)
		}
		var cfg config
		if err := json.Unmarshal(data, &cfg); err != nil {
			t.Fatalf("%s: unmarshal: %v", path, err)
		}
		for _, tool := range cfg.Tools {
			declared := make(map[string]bool, len(tool.Required)+len(tool.Defaults))
			for _, r := range tool.Required {
				declared[r] = true
			}
			for k := range tool.Defaults {
				declared[k] = true
			}
			for key, pat := range tool.Patterns {
				if _, err := regexp.Compile(pat); err != nil {
					t.Errorf("%s: tool %q pattern for %s does not compile: %v", path, tool.Name, key, err)
				}
				if !declared[key] {
					t.Errorf("%s: tool %q constrains {{%s}} with a pattern but does not declare it in required or defaults", path, tool.Name, key)
				}
			}
		}
	}
}

// --- handleCall: isError wire format ---

// TestHandleCallSetsIsErrorOnFailure verifies that a failing command sets
// isError: true in the MCP response, with the error: prefix preserved in the
// text content. Regression coverage for the 2026-07-06 incident: a failed
// exec that only surfaced as "error: ..." text (no isError) was treated as a
// successful call further up the stack.
func TestHandleCallSetsIsErrorOnFailure(t *testing.T) {
	tools := []toolDef{
		{Name: "fail_tool", Command: "false"},
	}
	s := newTestServer(tools, 0)
	id := int64(10)
	resp := s.dispatch(rpcRequest{
		JSONRPC: "2.0", ID: &id, Method: "tools/call",
		Params: mustJSON(t, map[string]any{"name": "fail_tool", "arguments": map[string]any{}}),
	})
	if resp.Error != nil {
		t.Fatalf("unexpected JSON-RPC error: %v", resp.Error.Message)
	}
	b, _ := json.Marshal(resp.Result)
	var result map[string]any
	if err := json.Unmarshal(b, &result); err != nil {
		t.Fatalf("unmarshal result: %v", err)
	}
	isErr, ok := result["isError"].(bool)
	if !ok || !isErr {
		t.Fatalf("expected isError: true, got result: %s", b)
	}
	content := result["content"].([]any)
	text := content[0].(map[string]any)["text"].(string)
	if !strings.HasPrefix(text, "error: ") {
		t.Errorf("text = %q, want error: prefix", text)
	}
}

// TestHandleCallSetsIsErrorOnStderrExit verifies a command that exits
// nonzero with stderr output also gets isError: true and the exit-code text.
func TestHandleCallSetsIsErrorOnStderrExit(t *testing.T) {
	tools := []toolDef{
		{Name: "stderr_tool", Command: "sh", Args: []string{"-c", "echo boom >&2; exit 2"}},
	}
	s := newTestServer(tools, 0)
	id := int64(11)
	resp := s.dispatch(rpcRequest{
		JSONRPC: "2.0", ID: &id, Method: "tools/call",
		Params: mustJSON(t, map[string]any{"name": "stderr_tool", "arguments": map[string]any{}}),
	})
	b, _ := json.Marshal(resp.Result)
	var result map[string]any
	if err := json.Unmarshal(b, &result); err != nil {
		t.Fatalf("unmarshal result: %v", err)
	}
	if isErr, ok := result["isError"].(bool); !ok || !isErr {
		t.Fatalf("expected isError: true, got result: %s", b)
	}
	content := result["content"].([]any)
	text := content[0].(map[string]any)["text"].(string)
	if !strings.Contains(text, "error: exit 2:") || !strings.Contains(text, "boom") {
		t.Errorf("text = %q, want error: exit 2: ... boom", text)
	}
}

// TestHandleCallNoIsErrorOnSuccess verifies a successful call carries no
// isError key at all (not even isError: false), preserving byte-identical
// behavior for clients that only check for the key's presence.
func TestHandleCallNoIsErrorOnSuccess(t *testing.T) {
	tools := []toolDef{
		{Name: "ok_tool", Command: "echo", Args: []string{"hi"}},
	}
	s := newTestServer(tools, 0)
	id := int64(12)
	resp := s.dispatch(rpcRequest{
		JSONRPC: "2.0", ID: &id, Method: "tools/call",
		Params: mustJSON(t, map[string]any{"name": "ok_tool", "arguments": map[string]any{}}),
	})
	b, _ := json.Marshal(resp.Result)
	var result map[string]any
	if err := json.Unmarshal(b, &result); err != nil {
		t.Fatalf("unmarshal result: %v", err)
	}
	if _, present := result["isError"]; present {
		t.Errorf("isError key present on success: %s", b)
	}
}

func mustJSON(t *testing.T, v any) json.RawMessage {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	return b
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

// --- example shell-tools.json schema consistency ---

var placeholderRE = regexp.MustCompile(`\{\{(\w+)\}\}`)

// TestExampleShellToolsJSON_PlaceholdersDeclared walks every
// examples/*/shell-tools.json and asserts that each {{key}} placeholder
// used in a tool's args is declared in required or defaults. toolList
// builds inputSchema.properties only from those two fields — a tool with
// neither leaves the model with an empty schema, so it never learns to
// supply the argument, and the placeholder passes through unsubstituted
// into the shell command at call time. Regression test for the bug found
// via examples/11-high-volume-ci's shell-tools.json (unsubstituted
// {{pr_number}} produced a literal, unparseable arithmetic expression).
func TestExampleShellToolsJSON_PlaceholdersDeclared(t *testing.T) {
	matches, err := filepath.Glob("../../examples/*/shell-tools.json")
	if err != nil {
		t.Fatalf("glob: %v", err)
	}
	if len(matches) == 0 {
		t.Fatal("no examples/*/shell-tools.json files found — glob path likely wrong")
	}
	for _, path := range matches {
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("%s: read: %v", path, err)
		}
		var cfg config
		if err := json.Unmarshal(data, &cfg); err != nil {
			t.Fatalf("%s: unmarshal: %v", path, err)
		}
		for _, tool := range cfg.Tools {
			declared := make(map[string]bool, len(tool.Required)+len(tool.Defaults))
			for _, r := range tool.Required {
				declared[r] = true
			}
			for k := range tool.Defaults {
				declared[k] = true
			}
			seen := make(map[string]bool)
			for _, arg := range tool.Args {
				for _, m := range placeholderRE.FindAllStringSubmatch(arg, -1) {
					key := m[1]
					if seen[key] {
						continue
					}
					seen[key] = true
					if !declared[key] {
						t.Errorf("%s: tool %q uses {{%s}} but does not declare it in required or defaults — the model will never learn to supply it", path, tool.Name, key)
					}
				}
			}
		}
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

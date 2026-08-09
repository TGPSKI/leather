package tool

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// writeToolDir writes the named files into a fresh temp dir and returns it.
func writeToolDir(t *testing.T, files map[string]string) string {
	t.Helper()
	dir := t.TempDir()
	for name, body := range files {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0600); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

// --- flow-style mappings (issue #74) ---

func TestLoad_FlowStyleMCPMapping(t *testing.T) {
	dir := writeToolDir(t, map[string]string{
		"report.skill.yaml": `name: report
tools:
  - name: report-write
    description: Write the report.
    type: mcp
    mcp: { server: shells, tool: catnip_report_write }
`,
	})
	reg, err := Load(dir)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	td, ok := reg.GetTool("report-write")
	if !ok {
		t.Fatal("report-write not registered")
	}
	if td.MCP.Server != "shells" || td.MCP.Tool != "catnip_report_write" {
		t.Errorf("mcp = %+v, want server=shells tool=catnip_report_write", td.MCP)
	}
}

func TestLoad_FlowStyleMatchesBlockStyle(t *testing.T) {
	flow, err := Load(writeToolDir(t, map[string]string{
		"a.skill.yaml": "name: a\ntools:\n  - name: t\n    type: mcp\n    mcp: { server: s, tool: u }\n",
	}))
	if err != nil {
		t.Fatalf("flow Load: %v", err)
	}
	block, err := Load(writeToolDir(t, map[string]string{
		"a.skill.yaml": "name: a\ntools:\n  - name: t\n    type: mcp\n    mcp:\n      server: s\n      tool: u\n",
	}))
	if err != nil {
		t.Fatalf("block Load: %v", err)
	}
	ft, _ := flow.GetTool("t")
	bt, _ := block.GetTool("t")
	if ft.MCP != bt.MCP {
		t.Errorf("flow %+v != block %+v", ft.MCP, bt.MCP)
	}
}

func TestLoad_FlowStyleHTTPMapping(t *testing.T) {
	reg, err := Load(writeToolDir(t, map[string]string{
		"web.skill.yaml": "name: web\ntools:\n  - name: fetch\n    type: http\n    http: { method: get, url: https://example.test/x, headers: { Accept: application/json } }\n",
	}))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	td, _ := reg.GetTool("fetch")
	if td.HTTP.Method != "GET" || td.HTTP.URL != "https://example.test/x" {
		t.Errorf("http = %+v", td.HTTP)
	}
	if td.HTTP.Headers["Accept"] != "application/json" {
		t.Errorf("headers = %+v", td.HTTP.Headers)
	}
}

func TestLoad_MCPToolMissingServerFails(t *testing.T) {
	for name, body := range map[string]string{
		"missing server": "name: a\ntools:\n  - name: t\n    type: mcp\n    mcp: { tool: u }\n",
		"missing tool":   "name: a\ntools:\n  - name: t\n    type: mcp\n    mcp: { server: s }\n",
		"no mcp block":   "name: a\ntools:\n  - name: t\n    type: mcp\n",
		"typo'd key":     "name: a\ntools:\n  - name: t\n    type: mcp\n    mcp: { srv: s, tool: u }\n",
	} {
		t.Run(name, func(t *testing.T) {
			_, err := Load(writeToolDir(t, map[string]string{"a.skill.yaml": body}))
			if err == nil {
				t.Fatal("Load succeeded; want an error naming the incomplete mcp tool")
			}
			if !strings.Contains(err.Error(), "mcp.server") {
				t.Errorf("error %q does not say what is missing", err)
			}
		})
	}
}

// --- toolset shape (issue #73) ---

func TestLoad_ToolsetWithToolDefinitionsFails(t *testing.T) {
	// The shape docs/GUIDE.md used to show. It parsed as a tool literally named
	// "name: list-tags" and resolved to nothing at run time.
	_, err := Load(writeToolDir(t, map[string]string{
		"release-read.toolset.yaml": `name: release-read
tools:
  - name: list-tags
    description: List all git tags.
    type: mcp
    mcp:
      server: shell
      tool: list-tags
`,
	}))
	if err == nil {
		t.Fatal("Load succeeded on a toolset of tool definitions; want an error")
	}
	if !strings.Contains(err.Error(), "release-read.toolset.yaml") {
		t.Errorf("error %q does not name the file", err)
	}
	if !strings.Contains(err.Error(), "tool name") {
		t.Errorf("error %q does not explain the required form", err)
	}
}

func TestLoad_ToolsetNameListForms(t *testing.T) {
	files := map[string]string{
		"defs.skill.yaml":    "name: defs\ntools:\n  - name: list-tags\n    type: mcp\n    mcp: { server: s, tool: list_tags }\n  - name: log-since\n    type: mcp\n    mcp: { server: s, tool: log_since }\n",
		"flow.toolset.yaml":  "name: flow\ntools: [list-tags, log-since]\n",
		"block.toolset.yaml": "name: block\ntools:\n  - list-tags\n  - log-since\n",
	}
	reg, err := Load(writeToolDir(t, files))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	for _, name := range []string{"flow", "block"} {
		if got := len(reg.GetToolsetTools([]string{name})); got != 2 {
			t.Errorf("toolset %q resolved %d tools, want 2", name, got)
		}
	}
}

func TestLoad_EmptyToolsetFails(t *testing.T) {
	_, err := Load(writeToolDir(t, map[string]string{
		"empty.toolset.yaml": "name: empty\ndescription: nothing here\n",
	}))
	if err == nil {
		t.Fatal("Load succeeded on a toolset naming no tools; want an error")
	}
}

// --- duplicate names still abort the load (issue #71) ---

func TestLoad_DuplicateToolNameAcrossSkillsFails(t *testing.T) {
	_, err := Load(writeToolDir(t, map[string]string{
		"one.skill.yaml": "name: one\ntools:\n  - name: shared\n    type: mcp\n    mcp: { server: s, tool: a }\n",
		"two.skill.yaml": "name: two\ntools:\n  - name: shared\n    type: mcp\n    mcp: { server: s, tool: b }\n",
	}))
	if err == nil {
		t.Fatal("Load succeeded with a duplicate tool name across skills")
	}
	if !strings.Contains(err.Error(), "shared") {
		t.Errorf("error %q does not name the colliding tool", err)
	}
}

// --- CheckScope (issue #72) ---

func TestCheckScope(t *testing.T) {
	reg, err := Load(writeToolDir(t, map[string]string{
		"repo.skill.yaml":   "name: repo\ntools:\n  - name: git-status\n    type: mcp\n    mcp: { server: s, tool: git_status }\n",
		"read.toolset.yaml": "name: read\ntools: [git-status]\n",
	}))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	tests := []struct {
		name                    string
		skills, toolsets, tools []string
		wantErr                 bool
		wantSubstr              string
	}{
		{name: "empty scope is fine"},
		{name: "all resolve", skills: []string{"repo"}, toolsets: []string{"read"}, tools: []string{"git-status"}},
		{name: "unknown skill", skills: []string{"nope"}, wantErr: true, wantSubstr: `unknown skill "nope"`},
		{name: "unknown toolset", toolsets: []string{"nope"}, wantErr: true, wantSubstr: `unknown toolset "nope"`},
		{name: "unknown tool", tools: []string{"nope"}, wantErr: true, wantSubstr: `unknown tool "nope"`},
		// The one-edit-from-correct case: a skill listed under toolsets:.
		{name: "skill named as toolset", toolsets: []string{"repo"}, wantErr: true, wantSubstr: "loaded under skills:"},
		{name: "toolset named as skill", skills: []string{"read"}, wantErr: true, wantSubstr: "loaded under toolsets:"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := reg.CheckScope(tc.skills, tc.toolsets, tc.tools)
			if tc.wantErr != (err != nil) {
				t.Fatalf("CheckScope err = %v, wantErr = %v", err, tc.wantErr)
			}
			if tc.wantSubstr != "" && !strings.Contains(err.Error(), tc.wantSubstr) {
				t.Errorf("error %q does not contain %q", err, tc.wantSubstr)
			}
		})
	}
}

func TestCheckScope_NilRegistry(t *testing.T) {
	var reg *Registry
	if err := reg.CheckScope(nil, nil, nil); err != nil {
		t.Errorf("empty scope against a nil registry = %v, want nil", err)
	}
	if err := reg.CheckScope([]string{"repo"}, nil, nil); err == nil {
		t.Error("named skill against a nil registry succeeded; want an error")
	}
}

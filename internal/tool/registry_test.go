package tool

import (
	"os"
	"strings"
	"testing"

	"github.com/tgpski/leather/internal/model"
)

func mustParseSkill(t *testing.T, src string) model.Skill {
	t.Helper()
	s, err := parseSkillYAML(strings.TrimSpace(src))
	if err != nil {
		t.Fatalf("mustParseSkill: %v", err)
	}
	return s
}

func mustParseToolset(t *testing.T, src string) model.Toolset {
	t.Helper()
	s, err := parseToolsetYAML(strings.TrimSpace(src))
	if err != nil {
		t.Fatalf("mustParseToolset: %v", err)
	}
	return s
}

func TestParseSkillYAML_BasicFields(t *testing.T) {
	src := `
name: test-skill
description: A test skill
system_prompt_append: |
  Use these tools wisely.
tools:
  - name: fetch_data
    description: Fetches data from an endpoint
    type: http
    http:
      method: GET
      url: https://api.example.com/data
      headers:
        Authorization: Bearer {{env:API_TOKEN}}
      query:
        format: json
`
	skill, err := parseSkillYAML(strings.TrimSpace(src))
	if err != nil {
		t.Fatalf("parseSkillYAML: %v", err)
	}
	if skill.Name != "test-skill" {
		t.Errorf("Name = %q, want %q", skill.Name, "test-skill")
	}
	if skill.Description != "A test skill" {
		t.Errorf("Description = %q, want %q", skill.Description, "A test skill")
	}
	if !strings.Contains(skill.SystemPromptAppend, "Use these tools wisely.") {
		t.Errorf("SystemPromptAppend = %q, want contains 'Use these tools wisely.'", skill.SystemPromptAppend)
	}
	if len(skill.Tools) != 1 {
		t.Fatalf("len(Tools) = %d, want 1", len(skill.Tools))
	}
	td := skill.Tools[0]
	if td.Name != "fetch_data" {
		t.Errorf("Tool.Name = %q, want %q", td.Name, "fetch_data")
	}
	if td.HTTP.Method != "GET" {
		t.Errorf("HTTP.Method = %q, want GET", td.HTTP.Method)
	}
	if td.HTTP.URL != "https://api.example.com/data" {
		t.Errorf("HTTP.URL = %q", td.HTTP.URL)
	}
	if td.HTTP.Headers["Authorization"] != "Bearer {{env:API_TOKEN}}" {
		t.Errorf("HTTP.Headers[Authorization] = %q", td.HTTP.Headers["Authorization"])
	}
	if td.HTTP.Query["format"] != "json" {
		t.Errorf("HTTP.Query[format] = %q", td.HTTP.Query["format"])
	}
}

func TestParseSkillYAML_MultipleTools(t *testing.T) {
	src := `
name: multi
tools:
  - name: tool_a
    type: http
    http:
      method: POST
      url: https://api.example.com/a
      body:
        key: value
  - name: tool_b
    type: http
    http:
      method: DELETE
      url: https://api.example.com/b
`
	skill, err := parseSkillYAML(strings.TrimSpace(src))
	if err != nil {
		t.Fatalf("parseSkillYAML: %v", err)
	}
	if len(skill.Tools) != 2 {
		t.Fatalf("len(Tools) = %d, want 2", len(skill.Tools))
	}
	if skill.Tools[0].Name != "tool_a" {
		t.Errorf("Tools[0].Name = %q", skill.Tools[0].Name)
	}
	if skill.Tools[0].HTTP.Method != "POST" {
		t.Errorf("Tools[0].HTTP.Method = %q", skill.Tools[0].HTTP.Method)
	}
	if skill.Tools[0].HTTP.Body["key"] != "value" {
		t.Errorf("Tools[0].HTTP.Body[key] = %q", skill.Tools[0].HTTP.Body["key"])
	}
	if skill.Tools[1].Name != "tool_b" {
		t.Errorf("Tools[1].Name = %q", skill.Tools[1].Name)
	}
	if skill.Tools[1].HTTP.Method != "DELETE" {
		t.Errorf("Tools[1].HTTP.Method = %q", skill.Tools[1].HTTP.Method)
	}
}

func TestExpandTemplate(t *testing.T) {
	cases := []struct {
		name    string
		tmpl    string
		args    map[string]any
		want    string
		wantErr bool
	}{
		{
			name: "no placeholders",
			tmpl: "https://api.example.com/data",
			args: nil,
			want: "https://api.example.com/data",
		},
		{
			name: "arg substitution",
			tmpl: "https://api.example.com/repos/{{.owner}}/{{.repo}}",
			args: map[string]any{"owner": "acme", "repo": "myrepo"},
			want: "https://api.example.com/repos/acme/myrepo",
		},
		{
			name: "missing arg is empty",
			tmpl: "https://api.example.com/{{.missing}}",
			args: map[string]any{},
			want: "https://api.example.com/",
		},
		{
			name:    "unclosed brace",
			tmpl:    "{{unclosed",
			args:    nil,
			wantErr: true,
		},
		{
			name:    "unknown expression",
			tmpl:    "{{unknown}}",
			args:    nil,
			wantErr: true,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := expandTemplate(tc.tmpl, tc.args, nil)
			if (err != nil) != tc.wantErr {
				t.Fatalf("err = %v, wantErr = %v", err, tc.wantErr)
			}
			if !tc.wantErr && got != tc.want {
				t.Errorf("got %q, want %q", got, tc.want)
			}
		})
	}
}

func TestRegistry_GetTools(t *testing.T) {
	r := NewRegistry()

	skill1 := mustParseSkill(t, `
name: skill1
tools:
  - name: tool_a
    type: http
    http:
      method: GET
      url: https://a.example.com
`)
	skill2 := mustParseSkill(t, `
name: skill2
tools:
  - name: tool_b
    type: http
    http:
      method: POST
      url: https://b.example.com
`)
	if err := r.Register(skill1); err != nil {
		t.Fatal(err)
	}
	if err := r.Register(skill2); err != nil {
		t.Fatal(err)
	}

	tools := r.GetTools([]string{"skill1", "skill2"})
	if len(tools) != 2 {
		t.Fatalf("GetTools = %d tools, want 2", len(tools))
	}
	if tools[0].Name != "tool_a" || tools[1].Name != "tool_b" {
		t.Errorf("unexpected tool names: %v", []string{tools[0].Name, tools[1].Name})
	}

	// Unknown skill is silently skipped.
	tools2 := r.GetTools([]string{"unknown"})
	if len(tools2) != 0 {
		t.Errorf("GetTools(unknown) = %d, want 0", len(tools2))
	}
}

func TestParseToolsetYAML_BasicFields(t *testing.T) {
	set := mustParseToolset(t, `
name: release-read
description: Read-only release checks
tools: [git_status, git_log_since, list_tags]
`)
	if set.Name != "release-read" {
		t.Errorf("Name = %q, want release-read", set.Name)
	}
	if set.Description != "Read-only release checks" {
		t.Errorf("Description = %q", set.Description)
	}
	if len(set.Tools) != 3 {
		t.Fatalf("len(Tools) = %d, want 3", len(set.Tools))
	}
	if set.Tools[0] != "git_status" || set.Tools[2] != "list_tags" {
		t.Errorf("Tools = %v", set.Tools)
	}
}

func TestRegistry_RegisterToolsetAndResolveTools(t *testing.T) {
	r := NewRegistry()
	skill := mustParseSkill(t, `
name: release-skill
tools:
  - name: git_status
    type: http
    http:
      method: GET
      url: https://example.com/status
  - name: git_log_since
    type: http
    http:
      method: GET
      url: https://example.com/log
  - name: list_tags
    type: http
    http:
      method: GET
      url: https://example.com/tags
`)
	if err := r.Register(skill); err != nil {
		t.Fatal(err)
	}
	if err := r.RegisterToolset(model.Toolset{Name: "release-read", Tools: []string{"git_status", "list_tags"}}); err != nil {
		t.Fatal(err)
	}
	resolved := r.ResolveTools([]string{"release-skill"}, []string{"release-read"}, []string{"git_status"})
	if len(resolved) != 3 {
		t.Fatalf("ResolveTools len = %d, want 3", len(resolved))
	}
	if resolved[0].Name != "git_status" || resolved[1].Name != "git_log_since" || resolved[2].Name != "list_tags" {
		t.Errorf("ResolveTools order = [%s %s %s]", resolved[0].Name, resolved[1].Name, resolved[2].Name)
	}
}

func TestLoad_ToolsetFile(t *testing.T) {
	dir := t.TempDir()
	skillYAML := `
name: loaded-skill
tools:
  - name: git_status
    type: http
    http:
      method: GET
      url: https://example.com/status
  - name: git_log_since
    type: http
    http:
      method: GET
      url: https://example.com/log
`
	toolsetYAML := `
name: release-read
description: Release read scope
tools: [git_status, git_log_since]
`
	if err := os.WriteFile(dir+"/loaded.skill.yaml", []byte(skillYAML), 0600); err != nil {
		t.Fatalf("WriteFile skill: %v", err)
	}
	if err := os.WriteFile(dir+"/release-read.toolset.yaml", []byte(toolsetYAML), 0600); err != nil {
		t.Fatalf("WriteFile toolset: %v", err)
	}
	r, err := Load(dir)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	set, ok := r.GetToolset("release-read")
	if !ok {
		t.Fatal("GetToolset: release-read not found after Load")
	}
	if len(set.Tools) != 2 {
		t.Fatalf("GetToolset tools = %d, want 2", len(set.Tools))
	}
	tools := r.GetToolsetTools([]string{"release-read"})
	if len(tools) != 2 {
		t.Fatalf("GetToolsetTools len = %d, want 2", len(tools))
	}
	if tools[0].Name != "git_status" || tools[1].Name != "git_log_since" {
		t.Errorf("GetToolsetTools = [%s %s]", tools[0].Name, tools[1].Name)
	}
}

func TestRegistry_DuplicateToolRejected(t *testing.T) {
	r := NewRegistry()
	skill1 := mustParseSkill(t, `
name: s1
tools:
  - name: dupe_tool
    type: http
    http:
      method: GET
      url: https://a.example.com
`)
	skill2 := mustParseSkill(t, `
name: s2
tools:
  - name: dupe_tool
    type: http
    http:
      method: GET
      url: https://b.example.com
`)
	if err := r.Register(skill1); err != nil {
		t.Fatal(err)
	}
	if err := r.Register(skill2); err == nil {
		t.Error("expected error for duplicate tool name, got nil")
	}
}

func TestRegistry_GetTool(t *testing.T) {
	r := NewRegistry()
	skill := mustParseSkill(t, `
name: sk
tools:
  - name: my_tool
    type: http
    http:
      method: GET
      url: https://example.com
`)
	if err := r.Register(skill); err != nil {
		t.Fatal(err)
	}

	td, ok := r.GetTool("my_tool")
	if !ok {
		t.Fatal("GetTool: expected true for registered tool")
	}
	if td.Name != "my_tool" {
		t.Errorf("GetTool name = %q, want %q", td.Name, "my_tool")
	}

	_, ok2 := r.GetTool("not_registered")
	if ok2 {
		t.Error("GetTool: expected false for unregistered tool")
	}
}

func TestRegistry_GetSkills(t *testing.T) {
	r := NewRegistry()
	s1 := mustParseSkill(t, `
name: alpha
system_prompt_append: "Use alpha wisely."
tools:
  - name: alpha_tool
    type: http
    http:
      method: GET
      url: https://alpha.example.com
`)
	s2 := mustParseSkill(t, `
name: beta
system_prompt_append: "Use beta wisely."
tools:
  - name: beta_tool
    type: http
    http:
      method: POST
      url: https://beta.example.com
`)
	if err := r.Register(s1); err != nil {
		t.Fatal(err)
	}
	if err := r.Register(s2); err != nil {
		t.Fatal(err)
	}

	skills := r.GetSkills([]string{"alpha", "beta"})
	if len(skills) != 2 {
		t.Fatalf("GetSkills len = %d, want 2", len(skills))
	}
	if skills[0].Name != "alpha" {
		t.Errorf("skills[0].Name = %q, want alpha", skills[0].Name)
	}
	if !strings.Contains(skills[0].SystemPromptAppend, "alpha wisely") {
		t.Errorf("skills[0].SystemPromptAppend = %q", skills[0].SystemPromptAppend)
	}

	// Unknown skill name silently skipped.
	onlyAlpha := r.GetSkills([]string{"alpha", "unknown"})
	if len(onlyAlpha) != 1 {
		t.Errorf("GetSkills with unknown: len = %d, want 1", len(onlyAlpha))
	}
}

func TestLoad_NonExistentDir(t *testing.T) {
	r, err := Load(t.TempDir() + "/nonexistent")
	if err != nil {
		t.Fatalf("Load of non-existent dir: %v", err)
	}
	if r == nil {
		t.Fatal("Load returned nil registry")
	}
}

func TestLoad_EmptyDir(t *testing.T) {
	dir := t.TempDir()
	r, err := Load(dir)
	if err != nil {
		t.Fatalf("Load of empty dir: %v", err)
	}
	if tools := r.GetTools(nil); len(tools) != 0 {
		t.Errorf("GetTools on empty registry: len = %d, want 0", len(tools))
	}
}

func TestLoad_SingleSkill(t *testing.T) {
	dir := t.TempDir()
	skillYAML := `
name: loaded-skill
system_prompt_append: "Loaded."
tools:
  - name: loaded_tool
    type: http
    http:
      method: GET
      url: https://loaded.example.com
`
	if err := os.WriteFile(dir+"/loaded.skill.yaml", []byte(skillYAML), 0600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	r, err := Load(dir)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	td, ok := r.GetTool("loaded_tool")
	if !ok {
		t.Fatal("GetTool: loaded_tool not found after Load")
	}
	if td.HTTP.URL != "https://loaded.example.com" {
		t.Errorf("tool URL = %q", td.HTTP.URL)
	}
}

func TestLoad_EmptyString(t *testing.T) {
	r, err := Load("")
	if err != nil {
		t.Fatalf("Load of empty string: %v", err)
	}
	if r == nil {
		t.Fatal("Load returned nil")
	}
}

// --- extract: parsing and ApplyExtractors tests ---

func TestParseSkillYAML_ExtractBlock(t *testing.T) {
	src := `
name: extract-skill
extract:
  - tool: my_tool
    pattern: "^AUTHOR: (.+)$"
    store: pr_author
  - tool: my_tool
    pattern: "^PR: (.+)$"
    store: pr_title
tools:
  - name: my_tool
    description: a tool
    type: http
    http:
      method: GET
      url: https://example.com
`
	skill, err := parseSkillYAML(strings.TrimSpace(src))
	if err != nil {
		t.Fatalf("parseSkillYAML: %v", err)
	}
	if len(skill.Extract) != 2 {
		t.Fatalf("len(Extract) = %d, want 2", len(skill.Extract))
	}
	if skill.Extract[0].Tool != "my_tool" {
		t.Errorf("Extract[0].Tool = %q, want my_tool", skill.Extract[0].Tool)
	}
	if skill.Extract[0].Pattern != "^AUTHOR: (.+)$" {
		t.Errorf("Extract[0].Pattern = %q", skill.Extract[0].Pattern)
	}
	if skill.Extract[0].Store != "pr_author" {
		t.Errorf("Extract[0].Store = %q", skill.Extract[0].Store)
	}
	if skill.Extract[1].Store != "pr_title" {
		t.Errorf("Extract[1].Store = %q", skill.Extract[1].Store)
	}
}

func TestRegistry_ExtractorCompileError(t *testing.T) {
	reg := NewRegistry()
	skill := model.Skill{
		Name: "bad-skill",
		Extract: []model.SkillExtract{
			{Tool: "t", Pattern: "[invalid(", Store: "x"},
		},
	}
	err := reg.Register(skill)
	if err == nil {
		t.Fatal("Register: expected error for invalid regex, got nil")
	}
}

func TestRegistry_ApplyExtractors(t *testing.T) {
	reg := NewRegistry()
	skill := model.Skill{
		Name: "extract-skill",
		Extract: []model.SkillExtract{
			{Tool: "gh_pr_thread", Pattern: `^AUTHOR: (.+)$`, Store: "pr_author"},
			{Tool: "gh_pr_thread", Pattern: `^PR: (.+)$`, Store: "pr_title"},
		},
	}
	if err := reg.Register(skill); err != nil {
		t.Fatalf("Register: %v", err)
	}

	content := "PR: Fix the bug\nAUTHOR: octocat\nSTATE: open\n"
	vars := make(map[string]string)
	reg.ApplyExtractors("gh_pr_thread", content, vars)

	if vars["pr_author"] != "octocat" {
		t.Errorf("pr_author = %q, want octocat", vars["pr_author"])
	}
	if vars["pr_title"] != "Fix the bug" {
		t.Errorf("pr_title = %q, want 'Fix the bug'", vars["pr_title"])
	}
}

func TestRegistry_ApplyExtractors_NoMatch(t *testing.T) {
	reg := NewRegistry()
	skill := model.Skill{
		Name: "extract-skill",
		Extract: []model.SkillExtract{
			{Tool: "gh_pr_thread", Pattern: `^AUTHOR: (.+)$`, Store: "pr_author"},
		},
	}
	if err := reg.Register(skill); err != nil {
		t.Fatalf("Register: %v", err)
	}

	vars := map[string]string{"pr_author": "original"}
	reg.ApplyExtractors("gh_pr_thread", "no match here", vars)

	// No match — existing value must be preserved.
	if vars["pr_author"] != "original" {
		t.Errorf("pr_author = %q, want original (no match should be a noop)", vars["pr_author"])
	}
}

func TestRegistry_ApplyExtractors_WrongTool(t *testing.T) {
	reg := NewRegistry()
	skill := model.Skill{
		Name: "extract-skill",
		Extract: []model.SkillExtract{
			{Tool: "gh_pr_thread", Pattern: `^AUTHOR: (.+)$`, Store: "pr_author"},
		},
	}
	if err := reg.Register(skill); err != nil {
		t.Fatalf("Register: %v", err)
	}

	vars := make(map[string]string)
	reg.ApplyExtractors("other_tool", "AUTHOR: octocat", vars)

	if _, ok := vars["pr_author"]; ok {
		t.Error("pr_author should not be set for wrong tool name")
	}
}

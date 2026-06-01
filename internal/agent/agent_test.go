package agent

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/tgpski/leather/internal/model"
)

// makeValidAgent returns a minimal valid model.Agent for use in tests.
func makeValidAgent() model.Agent {
	return model.Agent{
		Name:        "test-agent",
		Schedule:    "* * * * *",
		Model:       "llama3",
		Temperature: 0.7,
		Enabled:     true,
	}
}

// mustWrite writes content to path at 0600, failing the test on error.
func mustWrite(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0600); err != nil {
		t.Fatalf("mustWrite %s: %v", path, err)
	}
}

// --- parseFrontMatter ---

func TestParseFrontMatter_Full(t *testing.T) {
	src := `---
name: my-agent
schedule: "0 * * * *"
model: llama3
max_tokens: 4096
timeout: 45s
temperature: 0.5
enabled: true
tags: [daily, summary]
toolsets: [release-read, release-write]
---
This is the system prompt.
`
	fm, body, err := parseFrontMatter(src)
	if err != nil {
		t.Fatalf("parseFrontMatter: %v", err)
	}
	if fm.Name != "my-agent" {
		t.Errorf("Name = %q, want my-agent", fm.Name)
	}
	if fm.Schedule != "0 * * * *" {
		t.Errorf("Schedule = %q, want '0 * * * *'", fm.Schedule)
	}
	if fm.Model != "llama3" {
		t.Errorf("Model = %q, want llama3", fm.Model)
	}
	if fm.MaxTokens != 4096 {
		t.Errorf("MaxTokens = %d, want 4096", fm.MaxTokens)
	}
	if fm.Timeout != 45*time.Second {
		t.Errorf("Timeout = %v, want 45s", fm.Timeout)
	}
	if fm.Temperature != 0.5 {
		t.Errorf("Temperature = %v, want 0.5", fm.Temperature)
	}
	if !fm.Enabled {
		t.Error("Enabled = false, want true")
	}
	if len(fm.Tags) != 2 || fm.Tags[0] != "daily" || fm.Tags[1] != "summary" {
		t.Errorf("Tags = %v, want [daily summary]", fm.Tags)
	}
	if len(fm.Toolsets) != 2 || fm.Toolsets[0] != "release-read" || fm.Toolsets[1] != "release-write" {
		t.Errorf("Toolsets = %v, want [release-read release-write]", fm.Toolsets)
	}
	if body != "This is the system prompt." {
		t.Errorf("body = %q, want 'This is the system prompt.'", body)
	}
}

func TestSplitAgentBody_TurnDeclarations(t *testing.T) {
	body := `System prompt.
---
skills: [shell-git]
toolsets: [release-read]
tools: [git_status]
Check repo state.
---
toolsets: [release-write]
Create the tag.`
	sysPrompt, prompts, tools, skills, toolsets := splitAgentBody(body)
	if sysPrompt != "System prompt." {
		t.Fatalf("sysPrompt = %q", sysPrompt)
	}
	if len(prompts) != 2 {
		t.Fatalf("len(prompts) = %d, want 2", len(prompts))
	}
	if prompts[0] != "Check repo state." || prompts[1] != "Create the tag." {
		t.Errorf("prompts = %v", prompts)
	}
	if len(skills[0]) != 1 || skills[0][0] != "shell-git" {
		t.Errorf("skills[0] = %v", skills[0])
	}
	if len(toolsets[0]) != 1 || toolsets[0][0] != "release-read" {
		t.Errorf("toolsets[0] = %v", toolsets[0])
	}
	if len(tools[0]) != 1 || tools[0][0] != "git_status" {
		t.Errorf("tools[0] = %v", tools[0])
	}
	if len(toolsets[1]) != 1 || toolsets[1][0] != "release-write" {
		t.Errorf("toolsets[1] = %v", toolsets[1])
	}
}

func TestParseFrontMatter_NoFrontMatter(t *testing.T) {
	src := "Just a plain markdown file with no front matter."
	fm, body, err := parseFrontMatter(src)
	if err != nil {
		t.Fatalf("parseFrontMatter: %v", err)
	}
	if body != src {
		t.Errorf("body = %q, want original src", body)
	}
	// Defaults should be set.
	if !fm.Enabled {
		t.Error("Enabled should default to true")
	}
	if fm.Temperature != 0.7 {
		t.Errorf("Temperature should default to 0.7, got %v", fm.Temperature)
	}
}

func TestParseFrontMatter_UnclosedDelimiter(t *testing.T) {
	src := "---\nname: broken\n"
	_, _, err := parseFrontMatter(src)
	if err == nil {
		t.Error("expected error for unclosed front matter, got nil")
	}
}

func TestParseFrontMatter_InvalidMaxTokens(t *testing.T) {
	src := "---\nname: x\nmax_tokens: notanumber\n---\nbody"
	_, _, err := parseFrontMatter(src)
	if err == nil {
		t.Error("expected error for invalid max_tokens, got nil")
	}
}

func TestParseFrontMatter_UnknownKeysIgnored(t *testing.T) {
	src := "---\nname: agent\nfuture_field: value\n---\nbody"
	fm, _, err := parseFrontMatter(src)
	if err != nil {
		t.Fatalf("parseFrontMatter: %v", err)
	}
	if fm.Name != "agent" {
		t.Errorf("Name = %q, want agent", fm.Name)
	}
}

func TestParseFrontMatter_BlockStyleLists(t *testing.T) {
	src := `---
name: inspector
skills:
  - repo
  - shell-git
toolsets:
  - release-read
tags:
  - ci
  - prod
---
body`
	fm, _, err := parseFrontMatter(src)
	if err != nil {
		t.Fatalf("parseFrontMatter: %v", err)
	}
	if len(fm.Skills) != 2 || fm.Skills[0] != "repo" || fm.Skills[1] != "shell-git" {
		t.Errorf("Skills = %v, want [repo shell-git]", fm.Skills)
	}
	if len(fm.Toolsets) != 1 || fm.Toolsets[0] != "release-read" {
		t.Errorf("Toolsets = %v, want [release-read]", fm.Toolsets)
	}
	if len(fm.Tags) != 2 || fm.Tags[0] != "ci" || fm.Tags[1] != "prod" {
		t.Errorf("Tags = %v, want [ci prod]", fm.Tags)
	}
}

// --- Validate ---

func TestValidate_ValidAgent(t *testing.T) {
	a := makeValidAgent()
	if errs := Validate(a); len(errs) != 0 {
		t.Errorf("expected no errors, got %v", errs)
	}
}

func TestValidate_MissingRequired(t *testing.T) {
	a := makeValidAgent()
	a.Name = ""
	if errs := Validate(a); len(errs) != 1 {
		t.Errorf("expected 1 error for missing name, got %d: %v", len(errs), errs)
	}

	// Schedule is intentionally NOT required by Validate. Agents may be driven
	// by curings, queues, webhooks, run, chat, or test-agent — none of which
	// need a cron expression. Schedule enforcement for scheduled jobs lives
	// in the lifecycle loader (requireSchedule), not here.
	a = makeValidAgent()
	a.Schedule = ""
	if errs := Validate(a); len(errs) != 0 {
		t.Errorf("expected no errors when schedule is empty, got %d: %v", len(errs), errs)
	}
}

func TestValidate_TemperatureOutOfRange(t *testing.T) {
	a := makeValidAgent()
	a.Temperature = 3.0
	if errs := Validate(a); len(errs) == 0 {
		t.Error("expected error for temperature 3.0, got none")
	}
}

// --- LoadFile / LoadDir ---

func TestLoadFile_Valid(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.agent.md")
	mustWrite(t, path, `---
name: test-agent
schedule: "* * * * *"
model: llama3
---
Do things.
`)
	a, err := LoadFile(path)
	if err != nil {
		t.Fatalf("LoadFile: %v", err)
	}
	if a.Name != "test-agent" {
		t.Errorf("Name = %q, want test-agent", a.Name)
	}
	if a.SystemPrompt != "Do things." {
		t.Errorf("SystemPrompt = %q, want 'Do things.'", a.SystemPrompt)
	}
	if a.SourcePath != path {
		t.Errorf("SourcePath = %q, want %q", a.SourcePath, path)
	}
}

func TestLoadFile_Missing(t *testing.T) {
	_, err := LoadFile("/nonexistent/path.agent.md")
	if err == nil {
		t.Error("expected error for missing file, got nil")
	}
}

func TestLoadDir_MixedFiles(t *testing.T) {
	dir := t.TempDir()
	mustWrite(t, filepath.Join(dir, "good.agent.md"), `---
name: good
schedule: "0 * * * *"
model: llama3
---
Prompt.
`)
	// Missing name — clearly invalid (name is the only unconditionally-required
	// field; schedule is enforced separately by the lifecycle loader for
	// scheduled agents, not for curing/queue/run-driven agents).
	mustWrite(t, filepath.Join(dir, "bad.agent.md"), `---
schedule: "0 * * * *"
---
Prompt.
`)
	// Non-.agent.md file — should be skipped.
	mustWrite(t, filepath.Join(dir, "notes.md"), "not an agent")

	agents, errs := LoadDir(dir)
	if len(agents) != 1 {
		t.Errorf("expected 1 valid agent, got %d", len(agents))
	}
	if len(errs) == 0 {
		t.Error("expected errors for invalid agent, got none")
	}
}

func TestLoadDir_MissingDir(t *testing.T) {
	_, errs := LoadDir("/nonexistent/dir")
	if len(errs) == 0 {
		t.Error("expected error for missing directory, got none")
	}
}

// --- Lifecycle loading ---

func TestLoadDir_WithLifecycleFile(t *testing.T) {
	dir := t.TempDir()
	mustWrite(t, filepath.Join(dir, "daily.agent.md"), `---
name: daily
---
Run the daily job.
`)
	mustWrite(t, filepath.Join(dir, "daily.lifecycle.yaml"), `agent: daily
schedule: "0 9 * * *"
model: llama3
max_tokens: 4096
tags: [daily]
toolsets: [release-read]
`)

	agents, errs := LoadDir(dir)
	if len(errs) != 0 {
		t.Fatalf("unexpected errors: %v", errs)
	}
	if len(agents) != 1 {
		t.Fatalf("expected 1 agent, got %d", len(agents))
	}
	a := agents[0]
	if a.Name != "daily" {
		t.Errorf("Name = %q, want daily", a.Name)
	}
	if a.Schedule != "0 9 * * *" {
		t.Errorf("Schedule = %q, want '0 9 * * *'", a.Schedule)
	}
	if a.Model != "llama3" {
		t.Errorf("Model = %q, want llama3", a.Model)
	}
	if a.MaxTokens != 4096 {
		t.Errorf("MaxTokens = %d, want 4096", a.MaxTokens)
	}
	if len(a.Toolsets) != 1 || a.Toolsets[0] != "release-read" {
		t.Errorf("Toolsets = %v, want [release-read]", a.Toolsets)
	}
	if a.LifecycleSourcePath == "" {
		t.Error("LifecycleSourcePath should be set")
	}
	if a.SystemPrompt != "Run the daily job." {
		t.Errorf("SystemPrompt = %q, want 'Run the daily job.'", a.SystemPrompt)
	}
}

func TestLoadDir_LifecycleListForm(t *testing.T) {
	dir := t.TempDir()
	mustWrite(t, filepath.Join(dir, "summary.agent.md"), `---
name: summary
---
Summarize.
`)
	mustWrite(t, filepath.Join(dir, "summary.lifecycle.yaml"), `agent: summary
instances:
  - name: morning
    schedule: "0 9 * * *"
    model: llama3
  - name: evening
    schedule: "0 21 * * *"
    model: llama3-70b
`)

	agents, errs := LoadDir(dir)
	if len(errs) != 0 {
		t.Fatalf("unexpected errors: %v", errs)
	}
	if len(agents) != 2 {
		t.Fatalf("expected 2 agents from list form, got %d", len(agents))
	}
	// Results are sorted by name.
	names := map[string]bool{agents[0].Name: true, agents[1].Name: true}
	if !names["morning"] || !names["evening"] {
		t.Errorf("expected names [morning, evening], got [%s, %s]", agents[0].Name, agents[1].Name)
	}
	for _, a := range agents {
		if a.SystemPrompt != "Summarize." {
			t.Errorf("%s: SystemPrompt = %q, want 'Summarize.'", a.Name, a.SystemPrompt)
		}
	}
}

func TestLoadDir_LifecycleMissingAgentDef(t *testing.T) {
	dir := t.TempDir()
	mustWrite(t, filepath.Join(dir, "orphan.lifecycle.yaml"), `agent: nonexistent
schedule: "0 * * * *"
model: llama3
`)

	agents, errs := LoadDir(dir)
	if len(errs) == 0 {
		t.Error("expected error for lifecycle referencing nonexistent agent def")
	}
	if len(agents) != 0 {
		t.Errorf("expected 0 agents, got %d", len(agents))
	}
}

func TestLoadDir_LifecycleOverridesAgentFrontMatter(t *testing.T) {
	dir := t.TempDir()
	// Agent file has schedule/model in front matter; lifecycle should win.
	mustWrite(t, filepath.Join(dir, "override.agent.md"), `---
name: override
schedule: "* * * * *"
model: small-model
---
Prompt.
`)
	mustWrite(t, filepath.Join(dir, "override.lifecycle.yaml"), `agent: override
schedule: "0 12 * * *"
model: large-model
`)

	agents, errs := LoadDir(dir)
	if len(errs) != 0 {
		t.Fatalf("unexpected errors: %v", errs)
	}
	if len(agents) != 1 {
		t.Fatalf("expected 1 agent, got %d", len(agents))
	}
	a := agents[0]
	if a.Schedule != "0 12 * * *" {
		t.Errorf("Schedule = %q, want lifecycle value '0 12 * * *'", a.Schedule)
	}
	if a.Model != "large-model" {
		t.Errorf("Model = %q, want lifecycle value 'large-model'", a.Model)
	}
}

// T5.1: lifecycle file silent on a scalar must not overwrite frontmatter.
// Frontmatter says enabled=false; lifecycle omits enabled → still disabled.
func TestLoadDir_LifecyclePreservesFrontmatterWhenSilent(t *testing.T) {
	dir := t.TempDir()
	mustWrite(t, filepath.Join(dir, "guard.agent.md"), `---
name: guard
schedule: "* * * * *"
model: small-model
enabled: false
---
Prompt.
`)
	mustWrite(t, filepath.Join(dir, "guard.lifecycle.yaml"), `agent: guard
schedule: "0 6 * * *"
`)
	agents, errs := LoadDir(dir)
	if len(errs) != 0 {
		t.Fatalf("unexpected errors: %v", errs)
	}
	if len(agents) != 1 {
		t.Fatalf("expected 1 agent, got %d", len(agents))
	}
	a := agents[0]
	if a.Enabled {
		t.Errorf("Enabled = true; want false (frontmatter said false, lifecycle silent)")
	}
	if a.Model != "small-model" {
		t.Errorf("Model = %q; want 'small-model' (lifecycle silent → frontmatter wins)", a.Model)
	}
	if a.Schedule != "0 6 * * *" {
		t.Errorf("Schedule = %q; want lifecycle value", a.Schedule)
	}
}

// T5.1: lifecycle with explicit enabled:false overrides frontmatter true.
func TestLoadDir_LifecycleEnabledFalseOverrides(t *testing.T) {
	dir := t.TempDir()
	mustWrite(t, filepath.Join(dir, "kill.agent.md"), `---
name: kill
schedule: "* * * * *"
model: small-model
---
Prompt.
`)
	mustWrite(t, filepath.Join(dir, "kill.lifecycle.yaml"), `agent: kill
schedule: "0 6 * * *"
enabled: false
`)
	agents, errs := LoadDir(dir)
	if len(errs) != 0 {
		t.Fatalf("unexpected errors: %v", errs)
	}
	if len(agents) != 1 {
		t.Fatalf("expected 1 agent, got %d", len(agents))
	}
	if agents[0].Enabled {
		t.Error("Enabled = true; want false (lifecycle explicitly disabled)")
	}
}

func TestLoadDir_UnclaimedAgentUsedDirectly(t *testing.T) {
	dir := t.TempDir()
	// Agent with full front matter, no lifecycle file — should still load.
	mustWrite(t, filepath.Join(dir, "solo.agent.md"), `---
name: solo
schedule: "once"
model: llama3
---
Solo prompt.
`)

	agents, errs := LoadDir(dir)
	if len(errs) != 0 {
		t.Fatalf("unexpected errors: %v", errs)
	}
	if len(agents) != 1 {
		t.Fatalf("expected 1 agent, got %d", len(agents))
	}
	if agents[0].LifecycleSourcePath != "" {
		t.Error("LifecycleSourcePath should be empty for unclaimed agent def")
	}
}

// --- parseLifecycleYAML ---

func TestParseLifecycleYAML_MissingAgent(t *testing.T) {
	yaml := `schedule: "* * * * *"
model: llama3
`
	_, err := parseLifecycleYAML(yaml)
	if err == nil {
		t.Error("expected error for missing agent: field")
	}
}

func TestParseLifecycleYAML_MissingSchedule(t *testing.T) {
	yaml := `agent: test-agent
model: llama3
`
	_, err := parseLifecycleYAML(yaml)
	if err == nil {
		t.Error("expected error for missing schedule: field in flat form")
	}
}

// TestParseLifecycleYAML_MissingModel verifies that a lifecycle document with no model:
// field is valid — model may be provided by config.yaml at the CLI layer.
func TestParseLifecycleYAML_MissingModel(t *testing.T) {
	yaml := `agent: test-agent
schedule: "* * * * *"
`
	_, err := parseLifecycleYAML(yaml)
	if err != nil {
		t.Errorf("expected no error for missing model (now optional): %v", err)
	}
}

func TestParseLifecycleYAML_SingletonDisable(t *testing.T) {
	// disable: true on a singleton returns zero records.
	yaml := `agent: my-agent
schedule: "* * * * *"
model: llama3
disable: true
`
	records, err := parseLifecycleYAML(yaml)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(records) != 0 {
		t.Errorf("expected 0 records for disable: true singleton, got %d", len(records))
	}
}

func TestParseLifecycleYAML_InstanceDisable(t *testing.T) {
	// disable: true on a single instance removes it; the other remains.
	yaml := `agent: my-agent
model: llama3
instances:
  - name: keep
    schedule: "* * * * *"
  - name: drop
    schedule: "* * * * *"
    disable: true
`
	records, err := parseLifecycleYAML(yaml)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(records) != 1 {
		t.Fatalf("expected 1 record, got %d", len(records))
	}
	if records[0].JobName != "keep" {
		t.Errorf("JobName = %q, want \"keep\"", records[0].JobName)
	}
}

func TestParseLifecycleYAML_TopLevelDisableList(t *testing.T) {
	// disable: [name, ...] at top level removes the named instances.
	yaml := `agent: my-agent
model: llama3
disable: [drop1, drop2]
instances:
  - name: keep
    schedule: "* * * * *"
  - name: drop1
    schedule: "* * * * *"
  - name: drop2
    schedule: "* * * * *"
`
	records, err := parseLifecycleYAML(yaml)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(records) != 1 {
		t.Fatalf("expected 1 record, got %d", len(records))
	}
	if records[0].JobName != "keep" {
		t.Errorf("JobName = %q, want \"keep\"", records[0].JobName)
	}
}

func TestParseLifecycleYAML_FlatForm(t *testing.T) {
	yaml := `agent: my-agent
schedule: "0 6 * * *"
model: gpt-4
name: morning-run
`
	records, err := parseLifecycleYAML(yaml)
	if err != nil {
		t.Fatalf("parseLifecycleYAML: %v", err)
	}
	if len(records) != 1 {
		t.Fatalf("expected 1 record, got %d", len(records))
	}
	r := records[0]
	if r.AgentName != "my-agent" {
		t.Errorf("AgentName = %q, want my-agent", r.AgentName)
	}
	if r.JobName != "morning-run" {
		t.Errorf("JobName = %q, want morning-run", r.JobName)
	}
	if r.Schedule != "0 6 * * *" {
		t.Errorf("Schedule = %q, want '0 6 * * *'", r.Schedule)
	}
	if r.Model != "gpt-4" {
		t.Errorf("Model = %q, want gpt-4", r.Model)
	}
}

func TestParseLifecycleYAML_ListForm_MissingName(t *testing.T) {
	yaml := `agent: my-agent
instances:
  - schedule: "* * * * *"
    model: llama3
`
	_, err := parseLifecycleYAML(yaml)
	if err == nil {
		t.Error("expected error for list instance missing name:")
	}
}

func TestParseLifecycleYAML_ListForm_TopLevelDefaults(t *testing.T) {
	// model and temperature are defined at top level; instances only add schedule/name.
	yaml := `agent: shared-agent
model: llama3
temperature: 0.5
instances:
  - name: fast
    schedule: "*/5 * * * *"
  - name: slow
    schedule: "0 * * * *"
    model: llama3-70b
`
	records, err := parseLifecycleYAML(yaml)
	if err != nil {
		t.Fatalf("parseLifecycleYAML: %v", err)
	}
	if len(records) != 2 {
		t.Fatalf("expected 2 records, got %d", len(records))
	}
	byName := map[string]lifecycleRecord{}
	for _, r := range records {
		byName[r.JobName] = r
	}

	fast := byName["fast"]
	if fast.Model != "llama3" {
		t.Errorf("fast.Model = %q, want 'llama3' (inherited from top level)", fast.Model)
	}
	if fast.Temperature != 0.5 {
		t.Errorf("fast.Temperature = %v, want 0.5 (inherited from top level)", fast.Temperature)
	}
	if fast.Schedule != "*/5 * * * *" {
		t.Errorf("fast.Schedule = %q, want '*/5 * * * *'", fast.Schedule)
	}

	slow := byName["slow"]
	if slow.Model != "llama3-70b" {
		t.Errorf("slow.Model = %q, want 'llama3-70b' (instance overrides top level)", slow.Model)
	}
	if slow.Temperature != 0.5 {
		t.Errorf("slow.Temperature = %v, want 0.5 (inherited from top level)", slow.Temperature)
	}
}

func TestParseLifecycleYAML_ListForm_TagsAppend(t *testing.T) {
	// Top-level tags are merged with per-instance tags (additive).
	yaml := `agent: tag-agent
model: llama3
tags: [shared, prod]
instances:
  - name: alpha
    schedule: "* * * * *"
    tags: [alpha]
  - name: beta
    schedule: "* * * * *"
`
	records, err := parseLifecycleYAML(yaml)
	if err != nil {
		t.Fatalf("parseLifecycleYAML: %v", err)
	}
	if len(records) != 2 {
		t.Fatalf("expected 2 records, got %d", len(records))
	}
	byName := map[string]lifecycleRecord{}
	for _, r := range records {
		byName[r.JobName] = r
	}

	// alpha: top-level [shared, prod] + instance [alpha]
	alpha := byName["alpha"]
	wantAlpha := []string{"shared", "prod", "alpha"}
	if len(alpha.Tags) != len(wantAlpha) {
		t.Errorf("alpha.Tags = %v, want %v", alpha.Tags, wantAlpha)
	} else {
		for i, v := range wantAlpha {
			if alpha.Tags[i] != v {
				t.Errorf("alpha.Tags[%d] = %q, want %q", i, alpha.Tags[i], v)
			}
		}
	}

	// beta: top-level [shared, prod], no instance tags
	beta := byName["beta"]
	wantBeta := []string{"shared", "prod"}
	if len(beta.Tags) != len(wantBeta) {
		t.Errorf("beta.Tags = %v, want %v", beta.Tags, wantBeta)
	}
}

func TestParseLifecycleYAML_ListForm_PromptPerInstance(t *testing.T) {
	yaml := `agent: prompt-agent
model: llama3
prompt: default prompt
instances:
  - name: five
    schedule: "*/5 * * * *"
    prompt: five-minute check
  - name: hourly
    schedule: "0 * * * *"
`
	records, err := parseLifecycleYAML(yaml)
	if err != nil {
		t.Fatalf("parseLifecycleYAML: %v", err)
	}
	byName := map[string]lifecycleRecord{}
	for _, r := range records {
		byName[r.JobName] = r
	}

	if got := byName["five"].UserPrompt; got != "five-minute check" {
		t.Errorf("five.UserPrompt = %q, want 'five-minute check'", got)
	}
	// hourly inherits the top-level default prompt
	if got := byName["hourly"].UserPrompt; got != "default prompt" {
		t.Errorf("hourly.UserPrompt = %q, want 'default prompt' (inherited)", got)
	}
}

func TestParseLifecycleYAML_BlockStyleTags(t *testing.T) {
	// Both top-level and per-instance tags use block-style lists.
	yaml := `agent: block-agent
model: llama3
tags:
  - shared
  - prod
instances:
  - name: alpha
    schedule: "* * * * *"
    tags:
      - alpha
  - name: beta
    schedule: "* * * * *"
`
	records, err := parseLifecycleYAML(yaml)
	if err != nil {
		t.Fatalf("parseLifecycleYAML: %v", err)
	}
	if len(records) != 2 {
		t.Fatalf("expected 2 records, got %d", len(records))
	}
	byName := map[string]lifecycleRecord{}
	for _, r := range records {
		byName[r.JobName] = r
	}

	// alpha: top-level [shared, prod] + instance [alpha]
	wantAlpha := []string{"shared", "prod", "alpha"}
	alpha := byName["alpha"]
	if len(alpha.Tags) != len(wantAlpha) {
		t.Errorf("alpha.Tags = %v, want %v", alpha.Tags, wantAlpha)
	} else {
		for i, v := range wantAlpha {
			if alpha.Tags[i] != v {
				t.Errorf("alpha.Tags[%d] = %q, want %q", i, alpha.Tags[i], v)
			}
		}
	}

	// beta: only top-level tags
	wantBeta := []string{"shared", "prod"}
	beta := byName["beta"]
	if len(beta.Tags) != len(wantBeta) {
		t.Errorf("beta.Tags = %v, want %v", beta.Tags, wantBeta)
	} else {
		for i, v := range wantBeta {
			if beta.Tags[i] != v {
				t.Errorf("beta.Tags[%d] = %q, want %q", i, beta.Tags[i], v)
			}
		}
	}
}
func TestParseLifecycleYAML_Hooks(t *testing.T) {
	src := `agent: my-agent
schedule: "* * * * *"
model: llama3
hooks:
  pre_run: echo "before"
  post_success: echo "ok"
  post_error: echo "err"
`
	recs, err := parseLifecycleYAML(src)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(recs) != 1 {
		t.Fatalf("expected 1 record, got %d", len(recs))
	}
	h := recs[0].Hooks
	if h.PreRun != `echo "before"` {
		t.Errorf("PreRun: got %q", h.PreRun)
	}
	if h.PostSuccess != `echo "ok"` {
		t.Errorf("PostSuccess: got %q", h.PostSuccess)
	}
	if h.PostError != `echo "err"` {
		t.Errorf("PostError: got %q", h.PostError)
	}
}

func TestParseLifecycleYAML_QueueBatchAndMaxAttempts(t *testing.T) {
	tests := []struct {
		name            string
		yaml            string
		wantBatchSize   int
		wantMaxAttempts int
	}{
		{
			name: "flat form with queue_batch_size and queue_max_attempts",
			yaml: `agent: q-agent
schedule: "* * * * *"
model: llama3
queue_input: incoming
queue_batch_size: 5
queue_max_attempts: 3
`,
			wantBatchSize:   5,
			wantMaxAttempts: 3,
		},
		{
			name: "omitted fields default to zero",
			yaml: `agent: q-agent
schedule: "* * * * *"
model: llama3
queue_input: incoming
`,
			wantBatchSize:   0,
			wantMaxAttempts: 0,
		},
		{
			name: "zero values are ignored (non-positive)",
			yaml: `agent: q-agent
schedule: "* * * * *"
model: llama3
queue_batch_size: 0
queue_max_attempts: 0
`,
			wantBatchSize:   0,
			wantMaxAttempts: 0,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Helper()
			records, err := parseLifecycleYAML(tc.yaml)
			if err != nil {
				t.Fatalf("parseLifecycleYAML: %v", err)
			}
			if len(records) != 1 {
				t.Fatalf("expected 1 record, got %d", len(records))
			}
			r := records[0]
			if r.QueueBatchSize != tc.wantBatchSize {
				t.Errorf("QueueBatchSize = %d, want %d", r.QueueBatchSize, tc.wantBatchSize)
			}
			if r.QueueMaxAttempts != tc.wantMaxAttempts {
				t.Errorf("QueueMaxAttempts = %d, want %d", r.QueueMaxAttempts, tc.wantMaxAttempts)
			}
		})
	}
}

func TestLoadDir_QueueBatchSizePropagated(t *testing.T) {
	dir := t.TempDir()
	mustWrite(t, filepath.Join(dir, "batch.agent.md"), `---
name: batch
---
Run batch.
`)
	mustWrite(t, filepath.Join(dir, "batch.lifecycle.yaml"), `agent: batch
schedule: "* * * * *"
model: llama3
queue_input: work
queue_batch_size: 10
queue_max_attempts: 2
`)
	agents, errs := LoadDir(dir)
	if len(errs) != 0 {
		t.Fatalf("unexpected errors: %v", errs)
	}
	if len(agents) != 1 {
		t.Fatalf("expected 1 agent, got %d", len(agents))
	}
	a := agents[0]
	if a.QueueBatchSize != 10 {
		t.Errorf("QueueBatchSize = %d, want 10", a.QueueBatchSize)
	}
	if a.QueueMaxAttempts != 2 {
		t.Errorf("QueueMaxAttempts = %d, want 2", a.QueueMaxAttempts)
	}
	if a.QueueInput != "work" {
		t.Errorf("QueueInput = %q, want 'work'", a.QueueInput)
	}
}

func TestParseLifecycleYAML_BlockLiteralParameter(t *testing.T) {
	yaml := `agent: review-triage
schedule: once
model: llama3
parameters:
  pr_url: "https://example.com/pull/1"
  author: "alice"
  thread: |
    [alice, 1h ago]:
    This is the first line.

    This is after a blank.
    [bob, 30m ago]:
    Short reply.
  reviewer: "bob"
`
	records, err := parseLifecycleYAML(yaml)
	if err != nil {
		t.Fatalf("parseLifecycleYAML: %v", err)
	}
	if len(records) != 1 {
		t.Fatalf("expected 1 record, got %d", len(records))
	}
	p := records[0].Parameters
	if p["pr_url"] != "https://example.com/pull/1" {
		t.Errorf("pr_url = %q, want https://example.com/pull/1", p["pr_url"])
	}
	if p["author"] != "alice" {
		t.Errorf("author = %q, want alice", p["author"])
	}
	if p["reviewer"] != "bob" {
		t.Errorf("reviewer = %q, want bob", p["reviewer"])
	}
	wantThread := "[alice, 1h ago]:\nThis is the first line.\n\nThis is after a blank.\n[bob, 30m ago]:\nShort reply.\n"
	if p["thread"] != wantThread {
		t.Errorf("thread = %q, want %q", p["thread"], wantThread)
	}
}

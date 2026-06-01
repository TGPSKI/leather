// Package agent implements loading, parsing, and validation of agent definition files.
//
// Two file types are supported:
//   - *.agent.md  — agent identity and system prompt (required)
//   - *.lifecycle.yaml — preferred file for scheduling and operational config
//
// When a *.lifecycle.yaml file is present for an agent, its fields take
// precedence over the matching agent file's front matter for scheduling,
// model, and all other operational fields.
package agent

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/tgpski/leather/internal/model"
)

// LoadDir discovers all *.agent.md and *.lifecycle.yaml files in dir,
// merges them, and returns validated Agent values.
//
// Loading proceeds in three phases:
//  1. All *.agent.md files are parsed into an agent-name-keyed map.
//  2. All *.lifecycle.yaml files are parsed; each record produces one scheduled
//     job instance (a clone of the matching agent def with lifecycle config applied).
//     One lifecycle file may produce N instances via the instances: list form.
//  3. Agent defs not referenced by any lifecycle file are included as-is,
//     using their front-matter schedule/model fields directly.
//
// Files that fail to parse or validate are skipped; errors are returned
// alongside the successful agents.
func LoadDir(dir string) ([]model.Agent, []error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, []error{fmt.Errorf("agent/LoadDir: %w", err)}
	}

	var errs []error

	// Phase 1: parse all *.agent.md files into a name-keyed map.
	agentDefs := map[string]*model.Agent{}
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".agent.md") {
			continue
		}
		path := filepath.Join(dir, e.Name())
		a, err := LoadFile(path)
		if err != nil {
			errs = append(errs, err)
			continue
		}
		if a.Name == "" {
			errs = append(errs, fmt.Errorf("agent %s: missing required field: name", e.Name()))
			continue
		}
		if _, dup := agentDefs[a.Name]; dup {
			errs = append(errs, fmt.Errorf("agent %s: duplicate name %q", e.Name(), a.Name))
			continue
		}
		cp := a
		agentDefs[a.Name] = &cp
	}

	// Phase 2: parse all *.lifecycle.yaml files; each record becomes a job instance.
	jobsByName := map[string]*model.Agent{} // keyed by job/instance name
	claimedDefs := map[string]bool{}        // agent names referenced by any lifecycle

	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".lifecycle.yaml") {
			continue
		}
		path := filepath.Join(dir, e.Name())
		records, err := loadLifecycleFile(path)
		if err != nil {
			errs = append(errs, err)
			continue
		}
		for _, rec := range records {
			def, ok := agentDefs[rec.AgentName]
			if !ok {
				errs = append(errs, fmt.Errorf("lifecycle %s: no agent definition found for %q", e.Name(), rec.AgentName))
				continue
			}
			claimedDefs[rec.AgentName] = true
			if _, dup := jobsByName[rec.JobName]; dup {
				errs = append(errs, fmt.Errorf("lifecycle %s: duplicate job name %q", e.Name(), rec.JobName))
				continue
			}
			clone := *def
			clone.Name = rec.JobName
			applyLifecycle(&clone, rec)
			jobsByName[rec.JobName] = &clone
		}
	}

	// Phase 3: add unclaimed agent defs (front matter provides all config).
	for name, def := range agentDefs {
		if claimedDefs[name] {
			continue
		}
		if _, dup := jobsByName[name]; dup {
			errs = append(errs, fmt.Errorf("agent %q: job name conflicts with a lifecycle instance", name))
			continue
		}
		cp := *def
		jobsByName[name] = &cp
	}

	// Phase 4: validate all jobs, collect results sorted by name.
	names := make([]string, 0, len(jobsByName))
	for n := range jobsByName {
		names = append(names, n)
	}
	sort.Strings(names)

	var agents []model.Agent
	for _, n := range names {
		a := jobsByName[n]
		if verrs := Validate(*a); len(verrs) > 0 {
			for _, ve := range verrs {
				errs = append(errs, fmt.Errorf("agent %s: %w", filepath.Base(a.SourcePath), ve))
			}
			continue
		}
		agents = append(agents, *a)
	}
	return agents, errs
}

// applyLifecycle overlays lifecycle record fields onto a, with lifecycle taking precedence.
// Fields that the lifecycle file does not explicitly set leave the agent's existing
// value untouched, so frontmatter and config-yaml defaults are not silently wiped
// (T5.1).
func applyLifecycle(a *model.Agent, rec lifecycleRecord) {
	if rec.Schedule != "" {
		a.Schedule = rec.Schedule
	}
	if rec.Model != "" {
		a.Model = rec.Model
	}
	if rec.EnabledSet {
		a.Enabled = rec.Enabled
	}
	if rec.MaxTokens > 0 {
		a.MaxTokens = rec.MaxTokens
	}
	if rec.Timeout > 0 {
		a.Timeout = rec.Timeout
	}
	if rec.Temperature != 0 {
		a.Temperature = rec.Temperature
	}
	if len(rec.Tags) > 0 {
		a.Tags = rec.Tags
	}
	if len(rec.Skills) > 0 {
		a.Skills = rec.Skills
	}
	if len(rec.Toolsets) > 0 {
		a.Toolsets = rec.Toolsets
	}
	if rec.ToolRounds > 0 {
		a.ToolRounds = rec.ToolRounds
	}
	if rec.QueueInput != "" {
		a.QueueInput = rec.QueueInput
	}
	if rec.QueueBatchSize > 0 {
		a.QueueBatchSize = rec.QueueBatchSize
	}
	if rec.QueueMaxAttempts > 0 {
		a.QueueMaxAttempts = rec.QueueMaxAttempts
	}
	if rec.Cache.Enabled || rec.Cache.TTL > 0 {
		a.Cache = rec.Cache
	}
	if len(rec.OutputRoutes) > 0 {
		a.OutputRoutes = rec.OutputRoutes
	}
	if len(rec.UserPrompt) > 0 {
		a.UserPrompt = rec.UserPrompt
		a.TurnTools = nil // lifecycle prompt replaces body turns
		a.TurnSkills = nil
		a.TurnToolsets = nil
	}
	if len(rec.UserPrompts) > 0 {
		a.UserPrompts = rec.UserPrompts
		a.TurnTools = nil // lifecycle prompts replace body turns
		a.TurnSkills = nil
		a.TurnToolsets = nil
	}
	if rec.Hooks.PreRun != "" || rec.Hooks.PostSuccess != "" || rec.Hooks.PostError != "" {
		a.Hooks = rec.Hooks
	}
	if len(rec.Parameters) > 0 {
		a.Parameters = rec.Parameters
	}
	a.LifecycleSourcePath = rec.SourcePath
}

// LoadFile parses a single *.agent.md file and returns an Agent.
// schedule and model are optional at the file level — they may be supplied
// by a paired *.lifecycle.yaml file. Call Validate after merging.
func LoadFile(path string) (model.Agent, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return model.Agent{}, fmt.Errorf("agent/LoadFile: %w", err)
	}

	fm, body, err := parseFrontMatter(string(data))
	if err != nil {
		return model.Agent{}, fmt.Errorf("agent/LoadFile %s: %w", filepath.Base(path), err)
	}

	sysPrompt, turnPrompts, turnTools, turnSkills, turnToolsets := splitAgentBody(body)

	return model.Agent{
		Name:         fm.Name,
		Schedule:     fm.Schedule,
		Model:        fm.Model,
		SystemPrompt: sysPrompt,
		UserPrompts:  turnPrompts,
		TurnTools:    turnTools,
		MaxTokens:    fm.MaxTokens,
		Timeout:      fm.Timeout,
		Temperature:  fm.Temperature,
		Enabled:      fm.Enabled,
		Tags:         fm.Tags,
		Skills:       fm.Skills,
		Toolsets:     fm.Toolsets,
		ToolRounds:   fm.ToolRounds,
		TurnSkills:   turnSkills,
		TurnToolsets: turnToolsets,
		SourcePath:   path,
	}, nil
}

// splitAgentBody splits the agent body on "\n---\n" boundaries into a system
// prompt and a sequence of per-turn user prompts with optional tool restrictions.
//
// The first section becomes the system prompt. Each subsequent section may
// begin with optional declaration lines:
//   - skills: [skill1, skill2]
//   - toolsets: [toolset1, toolset2]
//   - tools: [tool1, tool2]
//
// These lines are consumed and not included in the user prompt text sent to the model.
//
// A nil entry in turn slices means "not declared for that turn". When the body
// contains no "---" separators, turn slices are nil and existing behaviour
// (single system-prompt agent) is preserved.
func splitAgentBody(body string) (sysPrompt string, turnPrompts []string, turnTools [][]string, turnSkills [][]string, turnToolsets [][]string) {
	const sep = "\n---\n"
	parts := strings.Split(body, sep)
	sysPrompt = strings.TrimSpace(parts[0])
	if len(parts) == 1 {
		return sysPrompt, nil, nil, nil, nil
	}
	for _, part := range parts[1:] {
		prompt, skills, toolsets, tools := parseTurnSection(part)
		turnPrompts = append(turnPrompts, prompt)
		turnTools = append(turnTools, tools)
		turnSkills = append(turnSkills, skills)
		turnToolsets = append(turnToolsets, toolsets)
	}
	return
}

func parseTurnSection(part string) (prompt string, skills []string, toolsets []string, tools []string) {
	lines := strings.Split(strings.TrimSpace(part), "\n")
	idx := 0
	for idx < len(lines) {
		line := strings.TrimSpace(lines[idx])
		if line == "" {
			idx++
			continue
		}
		key, raw, ok := turnDecl(line)
		if !ok {
			break
		}
		items := parseInlineList(raw)
		switch key {
		case "skills":
			skills = items
		case "toolsets":
			toolsets = items
		case "tools":
			tools = items
		}
		idx++
	}
	prompt = strings.TrimSpace(strings.Join(lines[idx:], "\n"))
	return prompt, skills, toolsets, tools
}

func turnDecl(line string) (string, string, bool) {
	for _, key := range []string{"skills", "toolsets", "tools"} {
		if after, found := strings.CutPrefix(line, key+":"); found {
			return key, strings.TrimSpace(after), true
		}
	}
	return "", "", false
}

func parseInlineList(raw string) []string {
	raw = strings.TrimPrefix(raw, "[")
	raw = strings.TrimSuffix(raw, "]")
	out := []string{}
	if strings.TrimSpace(raw) == "" {
		return out
	}
	for _, item := range strings.Split(raw, ",") {
		if s := strings.TrimSpace(item); s != "" {
			out = append(out, s)
		}
	}
	return out
}

// ApplyLifecycleFile looks for a *.lifecycle.yaml file co-located with agentPath
// (same directory, same stem) and, if found, applies it to a.
// The file is expected to be a singleton (no instances: block).
// Returns nil when no lifecycle file is found — absence is not an error.
func ApplyLifecycleFile(agentPath string, a *model.Agent) error {
	stem := strings.TrimSuffix(filepath.Base(agentPath), ".agent.md")
	lcPath := filepath.Join(filepath.Dir(agentPath), stem+".lifecycle.yaml")
	if _, err := os.Stat(lcPath); err != nil {
		return nil // not found — fine
	}
	records, err := loadLifecycleFile(lcPath)
	if err != nil {
		return err
	}
	if len(records) == 0 {
		return nil
	}
	applyLifecycle(a, records[0])
	return nil
}

// Validate checks an Agent for required fields and value constraints.
// Returns a slice of errors; an empty slice means the agent is valid.
//
// model is intentionally not checked here — it may be supplied by config.yaml
// and is applied via resolveAgent in the CLI layer before execution.
//
// schedule is intentionally not checked here either. Agents may be driven by
// the cron scheduler (schedule required), by a curing (queue/hide driven),
// by a webhook intake route, by `leather run`, by `leather chat`, or by
// `leather test-agent` — none of which require a cron expression. The
// schedule requirement for scheduled jobs is enforced where it belongs:
// inside the lifecycle loader (requireSchedule, internal/agent/lifecycle.go).
func Validate(a model.Agent) []error {
	var errs []error
	if a.Name == "" {
		errs = append(errs, fmt.Errorf("missing required field: name"))
	}
	if a.Temperature < 0 || a.Temperature > 2 {
		errs = append(errs, fmt.Errorf("temperature %.2g out of range [0, 2]", a.Temperature))
	}
	return errs
}

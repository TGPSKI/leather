// Package tool manages the tool registry: loading skill definitions from
// *.skill.yaml files, validating tool names, and dispatching executions.
package tool

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/tgpski/leather/internal/model"
)

// compiledExtractor is the compiled form of a model.SkillExtract rule.
type compiledExtractor struct {
	pattern *regexp.Regexp
	store   string
}

// Registry holds all loaded skills and their tools, indexed for fast lookup.
type Registry struct {
	skills     map[string]model.Skill          // skill name → Skill
	tools      map[string]model.ToolDefinition // tool name → ToolDefinition
	toolsets   map[string]model.Toolset        // toolset name → Toolset
	extractors map[string][]compiledExtractor  // tool name → compiled extract rules
}

// NewRegistry returns an empty Registry.
func NewRegistry() *Registry {
	return &Registry{
		skills:     make(map[string]model.Skill),
		tools:      make(map[string]model.ToolDefinition),
		toolsets:   make(map[string]model.Toolset),
		extractors: make(map[string][]compiledExtractor),
	}
}

// Load reads all *.skill.yaml files from dir and registers their skills and tools.
// Returns an error if dir cannot be read or any file fails to parse.
// A non-existent dir is silently ignored.
func Load(dir string) (*Registry, error) {
	r := NewRegistry()
	if dir == "" {
		return r, nil
	}
	entries, err := os.ReadDir(dir)
	if os.IsNotExist(err) {
		return r, nil
	}
	if err != nil {
		return nil, fmt.Errorf("tool/Load: read dir %s: %w", dir, err)
	}
	var pendingToolsets []struct {
		path string
		set  model.Toolset
	}
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		if !strings.HasSuffix(e.Name(), ".skill.yaml") && !strings.HasSuffix(e.Name(), ".toolset.yaml") {
			continue
		}
		path := filepath.Join(dir, e.Name())
		data, err := os.ReadFile(path)
		if err != nil {
			return nil, fmt.Errorf("tool/Load: read %s: %w", e.Name(), err)
		}
		if strings.HasSuffix(e.Name(), ".skill.yaml") {
			skill, err := parseSkillYAML(string(data))
			if err != nil {
				return nil, fmt.Errorf("tool/Load: parse %s: %w", e.Name(), err)
			}
			if skill.Name == "" {
				return nil, fmt.Errorf("tool/Load: %s: missing required field: name", e.Name())
			}
			if err := r.Register(skill); err != nil {
				return nil, fmt.Errorf("tool/Load: %s: %w", e.Name(), err)
			}
			continue
		}
		toolset, err := parseToolsetYAML(string(data))
		if err != nil {
			return nil, fmt.Errorf("tool/Load: parse %s: %w", e.Name(), err)
		}
		if toolset.Name == "" {
			return nil, fmt.Errorf("tool/Load: %s: missing required field: name", e.Name())
		}
		pendingToolsets = append(pendingToolsets, struct {
			path string
			set  model.Toolset
		}{path: e.Name(), set: toolset})
	}
	for _, pending := range pendingToolsets {
		if err := r.RegisterToolset(pending.set); err != nil {
			return nil, fmt.Errorf("tool/Load: %s: %w", pending.path, err)
		}
	}
	return r, nil
}

// Register adds a skill and indexes its tools. Returns an error on duplicate tool names
// or on invalid extract pattern regexps.
// This is the low-level entry point used by Load and directly by tests.
func (r *Registry) Register(s model.Skill) error {
	r.skills[s.Name] = s
	for _, t := range s.Tools {
		if _, exists := r.tools[t.Name]; exists {
			return fmt.Errorf("duplicate tool name %q (already registered by another skill)", t.Name)
		}
		r.tools[t.Name] = t
	}
	for i, e := range s.Extract {
		// Prepend (?m) so that ^ and $ anchor to line boundaries, matching
		// the natural expectation for patterns like "^AUTHOR: (.+)$".
		pat, err := regexp.Compile("(?m)" + e.Pattern)
		if err != nil {
			return fmt.Errorf("tool/Registry.Register: skill %q extract[%d] pattern %q: %w", s.Name, i, e.Pattern, err)
		}
		r.extractors[e.Tool] = append(r.extractors[e.Tool], compiledExtractor{pattern: pat, store: e.Store})
	}
	return nil
}

// ApplyExtractors scans content against all extraction rules registered for toolName.
// For each rule whose pattern matches, capture group 1 is written into vars under the
// rule's store key. Rules are applied in registration order; later matches overwrite
// earlier ones for the same store key. Noop when r is nil.
func (r *Registry) ApplyExtractors(toolName, content string, vars map[string]string) {
	if r == nil {
		return
	}
	for _, ext := range r.extractors[toolName] {
		if m := ext.pattern.FindStringSubmatch(content); len(m) > 1 {
			vars[ext.store] = m[1]
		}
	}
}

// RegisterToolset adds a named toolset. Every referenced tool must already be registered.
func (r *Registry) RegisterToolset(s model.Toolset) error {
	if _, exists := r.toolsets[s.Name]; exists {
		return fmt.Errorf("duplicate toolset name %q", s.Name)
	}
	for _, name := range s.Tools {
		if _, ok := r.tools[name]; !ok {
			return fmt.Errorf("toolset %q references unknown tool %q", s.Name, name)
		}
	}
	r.toolsets[s.Name] = s
	return nil
}

// GetTool returns the ToolDefinition for name, and false if not found.
func (r *Registry) GetTool(name string) (model.ToolDefinition, bool) {
	t, ok := r.tools[name]
	return t, ok
}

// GetToolset returns the Toolset for name, and false if not found.
func (r *Registry) GetToolset(name string) (model.Toolset, bool) {
	t, ok := r.toolsets[name]
	return t, ok
}

// GetTools returns the combined tool list for the named skills, in skill order.
// Unknown skill names are silently skipped. Duplicate tools (same name across
// skills) are deduplicated; the first occurrence wins.
func (r *Registry) GetTools(skillNames []string) []model.ToolDefinition {
	seen := make(map[string]bool)
	var out []model.ToolDefinition
	for _, name := range skillNames {
		s, ok := r.skills[name]
		if !ok {
			continue
		}
		for _, t := range s.Tools {
			if !seen[t.Name] {
				seen[t.Name] = true
				out = append(out, t)
			}
		}
	}
	return out
}

// GetToolsetTools returns the combined tool list for the named toolsets, in toolset order.
// Unknown toolset names are silently skipped. Duplicate tools are deduplicated; first wins.
func (r *Registry) GetToolsetTools(toolsetNames []string) []model.ToolDefinition {
	seen := make(map[string]bool)
	var out []model.ToolDefinition
	for _, name := range toolsetNames {
		ts, ok := r.toolsets[name]
		if !ok {
			continue
		}
		for _, toolName := range ts.Tools {
			if seen[toolName] {
				continue
			}
			toolDef, ok := r.tools[toolName]
			if !ok {
				continue
			}
			seen[toolName] = true
			out = append(out, toolDef)
		}
	}
	return out
}

// ResolveTools returns the deduplicated union of tools declared via skills,
// toolsets, and explicit tool names, in that order.
func (r *Registry) ResolveTools(skillNames, toolsetNames, toolNames []string) []model.ToolDefinition {
	seen := make(map[string]bool)
	var out []model.ToolDefinition
	appendTool := func(toolDef model.ToolDefinition) {
		if seen[toolDef.Name] {
			return
		}
		seen[toolDef.Name] = true
		out = append(out, toolDef)
	}
	for _, toolDef := range r.GetTools(skillNames) {
		appendTool(toolDef)
	}
	for _, toolDef := range r.GetToolsetTools(toolsetNames) {
		appendTool(toolDef)
	}
	for _, name := range toolNames {
		if toolDef, ok := r.GetTool(name); ok {
			appendTool(toolDef)
		}
	}
	return out
}

// GetSkills returns the Skill definitions for the named skills, in order.
// Unknown names are silently skipped.
func (r *Registry) GetSkills(skillNames []string) []model.Skill {
	var out []model.Skill
	for _, name := range skillNames {
		if s, ok := r.skills[name]; ok {
			out = append(out, s)
		}
	}
	return out
}

// parseToolsetYAML parses a *.toolset.yaml document into a Toolset.
// Supported top-level fields: name, description, tools.
func parseToolsetYAML(src string) (model.Toolset, error) {
	src = strings.ReplaceAll(src, "\r\n", "\n")
	lines := strings.Split(src, "\n")
	var set model.Toolset
	inTools := false
	for _, rawLine := range lines {
		line := strings.TrimSpace(rawLine)
		if line == "" || strings.HasPrefix(line, "#") || line == "---" {
			continue
		}
		if inTools {
			if strings.HasPrefix(line, "- ") {
				if name := toolsetUnquote(strings.TrimSpace(strings.TrimPrefix(line, "- "))); name != "" {
					set.Tools = append(set.Tools, name)
				}
				continue
			}
			inTools = false
		}
		key, val := splitKV(line)
		switch key {
		case "name":
			set.Name = toolsetUnquote(val)
		case "description":
			set.Description = toolsetUnquote(val)
		case "tools":
			if strings.HasPrefix(strings.TrimSpace(val), "[") {
				raw := strings.TrimSpace(val)
				raw = strings.TrimPrefix(raw, "[")
				raw = strings.TrimSuffix(raw, "]")
				for _, item := range strings.Split(raw, ",") {
					if name := toolsetUnquote(strings.TrimSpace(item)); name != "" {
						set.Tools = append(set.Tools, name)
					}
				}
			} else {
				inTools = true
			}
		}
	}
	return set, nil
}

func toolsetUnquote(v string) string {
	if len(v) >= 2 {
		if (v[0] == '"' && v[len(v)-1] == '"') || (v[0] == '\'' && v[len(v)-1] == '\'') {
			return v[1 : len(v)-1]
		}
	}
	return v
}

func splitKV(line string) (string, string) {
	idx := strings.Index(line, ":")
	if idx < 0 {
		return "", ""
	}
	return strings.TrimSpace(line[:idx]), strings.TrimSpace(line[idx+1:])
}

// parseSkillYAML parses a *.skill.yaml document into a Skill.
//
// Supported top-level fields: name, description, system_prompt_append.
// The tools: block contains a list of tool items; each tool item supports:
//
//	name, description, type, and an http: sub-block with method, url,
//	headers, query, and body sub-maps.
func parseSkillYAML(src string) (model.Skill, error) {
	src = strings.ReplaceAll(src, "\r\n", "\n")
	lines := strings.Split(src, "\n")

	// Locate tools: line index.
	toolsLine := -1
	for i, l := range lines {
		if strings.TrimSpace(l) == "tools:" {
			toolsLine = i
			break
		}
	}

	// Parse top-level section (before tools:).
	topEnd := len(lines)
	if toolsLine >= 0 {
		topEnd = toolsLine
	}
	skill, err := parseSkillTop(lines[:topEnd])
	if err != nil {
		return model.Skill{}, err
	}

	if toolsLine >= 0 {
		tools, err := parseToolList(lines[toolsLine+1:])
		if err != nil {
			return model.Skill{}, err
		}
		// Apply skill-level required_env to each tool's AllowedEnv.
		// A per-tool AllowedEnv (if already set) takes precedence.
		if len(skill.RequiredEnv) > 0 {
			for i := range tools {
				if tools[i].AllowedEnv == nil {
					tools[i].AllowedEnv = skill.RequiredEnv
				}
			}
		}
		skill.Tools = tools
	}
	return skill, nil
}

// parseSkillTop parses the top-level skill fields from lines before the tools: block.
func parseSkillTop(lines []string) (model.Skill, error) {
	var skill model.Skill
	inBlock := false
	blockKey := ""
	var blockLines []string
	var blockBaseIndent int
	inParams := false
	inExtract := false
	inRequiredEnv := false
	var extractItem model.SkillExtract

	flush := func() {
		if blockKey == "" {
			return
		}
		v := strings.Join(blockLines, "\n")
		if !strings.HasSuffix(v, "\n") {
			v += "\n"
		}
		if blockKey == "system_prompt_append" {
			skill.SystemPromptAppend = v
		}
		blockKey = ""
		blockLines = nil
		blockBaseIndent = 0
	}

	flushExtract := func() {
		if extractItem.Tool != "" || extractItem.Pattern != "" || extractItem.Store != "" {
			skill.Extract = append(skill.Extract, extractItem)
			extractItem = model.SkillExtract{}
		}
	}

	setExtractKV := func(k, v string) {
		switch k {
		case "tool":
			extractItem.Tool = skillUnquote(v)
		case "pattern":
			extractItem.Pattern = skillUnquote(v)
		case "store":
			extractItem.Store = skillUnquote(v)
		}
	}

	for _, rawLine := range lines {
		trimmed := strings.TrimSpace(rawLine)
		ind := countLeadingSpaces(rawLine)

		if inBlock {
			if trimmed == "" {
				blockLines = append(blockLines, "")
				continue
			}
			if ind <= 0 {
				// Back at top level — end block.
				flush()
				inBlock = false
				// Fall through to process this line.
			} else {
				// Strip base indent from the block content.
				content := rawLine
				if blockBaseIndent > 0 && len(rawLine) >= blockBaseIndent {
					content = rawLine[blockBaseIndent:]
				}
				blockLines = append(blockLines, content)
				continue
			}
		}

		// Collect indented key-value pairs under parameters:.
		if inParams {
			if trimmed == "" {
				continue
			}
			if ind > 0 {
				k, v := splitKV(trimmed)
				if k != "" {
					if skill.Parameters == nil {
						skill.Parameters = make(map[string]string)
					}
					skill.Parameters[k] = skillUnquote(v)
				}
				continue
			}
			// Back at top level — end parameters block; fall through.
			inParams = false
		}

		// Collect list items under extract:.
		if inExtract {
			if trimmed == "" {
				continue
			}
			if ind > 0 {
				if strings.HasPrefix(trimmed, "- ") {
					flushExtract()
					rest := strings.TrimPrefix(trimmed, "- ")
					k, v := splitKV(rest)
					setExtractKV(k, v)
				} else {
					k, v := splitKV(trimmed)
					setExtractKV(k, v)
				}
				continue
			}
			// Back at top level — end extract block; fall through.
			flushExtract()
			inExtract = false
		}

		// Collect list items under required_env:.
		if inRequiredEnv {
			if trimmed == "" {
				continue
			}
			if ind > 0 {
				if strings.HasPrefix(trimmed, "- ") {
					env := strings.TrimSpace(strings.TrimPrefix(trimmed, "- "))
					if env != "" {
						skill.RequiredEnv = append(skill.RequiredEnv, env)
					}
				}
				continue
			}
			// Back at top level — end required_env block; fall through.
			inRequiredEnv = false
		}

		if trimmed == "" || strings.HasPrefix(trimmed, "#") || trimmed == "---" {
			continue
		}
		if ind > 0 {
			continue // nested content in top section, skip
		}

		key, val := splitKV(trimmed)
		switch key {
		case "name":
			skill.Name = skillUnquote(val)
		case "description":
			skill.Description = skillUnquote(val)
		case "system_prompt_append":
			if val == "|" || val == "" {
				inBlock = true
				blockKey = "system_prompt_append"
				blockLines = nil
				blockBaseIndent = 2 // expect 2-space indented block
			} else {
				skill.SystemPromptAppend = skillUnquote(val)
			}
		case "parameters":
			inParams = true
		case "extract":
			inExtract = true
		case "required_env":
			inRequiredEnv = true
		}
	}
	flush()
	flushExtract()
	return skill, nil
}

// parseToolList parses the lines following "tools:" into a list of ToolDefinitions.
func parseToolList(lines []string) ([]model.ToolDefinition, error) {
	// Find the indent level of list items.
	listIndent := -1
	for _, l := range lines {
		t := strings.TrimSpace(l)
		if t == "" || strings.HasPrefix(t, "#") {
			continue
		}
		if strings.HasPrefix(t, "- ") {
			listIndent = countLeadingSpaces(l)
			break
		}
	}
	if listIndent < 0 {
		return nil, nil
	}

	// Split into per-tool blocks.
	var blocks []string
	var cur []string
	for _, rawLine := range lines {
		t := strings.TrimSpace(rawLine)
		if t == "" || strings.HasPrefix(t, "#") {
			if cur != nil {
				cur = append(cur, "")
			}
			continue
		}
		ind := countLeadingSpaces(rawLine)
		if ind == listIndent && strings.HasPrefix(t, "- ") {
			if cur != nil {
				blocks = append(blocks, strings.Join(cur, "\n"))
			}
			// First line of new tool: strip "- " prefix and normalize indent.
			rest := strings.TrimPrefix(t, "- ")
			cur = []string{rest}
			continue
		}
		// Lines belonging to the current tool (indented past the list item level).
		if cur != nil && ind > listIndent {
			strip := listIndent + 2
			stripped := rawLine
			if len(rawLine) >= strip {
				stripped = rawLine[strip:]
			}
			cur = append(cur, stripped)
		}
	}
	if cur != nil {
		blocks = append(blocks, strings.Join(cur, "\n"))
	}

	out := make([]model.ToolDefinition, 0, len(blocks))
	for i, b := range blocks {
		td, err := parseToolBlock(b)
		if err != nil {
			return nil, fmt.Errorf("tool %d: %w", i, err)
		}
		out = append(out, td)
	}
	return out, nil
}

// parseToolBlock parses a single tool item block (with "- " already stripped).
func parseToolBlock(block string) (model.ToolDefinition, error) {
	lines := strings.Split(block, "\n")

	// Locate http: and mcp: sub-block start lines.
	httpLine := -1
	mcpLine := -1
	for i, l := range lines {
		t := strings.TrimSpace(l)
		if t == "http:" && httpLine < 0 {
			httpLine = i
		}
		if t == "mcp:" && mcpLine < 0 {
			mcpLine = i
		}
	}

	// topEnd is the first sub-block line, capping the flat-field section.
	topEnd := len(lines)
	for _, sub := range []int{httpLine, mcpLine} {
		if sub >= 0 && sub < topEnd {
			topEnd = sub
		}
	}

	var td model.ToolDefinition
	for _, l := range lines[:topEnd] {
		t := strings.TrimSpace(l)
		if t == "" || strings.HasPrefix(t, "#") {
			continue
		}
		if countLeadingSpaces(l) > 0 {
			continue // nested lines before a sub-block belong to that block
		}
		key, val := splitKV(t)
		switch key {
		case "name":
			td.Name = skillUnquote(val)
		case "description":
			td.Description = skillUnquote(val)
		case "type":
			td.Type = skillUnquote(val)
		case "output_file":
			td.OutputFile = skillUnquote(val)
		}
	}
	if td.Type == "" {
		td.Type = "http"
	}

	if httpLine >= 0 {
		end := len(lines)
		if mcpLine >= 0 && mcpLine > httpLine {
			end = mcpLine
		}
		cfg, err := parseHTTPConfig(lines[httpLine+1 : end])
		if err != nil {
			return model.ToolDefinition{}, fmt.Errorf("http config for tool %q: %w", td.Name, err)
		}
		td.HTTP = cfg
	}

	if mcpLine >= 0 {
		end := len(lines)
		if httpLine >= 0 && httpLine > mcpLine {
			end = httpLine
		}
		cfg, err := parseMCPConfig(lines[mcpLine+1 : end])
		if err != nil {
			return model.ToolDefinition{}, fmt.Errorf("mcp config for tool %q: %w", td.Name, err)
		}
		td.MCP = cfg
	}
	return td, nil
}

// parseMCPConfig parses the key-value lines inside an mcp: sub-block.
func parseMCPConfig(lines []string) (model.MCPToolConfig, error) {
	var cfg model.MCPToolConfig
	for _, l := range lines {
		t := strings.TrimSpace(l)
		if t == "" || strings.HasPrefix(t, "#") {
			continue
		}
		key, val := splitKV(t)
		switch key {
		case "server":
			cfg.Server = skillUnquote(val)
		case "tool":
			cfg.Tool = skillUnquote(val)
		}
	}
	return cfg, nil
}

// parseHTTPConfig parses the lines inside an http: sub-block.
func parseHTTPConfig(lines []string) (model.HTTPToolConfig, error) {
	var cfg model.HTTPToolConfig

	type mapSection struct {
		key   string
		start int
	}
	var sections []mapSection
	flatEnd := len(lines)

	for i, l := range lines {
		t := strings.TrimSpace(l)
		if t == "headers:" || t == "query:" || t == "body:" {
			if len(sections) == 0 && i < flatEnd {
				flatEnd = i
			}
			sections = append(sections, mapSection{key: strings.TrimSuffix(t, ":"), start: i})
		}
	}

	// Parse flat fields (method, url) from lines before first section.
	for _, l := range lines[:flatEnd] {
		t := strings.TrimSpace(l)
		if t == "" || strings.HasPrefix(t, "#") {
			continue
		}
		key, val := splitKV(t)
		switch key {
		case "method":
			cfg.Method = strings.ToUpper(skillUnquote(val))
		case "url":
			cfg.URL = skillUnquote(val)
		}
	}

	// Parse each map sub-section.
	for idx, sec := range sections {
		end := len(lines)
		if idx+1 < len(sections) {
			end = sections[idx+1].start
		}
		m := make(map[string]string)
		for _, l := range lines[sec.start+1 : end] {
			t := strings.TrimSpace(l)
			if t == "" || strings.HasPrefix(t, "#") {
				continue
			}
			key, val := splitKV(t)
			if key != "" {
				m[key] = skillUnquote(val)
			}
		}
		switch sec.key {
		case "headers":
			cfg.Headers = m
		case "query":
			cfg.Query = m
		case "body":
			cfg.Body = m
		}
	}
	return cfg, nil
}

// skillUnquote strips surrounding single or double quotes.
func skillUnquote(s string) string {
	if len(s) >= 2 {
		if (s[0] == '"' && s[len(s)-1] == '"') || (s[0] == '\'' && s[len(s)-1] == '\'') {
			return s[1 : len(s)-1]
		}
	}
	return s
}

// countLeadingSpaces returns the number of leading space characters in s.
func countLeadingSpaces(s string) int {
	for i, c := range s {
		if c != ' ' {
			return i
		}
	}
	return len(s)
}

package agent

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/tgpski/leather/internal/config"
	"github.com/tgpski/leather/internal/model"
)

// lifecycleRecord is one resolved lifecycle instance parsed from a *.lifecycle.yaml file.
// A single lifecycle file may produce multiple records via the instantiations: list form.
//
// Filename conventions (for human readability only — not parsed by code):
//
//	{agent-name}.lifecycle.yaml                  → singleton for agent-name
//	{instance-name}.{agent-name}.lifecycle.yaml  → named instance of agent-name
//
// YAML field values are always authoritative. The filename is never parsed.
type lifecycleRecord struct {
	AgentName        string // from required `agent:` field
	JobName          string // from optional `name:` field; defaults to AgentName for singletons
	Schedule         string // required
	Model            string // required (LLM model name)
	Enabled          bool
	EnabledSet       bool // true when `enabled:` was explicitly present in YAML (T5.1)
	MaxTokens        int
	Timeout          time.Duration
	Temperature      float64
	Tags             []string
	Skills           []string            // skill names for tool calling
	Toolsets         []string            // named toolsets for tool calling
	ToolRounds       int                 // max tool-call cycles per run; 0 = global default
	QueueInput       string              // named queue to dequeue one item per tick for prompt substitution
	QueueBatchSize   int                 // max items dequeued per scheduler tick (0/1 = default)
	QueueMaxAttempts int                 // max retries before DLQ promotion; 0 = disabled
	UserPrompt       string              // per-instantiation user message sent at each execution
	UserPrompts      []string            // ordered chain of user messages; non-empty replaces UserPrompt
	Cache            model.CacheConfig   // response caching config
	OutputRoutes     []model.OutputRoute // output routing destinations
	Hooks            model.AgentHooks    // lifecycle shell hooks
	Parameters       map[string]string   // from parameters: block; empty-string values prompt the user
	SourcePath       string
}

// loadLifecycleFile reads a *.lifecycle.yaml file and returns one or more records.
func loadLifecycleFile(path string) ([]lifecycleRecord, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("agent/loadLifecycleFile: %w", err)
	}
	records, err := parseLifecycleYAML(string(data))
	if err != nil {
		return nil, fmt.Errorf("agent/loadLifecycleFile %s: %w", filepath.Base(path), err)
	}
	for i := range records {
		records[i].SourcePath = path
	}
	return records, nil
}

// parseLifecycleYAML parses a lifecycle YAML document.
//
// Required top-level field: agent.
// Flat form: schedule is also required at the top level.
// List form (instances: key present): top-level scalar fields serve as defaults for every
// instance; instance fields override scalars and append to tags.
func parseLifecycleYAML(src string) ([]lifecycleRecord, error) {
	src = strings.ReplaceAll(src, "\r\n", "\n")

	topVals, topLists := config.ParseBlock(extractTopLevel(src))
	agentName := topVals["agent"]
	if agentName == "" {
		return nil, fmt.Errorf("missing required field: agent")
	}

	if isListForm(src) {
		return parseListForm(src, agentName, topVals, topLists)
	}
	// Singleton: disable: true drops the record entirely.
	disabled, _ := strconv.ParseBool(topVals["disable"])
	if disabled {
		return []lifecycleRecord{}, nil
	}
	rec, err := parseFlatForm(agentName, topVals, topLists, src)
	if err != nil {
		return nil, err
	}
	return []lifecycleRecord{rec}, nil
}

// isListForm reports whether src has a top-level "instances:" key.
func isListForm(src string) bool {
	for _, line := range strings.Split(src, "\n") {
		if strings.TrimSpace(line) == "instances:" {
			return true
		}
	}
	return false
}

// containsString reports whether s appears in slice.
func containsString(slice []string, s string) bool {
	for _, v := range slice {
		if v == s {
			return true
		}
	}
	return false
}

// extractTopLevel returns the lines of src that appear before the "instances:" block.
// These lines carry the top-level default values shared across all instances.
func extractTopLevel(src string) string {
	var lines []string
	for _, line := range strings.Split(src, "\n") {
		if strings.TrimSpace(line) == "instances:" {
			break
		}
		lines = append(lines, line)
	}
	return strings.Join(lines, "\n")
}

// parseFlatForm applies pre-parsed top-level YAML maps to a singleton lifecycle record.
// src is the raw YAML string used to extract nested sub-blocks (cache, output).
// name: is optional and defaults to the agent name.
func parseFlatForm(agentName string, vals map[string]string, lists map[string][]string, src string) (lifecycleRecord, error) {
	rec := lifecycleRecord{
		AgentName: agentName,
		JobName:   agentName,
		Enabled:   true,
	}
	if err := applyLifecycleFields(vals, lists, &rec); err != nil {
		return lifecycleRecord{}, err
	}
	cacheCfg, err := parseCacheBlock(src)
	if err != nil {
		return lifecycleRecord{}, err
	}
	rec.Cache = cacheCfg
	rec.OutputRoutes = parseOutputRoutes(src)
	rec.Hooks = parseHooksBlock(src)
	rec.Parameters = parseParametersBlock(src)
	return requireSchedule(rec)
}

// parseListForm parses the instances: list and returns one record per instance.
//
// Inheritance: top-level scalar fields (model, schedule, max_tokens, timeout,
// temperature, enabled, prompt) serve as defaults; each instance overrides them
// individually. Tags are additive — instance tags append to top-level tags.
// name: must be specified per instance; agent: is always sourced from the top level.
func parseListForm(src, agentName string, topVals map[string]string, topLists map[string][]string) ([]lifecycleRecord, error) {
	// Build a base record from the already-parsed top-level fields.
	base := lifecycleRecord{
		AgentName: agentName,
		Enabled:   true,
	}
	if err := applyLifecycleFields(topVals, topLists, &base); err != nil {
		return nil, fmt.Errorf("top-level defaults: %w", err)
	}
	// Parse top-level cache and output blocks; instances inherit these defaults.
	topSrc := extractTopLevel(src)
	cacheCfg, err := parseCacheBlock(topSrc)
	if err != nil {
		return nil, fmt.Errorf("top-level cache: %w", err)
	}
	base.Cache = cacheCfg
	base.OutputRoutes = parseOutputRoutes(topSrc)
	base.Hooks = parseHooksBlock(topSrc)
	base.Parameters = parseParametersBlock(topSrc)
	disabledNames := topLists["disable"]

	blocks := splitInstanceBlocks(src)
	if len(blocks) == 0 {
		return nil, fmt.Errorf("instances: list is empty")
	}
	var records []lifecycleRecord
	for i, block := range blocks {
		rec := base      // inherit top-level defaults; tags will append below
		rec.JobName = "" // name must come from the instance block
		iVals, iLists := config.ParseBlock(block)
		if err := applyLifecycleFields(iVals, iLists, &rec); err != nil {
			return nil, fmt.Errorf("instance %d: %w", i, err)
		}
		// T2.5: parse nested per-instance blocks. Each of these overrides the
		// inherited base when present. If absent, the base value (already set
		// by the rec := base copy above) is preserved.
		if instCache, err := parseCacheBlock(block); err == nil && hasNestedBlock(block, "cache:") {
			rec.Cache = instCache
		}
		if hasNestedBlock(block, "hooks:") {
			rec.Hooks = parseHooksBlock(block)
		}
		if hasNestedBlock(block, "output:") {
			rec.OutputRoutes = parseOutputRoutes(block)
		}
		if hasNestedBlock(block, "parameters:") {
			rec.Parameters = parseParametersBlock(block)
		}
		if rec.JobName == "" {
			return nil, fmt.Errorf("instance %d: missing required field: name", i)
		}
		// Per-instance disable: true removes the instance.
		disabled, _ := strconv.ParseBool(iVals["disable"])
		if disabled {
			continue
		}
		// Top-level disable: [name, ...] removes named instances.
		if containsString(disabledNames, rec.JobName) {
			continue
		}
		r, err := requireSchedule(rec)
		if err != nil {
			return nil, fmt.Errorf("instance %d (%s): %w", i, rec.JobName, err)
		}
		records = append(records, r)
	}
	return records, nil
}

// splitInstanceBlocks splits the YAML block under "instances:" into per-item blocks.
//
// The indentation level of the first "- " item after "instances:" is the reference
// level. Only "- " items at that exact indent start a new instance block; deeper
// "- " items (e.g., block-style tag lists) are accumulated as content of the
// current block.
//
// T2.5: continuation lines preserve indentation RELATIVE to the instance-level
// indent (= reference indent + 2 for the "- " prefix), so nested blocks
// (cache:, hooks:, output:, parameters:) parse correctly per instance.
func splitInstanceBlocks(src string) []string {
	var blocks []string
	var current []string
	inInstances := false
	instanceIndent := -1 // indentation of instance-level "- " items; -1 = not yet seen

	for _, line := range strings.Split(src, "\n") {
		if strings.TrimSpace(line) == "instances:" {
			inInstances = true
			continue
		}
		if !inInstances {
			continue
		}
		stripped := strings.TrimLeft(line, " \t")
		indent := len(line) - len(stripped)

		if strings.HasPrefix(stripped, "- ") {
			if instanceIndent < 0 {
				instanceIndent = indent // first "- " sets the reference level
			}
			if indent == instanceIndent {
				// New top-level instance.
				if current != nil {
					blocks = append(blocks, strings.Join(current, "\n"))
				}
				// Strip "- " prefix so the first line is at the same nominal
				// indent as the keys that follow on the next lines.
				current = []string{strings.TrimPrefix(stripped, "- ")}
				continue
			}
			// Deeper "- " item (e.g., inside tags:) — fall through to accumulate.
		}
		// Accumulate lines that belong to the current instance (deeper than instance level).
		// Preserve indentation RELATIVE to the instance-level indent + 2 (the
		// position of the first child key after "- "), so nested YAML blocks
		// retain their structure for parseCacheBlock/parseHooksBlock/etc.
		if current != nil && indent > instanceIndent {
			base := instanceIndent + 2
			rel := indent - base
			if rel < 0 {
				rel = 0
			}
			current = append(current, strings.Repeat(" ", rel)+stripped)
		}
	}
	if current != nil {
		blocks = append(blocks, strings.Join(current, "\n"))
	}
	return blocks
}

// hasNestedBlock reports whether the given block contains a top-level YAML key
// matching name (e.g. "cache:" or "hooks:"). Used by parseListForm to decide
// whether a per-instance override block was provided. Case-sensitive.
func hasNestedBlock(block, name string) bool {
	for _, line := range strings.Split(block, "\n") {
		trimmed := strings.TrimLeft(line, " \t")
		if trimmed == name || strings.HasPrefix(trimmed, name+" ") || strings.HasPrefix(trimmed, name+"\t") {
			return true
		}
	}
	return false
}

// applyLifecycleFields applies pre-parsed YAML maps to rec.
// Only keys present in vals/lists are applied; absent keys leave rec unchanged,
// preserving top-level → instance inheritance.
func applyLifecycleFields(vals map[string]string, lists map[string][]string, rec *lifecycleRecord) error {
	if v, ok := vals["name"]; ok {
		rec.JobName = v
	}
	if v, ok := vals["schedule"]; ok {
		rec.Schedule = v
	}
	if v, ok := vals["model"]; ok {
		rec.Model = v
	}
	if v, ok := vals["prompt"]; ok {
		rec.UserPrompt = v
	}
	if prompts := lists["prompts"]; len(prompts) > 0 {
		rec.UserPrompts = prompts
	}
	if v, ok := vals["enabled"]; ok {
		b, err := strconv.ParseBool(v)
		if err != nil {
			return fmt.Errorf("invalid enabled %q: %w", v, err)
		}
		rec.Enabled = b
		rec.EnabledSet = true
	}
	if v, ok := vals["max_tokens"]; ok {
		n, err := strconv.Atoi(v)
		if err != nil {
			return fmt.Errorf("invalid max_tokens %q: %w", v, err)
		}
		rec.MaxTokens = n
	}
	if v, ok := vals["timeout"]; ok {
		d, err := time.ParseDuration(v)
		if err != nil {
			return fmt.Errorf("invalid timeout %q: %w", v, err)
		}
		rec.Timeout = d
	}
	if v, ok := vals["temperature"]; ok {
		f, err := strconv.ParseFloat(v, 64)
		if err != nil {
			return fmt.Errorf("invalid temperature %q: %w", v, err)
		}
		rec.Temperature = f
	}
	// Tags are additive: instance tags append to top-level tags.
	rec.Tags = append(rec.Tags, lists["tags"]...)
	// Skills are additive: instance skills append to top-level skills.
	rec.Skills = append(rec.Skills, lists["skills"]...)
	// Toolsets are additive: instance toolsets append to top-level toolsets.
	rec.Toolsets = append(rec.Toolsets, lists["toolsets"]...)
	if v, ok := vals["tool_rounds"]; ok {
		n, err := strconv.Atoi(v)
		if err != nil {
			return fmt.Errorf("invalid tool_rounds %q: %w", v, err)
		}
		rec.ToolRounds = n
	}
	if v, ok := vals["queue_input"]; ok {
		rec.QueueInput = v
	}
	if v, ok := vals["queue_batch_size"]; ok {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			rec.QueueBatchSize = n
		}
	}
	if v, ok := vals["queue_max_attempts"]; ok {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			rec.QueueMaxAttempts = n
		}
	}
	return nil
}

// requireSchedule returns rec if schedule is populated, else an error.
// model is intentionally not required here — it may be provided by config.yaml.
func requireSchedule(rec lifecycleRecord) (lifecycleRecord, error) {
	if rec.Schedule == "" {
		return lifecycleRecord{}, fmt.Errorf("missing required field: schedule")
	}
	return rec, nil
}

// parseParametersBlock extracts the "parameters:" sub-block from src and returns
// a map of key → value. An empty value means "prompt the user interactively".
// Block literal scalars (key: |) are supported with clip-chomp semantics.
func parseParametersBlock(src string) map[string]string {
	var params map[string]string
	inParams := false
	blockKey := "" // non-empty while accumulating a block literal scalar
	blockIndent := -1
	var blockLines []string

	ensureParams := func() {
		if params == nil {
			params = make(map[string]string)
		}
	}
	flushBlock := func() {
		if blockKey == "" {
			return
		}
		// Clip chomp: strip trailing blank lines, join with \n, add one trailing \n.
		end := len(blockLines)
		for end > 0 && blockLines[end-1] == "" {
			end--
		}
		value := strings.Join(blockLines[:end], "\n")
		if value != "" {
			value += "\n"
		}
		ensureParams()
		params[blockKey] = value
		blockKey = ""
		blockIndent = -1
		blockLines = nil
	}

	for _, line := range strings.Split(src, "\n") {
		trimmed := strings.TrimLeft(line, " \t")

		if !inParams {
			if trimmed == "parameters:" {
				inParams = true
			}
			continue
		}

		// A non-indented non-empty line exits the parameters block entirely.
		if len(line) > 0 && line[0] != ' ' && line[0] != '\t' {
			flushBlock()
			break
		}

		if blockKey != "" {
			// Accumulating block literal scalar content.
			if trimmed == "" {
				blockLines = append(blockLines, "")
				continue
			}
			indent := len(line) - len(trimmed)
			if blockIndent < 0 {
				// First non-empty content line establishes the indent level.
				blockIndent = indent
			}
			if indent >= blockIndent {
				// Content line: strip the block indent prefix.
				content := line
				if len(line) >= blockIndent {
					content = line[blockIndent:]
				}
				blockLines = append(blockLines, content)
				continue
			}
			// indent < blockIndent: this line begins the next parameter key.
			flushBlock()
			// Fall through to process as a normal parameter line.
		}

		if trimmed == "" {
			continue
		}

		k, v, ok := lifecycleSplitKV(trimmed)
		if !ok {
			continue
		}
		// Block literal scalar indicators: |, |-,  |+
		if v == "|" || v == "|-" || v == "|+" {
			blockKey = k
			blockIndent = -1
			blockLines = nil
			continue
		}
		ensureParams()
		params[k] = v
	}
	flushBlock()
	return params
}

// parseCacheBlock extracts the "cache:" sub-block from src and returns a CacheConfig.
// The block is expected to have "enabled:" and/or "ttl:" as indented children.
func parseCacheBlock(src string) (model.CacheConfig, error) {
	var cfg model.CacheConfig
	inCache := false
	for _, line := range strings.Split(src, "\n") {
		trimmed := strings.TrimLeft(line, " \t")
		if trimmed == "cache:" {
			inCache = true
			continue
		}
		if inCache {
			// A line with no leading whitespace that is not blank ends the block.
			if len(line) > 0 && line[0] != ' ' && line[0] != '\t' {
				break
			}
			if trimmed == "" {
				continue
			}
			k, v, ok := lifecycleSplitKV(trimmed)
			if !ok {
				continue
			}
			switch k {
			case "enabled":
				b, err := strconv.ParseBool(v)
				if err != nil {
					return model.CacheConfig{}, fmt.Errorf("invalid cache.enabled %q: %w", v, err)
				}
				cfg.Enabled = b
			case "ttl":
				d, err := time.ParseDuration(v)
				if err != nil {
					return model.CacheConfig{}, fmt.Errorf("invalid cache.ttl %q: %w", v, err)
				}
				cfg.TTL = d
			}
		}
	}
	return cfg, nil
}

// parseOutputRoutes extracts the "output:" list block from src and returns
// a slice of OutputRoute descriptors.
//
// Each list item begins with "- type: <type>" and may contain additional keys:
//
//	output:
//	  - type: file
//	    path: /tmp/result-{{.date}}.txt
//	  - type: queue
//	    queue: outbound
//	  - type: http
//	    url: http://localhost:9000/hook
//	    method: POST
//	    headers:
//	      Authorization: Bearer {{env:TOKEN}}
func parseOutputRoutes(src string) []model.OutputRoute {
	// Collect lines under the top-level "output:" key.
	var outputLines []string
	inOutput := false
	for _, line := range strings.Split(src, "\n") {
		trimmed := strings.TrimLeft(line, " \t")
		if trimmed == "output:" {
			inOutput = true
			continue
		}
		if inOutput {
			// Top-level key with no leading whitespace ends the block.
			if len(line) > 0 && line[0] != ' ' && line[0] != '\t' {
				break
			}
			outputLines = append(outputLines, line)
		}
	}
	if len(outputLines) == 0 {
		return nil
	}

	// Split into per-item blocks. Each item starts with a "- " line.
	var items [][]string
	var current []string
	for _, line := range outputLines {
		stripped := strings.TrimLeft(line, " \t")
		if strings.HasPrefix(stripped, "- ") {
			if current != nil {
				items = append(items, current)
			}
			// First line of the item: the content after "- ".
			current = []string{strings.TrimPrefix(stripped, "- ")}
		} else if current != nil {
			// Subsequent lines: keep trimmed content for flat parsing.
			current = append(current, strings.TrimSpace(line))
		}
	}
	if current != nil {
		items = append(items, current)
	}

	var routes []model.OutputRoute
	for _, item := range items {
		route := parseOutputRouteItem(item)
		if route.Type != "" {
			routes = append(routes, route)
		}
	}
	return routes
}

// parseOutputRouteItem parses a single output route item from its flattened line slice.
// Lines are expected to be already trimmed of leading whitespace from list indentation.
func parseOutputRouteItem(lines []string) model.OutputRoute {
	var route model.OutputRoute
	inHeaders := false
	headers := map[string]string{}
	for _, line := range lines {
		trimmed := strings.TrimLeft(line, " \t")
		if trimmed == "" {
			continue
		}
		if trimmed == "headers:" {
			inHeaders = true
			continue
		}
		k, v, ok := lifecycleSplitKV(trimmed)
		if !ok {
			continue
		}
		// Known route-level keys terminate header collection.
		if inHeaders {
			switch k {
			case "type", "path", "queue", "url", "method", "backend":
				inHeaders = false
				// Fall through to process as a route key.
			default:
				headers[k] = v
				continue
			}
		}
		switch k {
		case "type":
			route.Type = v
		case "path":
			route.FilePath = v
		case "queue":
			route.Queue = v
		case "url":
			route.URL = v
		case "method":
			route.Method = v
		case "backend":
			route.NotifyBackend = v
		}
	}
	if len(headers) > 0 {
		route.Headers = headers
	}
	return route
}

// parseHooksBlock extracts the "hooks:" sub-block from src and returns AgentHooks.
func parseHooksBlock(src string) model.AgentHooks {
	var h model.AgentHooks
	inHooks := false
	for _, line := range strings.Split(src, "\n") {
		trimmed := strings.TrimLeft(line, " \t")
		if trimmed == "hooks:" {
			inHooks = true
			continue
		}
		if inHooks {
			if len(line) > 0 && line[0] != ' ' && line[0] != '\t' {
				break
			}
			if trimmed == "" {
				continue
			}
			k, v, ok := lifecycleSplitKV(trimmed)
			if !ok {
				continue
			}
			switch k {
			case "pre_run":
				h.PreRun = v
			case "post_success":
				h.PostSuccess = v
			case "post_error":
				h.PostError = v
			}
		}
	}
	return h
}

// lifecycleSplitKV splits "key: value" into (key, value, true).
// Returns ("", "", false) if the line contains no colon.
// Surrounding quotes are stripped from value.
func lifecycleSplitKV(line string) (key, value string, ok bool) {
	idx := strings.Index(line, ":")
	if idx < 0 {
		return "", "", false
	}
	key = strings.TrimSpace(line[:idx])
	value = strings.TrimSpace(line[idx+1:])
	if len(value) >= 2 && ((value[0] == '"' && value[len(value)-1] == '"') ||
		(value[0] == '\'' && value[len(value)-1] == '\'')) {
		value = value[1 : len(value)-1]
	}
	return key, value, key != ""
}

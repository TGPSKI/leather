// Package schema provides lightweight schema validation for leather definition files.
//
// Each definition file type (*.agent.md frontmatter, *.lifecycle.yaml, *.skill.yaml,
// *.worker.yaml) has a predefined Schema in defs.go. The Validate function checks
// flat YAML data (scalars and list fields) against a Schema and returns a slice of
// Violation values — one per violated field.
//
// Schema validation is additive to the existing parse-time checks: it catches type
// errors and enum violations that the parsers accept silently (e.g. tool_rounds: "abc").
package schema

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/tgpski/leather/internal/yamlx"
)

// FieldType describes the expected format of a scalar field value.
type FieldType uint8

const (
	TypeString   FieldType = iota // any non-empty string; no further format check
	TypeInteger                   // must parse as a base-10 integer
	TypeNumber                    // must parse as a float64
	TypeBoolean                   // must be "true" or "false" (case-insensitive)
	TypeDuration                  // must be a valid Go time.ParseDuration string
	TypeCron                      // must be a 5-field cron expression or "once"
	TypeEnum                      // must be one of AllowedValues
)

// Field defines validation rules for a single YAML field.
type Field struct {
	Type           FieldType
	Required       bool
	IsList         bool     // true if this field is a list (validated via lists map)
	AllowedValues  []string // for TypeEnum: acceptable values
	IntMin, IntMax int      // for TypeInteger: inclusive bounds; zero = no bound unless HasMin/HasMax set
	HasMin, HasMax bool
}

// Schema maps field names to their Field definitions.
// Each Schema covers the flat (scalar + list) portion of a YAML document.
// Nested blocks (cache:, output:, hooks:, instances:) are outside the flat scope
// and are not validated here — that responsibility stays with the dedicated parsers.
type Schema map[string]Field

// Violation is a single schema validation failure.
type Violation struct {
	Field   string // YAML field name
	Line    int    // 1-indexed source line; 0 means unknown (e.g. ParseBlock callers)
	Message string // human-readable description of the failure
}

// ValidateFlat checks flat YAML data against s.
// vals holds scalar field values; lists holds list field values (from yamlx.ParseBlock).
// lines maps field names to their 1-indexed source line numbers; pass nil when
// line information is unavailable (violations will have Line == 0).
// Returns violations for: missing required fields, type mismatches, enum violations,
// and out-of-range integers. Unknown fields are not flagged (forward-compatible).
func ValidateFlat(vals map[string]string, lists map[string][]string, lines map[string]int, s Schema) []Violation {
	var vs []Violation
	for name, f := range s {
		if f.IsList {
			items := lists[name]
			if f.Required && len(items) == 0 {
				vs = append(vs, Violation{
					Field:   name,
					Message: "required field missing or empty list",
				})
			}
			continue
		}
		val := vals[name]
		if f.Required && val == "" {
			vs = append(vs, Violation{Field: name, Line: lines[name], Message: "required field missing"})
			continue
		}
		if val == "" {
			continue // optional field absent — skip further checks
		}
		if msg := checkValue(val, f); msg != "" {
			vs = append(vs, Violation{Field: name, Line: lines[name], Message: msg})
		}
	}
	return vs
}

// checkValue validates val against f's type constraints.
// Returns an empty string on success, a human-readable message on failure.
func checkValue(val string, f Field) string {
	switch f.Type {
	case TypeBoolean:
		v := strings.ToLower(val)
		if v != "true" && v != "false" {
			return fmt.Sprintf("must be true or false, got %q", val)
		}
	case TypeInteger:
		n, err := strconv.Atoi(val)
		if err != nil {
			return fmt.Sprintf("must be an integer, got %q", val)
		}
		if f.HasMin && n < f.IntMin {
			return fmt.Sprintf("must be >= %d, got %d", f.IntMin, n)
		}
		if f.HasMax && n > f.IntMax {
			return fmt.Sprintf("must be <= %d, got %d", f.IntMax, n)
		}
	case TypeNumber:
		if _, err := strconv.ParseFloat(val, 64); err != nil {
			return fmt.Sprintf("must be a number, got %q", val)
		}
	case TypeDuration:
		if _, err := time.ParseDuration(val); err != nil {
			return fmt.Sprintf("must be a Go duration (e.g. 30s, 5m, 1h), got %q", val)
		}
	case TypeCron:
		if val != "once" && !validCron(val) {
			return fmt.Sprintf("must be a 5-field cron expression or \"once\", got %q", val)
		}
	case TypeEnum:
		for _, a := range f.AllowedValues {
			if val == a {
				return ""
			}
		}
		return fmt.Sprintf("must be one of [%s], got %q", strings.Join(f.AllowedValues, ", "), val)
	}
	return ""
}

var (
	cronOnce sync.Once
	cronRE   *regexp.Regexp
)

// validCron reports whether s is a valid 5- or 6-field cron expression.
// Accepts digit, *, /, -, and , characters within each field.
func validCron(s string) bool {
	cronOnce.Do(func() {
		// Five or six non-whitespace tokens separated by whitespace.
		cronRE = regexp.MustCompile(`^(\S+\s+){4,5}\S+$`)
	})
	n := len(strings.Fields(s))
	return (n == 5 || n == 6) && cronRE.MatchString(strings.TrimSpace(s))
}

// ValidateAgentFrontmatter parses src as agent front-matter YAML and returns field violations.
// src should be the YAML block between the --- delimiters (excluding the delimiters themselves).
func ValidateAgentFrontmatter(src string) []Violation {
	vals, lists := yamlx.ParseBlock(src)
	return ValidateFlat(vals, lists, nil, AgentFrontmatterSchema)
}

// ValidateLifecycleYAML parses src as lifecycle YAML and returns field violations.
// Nested blocks (cache:, hooks:, instances:) are not validated here.
// The output: block is partially validated: each item's type: field is checked
// against the allowed enum values [file, queue, http, notify].
func ValidateLifecycleYAML(src string) []Violation {
	vals, lists := yamlx.ParseBlock(src)
	viols := ValidateFlat(vals, lists, nil, LifecycleSchema)
	viols = append(viols, validateLifecycleOutputRoutes(src)...)
	return viols
}

// validateLifecycleOutputRoutes extracts each item in the output: list and
// validates that its type: field is one of the allowed values.
func validateLifecycleOutputRoutes(src string) []Violation {
	const key = "output:"
	allowedTypes := []string{"file", "queue", "http", "notify"}

	src = strings.ReplaceAll(src, "\r\n", "\n")
	lines := strings.Split(src, "\n")

	var viols []Violation
	inOutput := false
	itemIdx := -1
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}
		// Detect the top-level output: key.
		if !inOutput {
			if trimmed == key {
				inOutput = true
			}
			continue
		}
		// A non-indented non-blank line exits the block.
		if len(line) > 0 && line[0] != ' ' && line[0] != '\t' {
			break
		}
		// New list item.
		if strings.HasPrefix(trimmed, "- ") {
			itemIdx++
			rest := strings.TrimPrefix(trimmed, "- ")
			k, v := splitSchemaKV(rest)
			if k == "type" {
				if msg := checkEnum(v, allowedTypes); msg != "" {
					viols = append(viols, Violation{
						Field:   fmt.Sprintf("output[%d].type", itemIdx),
						Message: msg,
					})
				}
			}
			continue
		}
		// Continuation line within current item.
		if itemIdx >= 0 {
			k, v := splitSchemaKV(trimmed)
			if k == "type" {
				if msg := checkEnum(v, allowedTypes); msg != "" {
					viols = append(viols, Violation{
						Field:   fmt.Sprintf("output[%d].type", itemIdx),
						Message: msg,
					})
				}
			}
		}
	}
	return viols
}

// checkEnum validates that val is one of the allowed values. Returns "" on success.
func checkEnum(val string, allowed []string) string {
	for _, a := range allowed {
		if val == a {
			return ""
		}
	}
	return fmt.Sprintf("must be one of [%s], got %q", strings.Join(allowed, ", "), val)
}

// ValidateConfigYAML parses src as config.yaml content and returns field violations.
// The notify: nested block is not validated (parsed separately by the config loader).
func ValidateConfigYAML(src string) []Violation {
	_, lists := yamlx.ParseBlock(src)
	vals, _, lines, _ := yamlx.ParseFlatLines(strings.NewReader(src))
	return ValidateFlat(vals, lists, lines, ConfigSchema)
}

// ValidateSkillYAML parses src as skill YAML and returns field violations.
// Tool-level nested objects are not validated here.
func ValidateSkillYAML(src string) []Violation {
	_, lists := yamlx.ParseBlock(src)
	vals, _, lines, _ := yamlx.ParseFlatLines(strings.NewReader(src))
	return ValidateFlat(vals, lists, lines, SkillSchema)
}

// ValidateToolsetYAML parses src as toolset YAML and returns field violations.
func ValidateToolsetYAML(src string) []Violation {
	_, lists := yamlx.ParseBlock(src)
	vals, _, lines, _ := yamlx.ParseFlatLines(strings.NewReader(src))
	return ValidateFlat(vals, lists, lines, ToolsetSchema)
}

// ValidateWorkerYAML parses src as worker YAML and returns field violations.
func ValidateWorkerYAML(src string) []Violation {
	_, lists := yamlx.ParseBlock(src)
	vals, _, lines, _ := yamlx.ParseFlatLines(strings.NewReader(src))
	return ValidateFlat(vals, lists, lines, WorkerSchema)
}

// ValidateMCPServersYAML validates the flat fields of each item under the
// "servers:" key in an mcp-servers.yaml document. Violations are prefixed
// with the item index (e.g. "[0].name").
func ValidateMCPServersYAML(src string) []Violation {
	items := splitMCPItems(src)
	var viols []Violation
	for i, item := range items {
		for _, v := range ValidateFlat(item, nil, nil, MCPServersItemSchema) {
			v.Field = fmt.Sprintf("[%d].%s", i, v.Field)
			viols = append(viols, v)
		}
	}
	return viols
}

// splitMCPItems parses the servers list from an mcp-servers.yaml document
// into per-item flat key-value maps. Each list item starts with "  - " at
// indent 2 under the top-level "servers:" key.
func splitMCPItems(src string) []map[string]string {
	src = strings.ReplaceAll(src, "\r\n", "\n")
	lines := strings.Split(src, "\n")
	var items []map[string]string
	var cur map[string]string
	inServers := false
	for _, rawLine := range lines {
		trimmed := strings.TrimSpace(rawLine)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}
		ind := 0
		for _, c := range rawLine {
			if c != ' ' {
				break
			}
			ind++
		}
		// Top-level key — but only when the line is NOT a list item.
		if ind == 0 && !strings.HasPrefix(trimmed, "- ") {
			k, _ := splitSchemaKV(trimmed)
			inServers = (k == "servers")
			continue
		}
		if !inServers {
			continue
		}
		// List item start at any indent level.
		if strings.HasPrefix(trimmed, "- ") {
			if cur != nil {
				items = append(items, cur)
			}
			cur = make(map[string]string)
			rest := strings.TrimPrefix(trimmed, "- ")
			if rest != "" {
				k, v := splitSchemaKV(rest)
				if k != "" {
					cur[k] = v
				}
			}
			continue
		}
		// Continuation fields under the current list item.
		if cur != nil {
			k, v := splitSchemaKV(trimmed)
			if k != "" {
				cur[k] = v
			}
		}
	}
	if cur != nil {
		items = append(items, cur)
	}
	return items
}

// splitSchemaKV splits "key: value" into (key, value). Returns ("", "") if no colon found.
func splitSchemaKV(line string) (string, string) {
	idx := strings.Index(line, ":")
	if idx < 0 {
		return "", ""
	}
	key := strings.TrimSpace(line[:idx])
	val := strings.TrimSpace(line[idx+1:])
	// Strip trailing inline comment.
	if ci := strings.Index(val, " #"); ci >= 0 {
		val = strings.TrimSpace(val[:ci])
	}
	// Strip surrounding quotes.
	if len(val) >= 2 {
		if (val[0] == '"' && val[len(val)-1] == '"') || (val[0] == '\'' && val[len(val)-1] == '\'') {
			val = val[1 : len(val)-1]
		}
	}
	return key, val
}

// ValidateTanneryYAML validates a tannery.yaml document. It checks the flat
// top-level fields against TanneryConfigSchema and walks the nested routes,
// queues, and webhooks blocks against their respective item schemas.
func ValidateTanneryYAML(src string) []Violation {
	// Flat top-level fields (hide_dir, curing_dir, artifact_dir).
	vals, _ := yamlx.ParseBlock(src)
	viols := ValidateFlat(vals, nil, nil, TanneryConfigSchema)

	src = strings.ReplaceAll(src, "\r\n", "\n")
	lines := strings.Split(src, "\n")

	// Walk routes:, queues:, webhooks: blocks.
	viols = append(viols, walkTanneryRoutes(lines)...)
	viols = append(viols, walkTanneryQueues(lines)...)
	viols = append(viols, walkTanneryWebhooks(lines)...)
	return viols
}

// walkTanneryRoutes extracts each item in the routes: list and validates it
// against TanneryRouteSchema. Match sub-block fields are flattened into the
// parent item as "match.source" / "match.event_type" for reporting only.
func walkTanneryRoutes(lines []string) []Violation {
	items := splitTanneryListBlock(lines, "routes")
	var viols []Violation
	for i, it := range items {
		for _, v := range ValidateFlat(it.vals, nil, nil, TanneryRouteSchema) {
			v.Field = fmt.Sprintf("routes[%d].%s", i, v.Field)
			viols = append(viols, v)
		}
		// Exactly one of queue or queue_pattern is required per route.
		hasQueue := it.vals["queue"] != ""
		hasPattern := it.vals["queue_pattern"] != ""
		if !hasQueue && !hasPattern {
			viols = append(viols, Violation{
				Field:   fmt.Sprintf("routes[%d].queue", i),
				Message: "required field missing",
			})
		}
		// match: nested block — source is required when match: is present.
		if _, hasMatch := it.subBlocks["match"]; hasMatch {
			matchVals := it.subBlocks["match"]
			if matchVals["source"] == "" {
				viols = append(viols, Violation{
					Field:   fmt.Sprintf("routes[%d].match.source", i),
					Message: "required field missing",
				})
			}
		}
	}
	return viols
}

// walkTanneryQueues extracts each entry in the queues: map and validates it.
func walkTanneryQueues(lines []string) []Violation {
	entries := splitTanneryMapBlock(lines, "queues")
	var viols []Violation
	for name, vals := range entries {
		for _, v := range ValidateFlat(vals, nil, nil, TanneryQueueSchema) {
			v.Field = fmt.Sprintf("queues.%s.%s", name, v.Field)
			viols = append(viols, v)
		}
	}
	return viols
}

// walkTanneryWebhooks extracts each item in the webhooks: list and validates it.
func walkTanneryWebhooks(lines []string) []Violation {
	items := splitTanneryListBlock(lines, "webhooks")
	var viols []Violation
	for i, it := range items {
		for _, v := range ValidateFlat(it.vals, nil, nil, TanneryWebhookSchema) {
			v.Field = fmt.Sprintf("webhooks[%d].%s", i, v.Field)
			viols = append(viols, v)
		}
	}
	return viols
}

// ValidateCuringYAML validates a *.curing.yaml document. It checks the flat
// top-level fields against CuringSchema and the nested output: block against
// CuringOutputSchema.
func ValidateCuringYAML(src string) []Violation {
	vals, lists := yamlx.ParseBlock(src)
	viols := ValidateFlat(vals, lists, nil, CuringSchema)

	// Walk nested output: block.
	src = strings.ReplaceAll(src, "\r\n", "\n")
	lines := strings.Split(src, "\n")
	for name, sub := range collectTopLevelMaps(lines, []string{"output"}) {
		for _, v := range ValidateFlat(sub, nil, nil, CuringOutputSchema) {
			v.Field = fmt.Sprintf("%s.%s", name, v.Field)
			viols = append(viols, v)
		}
	}

	// Curing must declare exactly one of `queue` or `queue_prefix` at the
	// top level: the worker uses these to determine the source queue. Having
	// both is ambiguous; having neither leaves the worker with nothing to
	// poll.
	_, hasQueue := vals["queue"]
	_, hasQueuePrefix := vals["queue_prefix"]
	switch {
	case !hasQueue && !hasQueuePrefix:
		viols = append(viols, Violation{
			Field:   "queue",
			Message: "one of `queue` or `queue_prefix` is required",
		})
	case hasQueue && hasQueuePrefix:
		viols = append(viols, Violation{
			Field:   "queue",
			Message: "`queue` and `queue_prefix` are mutually exclusive",
		})
	}
	return viols
}

// tanneryListItem is one entry from a tannery list-of-maps block.
type tanneryListItem struct {
	// vals holds the item's scalar key/value pairs (those declared inline with the "- " marker
	// and on continuation lines at the same indent level).
	vals map[string]string
	// subBlocks holds nested map blocks declared under this item, keyed by sub-block name.
	subBlocks map[string]map[string]string
}

// splitTanneryListBlock returns the items under a top-level list-of-maps key
// such as "routes:" or "webhooks:". Each item starts with a "- " marker and
// may contain inline fields plus continuation lines and one level of nested
// map blocks (e.g. "match:").
func splitTanneryListBlock(lines []string, key string) []tanneryListItem {
	var items []tanneryListItem
	var cur *tanneryListItem
	var curSub string // name of the current nested map block, "" when none
	var subInd int    // leading-space count of the sub-block header line
	inBlock := false
	blockIndent := -1

	flush := func() {
		if cur != nil {
			items = append(items, *cur)
			cur = nil
			curSub = ""
		}
	}

	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}
		ind := leadingSpaces(line)
		// Top-level key transition: flush the in-progress item before changing state.
		if ind == 0 && !strings.HasPrefix(trimmed, "- ") {
			flush()
			k, _ := splitSchemaKV(trimmed)
			inBlock = (k == key)
			blockIndent = -1
			curSub = ""
			continue
		}
		if !inBlock {
			continue
		}
		if strings.HasPrefix(trimmed, "- ") {
			flush()
			cur = &tanneryListItem{vals: map[string]string{}, subBlocks: map[string]map[string]string{}}
			curSub = ""
			blockIndent = ind
			rest := strings.TrimPrefix(trimmed, "- ")
			if rest != "" {
				k, v := splitSchemaKV(rest)
				if k != "" {
					cur.vals[k] = v
				}
			}
			continue
		}
		if cur == nil {
			continue
		}
		// Continuation lines beneath this item (indent > blockIndent).
		if ind <= blockIndent {
			// Left the block entirely (non-list line at item indent — safety net).
			flush()
			inBlock = false
			continue
		}
		k, v := splitSchemaKV(trimmed)
		// Inside a nested map (e.g. match:)?
		if curSub != "" {
			// Exit the sub-block when indentation returns to the item-field level
			// (i.e. the same indent as the sub-block header or less).
			if ind <= subInd {
				curSub = "" // exit sub-block; fall through to handle as a regular field
			} else {
				cur.subBlocks[curSub][k] = v
				continue
			}
		}
		if v == "" && k != "" {
			// Possibly start of a nested map block (e.g. "match:").
			cur.subBlocks[k] = map[string]string{}
			curSub = k
			subInd = ind
			continue
		}
		if k != "" {
			cur.vals[k] = v
		}
	}
	flush()
	return items
}

// splitTanneryMapBlock returns the entries under a top-level map-of-maps key
// such as "queues:". Each entry has the form "name:\n  field: value".
func splitTanneryMapBlock(lines []string, key string) map[string]map[string]string {
	out := make(map[string]map[string]string)
	var curName string
	inBlock := false

	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}
		ind := leadingSpaces(line)
		if ind == 0 && !strings.HasPrefix(trimmed, "- ") {
			k, _ := splitSchemaKV(trimmed)
			inBlock = (k == key)
			curName = ""
			continue
		}
		if !inBlock {
			continue
		}
		k, v := splitSchemaKV(trimmed)
		if ind == 2 && v == "" && k != "" {
			curName = k
			out[curName] = map[string]string{}
			continue
		}
		if curName != "" && k != "" {
			out[curName][k] = v
		}
	}
	return out
}

// collectTopLevelMaps returns the contents of the named top-level map blocks
// (e.g. {"output"}) found in lines. Each result is a flat key→value map.
func collectTopLevelMaps(lines []string, keys []string) map[string]map[string]string {
	want := make(map[string]struct{}, len(keys))
	for _, k := range keys {
		want[k] = struct{}{}
	}
	out := make(map[string]map[string]string)
	cur := ""
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}
		ind := leadingSpaces(line)
		if ind == 0 {
			k, v := splitSchemaKV(trimmed)
			if _, ok := want[k]; ok && v == "" {
				cur = k
				out[cur] = map[string]string{}
				continue
			}
			cur = ""
			continue
		}
		if cur == "" {
			continue
		}
		k, v := splitSchemaKV(trimmed)
		if k != "" {
			out[cur][k] = v
		}
	}
	return out
}

// leadingSpaces returns the number of leading space characters on line.
func leadingSpaces(line string) int {
	n := 0
	for _, c := range line {
		if c != ' ' {
			break
		}
		n++
	}
	return n
}

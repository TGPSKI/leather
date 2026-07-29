package schema

import (
	"encoding/json"
	"fmt"
	"regexp"
)

// shellToolsConfig mirrors cmd/shell-mcp's config root. The shell-mcp binary
// is the format authority; this validator exists so `leather validate` can
// reject a malformed shell-tools.json before it fails at runtime inside the
// MCP server (where the error surfaces as a silent tool-less agent).
type shellToolsConfig struct {
	OutputCapBytes *int                         `json:"output_cap_bytes"`
	Tools          []map[string]json.RawMessage `json:"tools"`
}

// shellToolKeys is the complete field set of shell-mcp's toolDef. Anything
// else — most importantly the removed exec.*/argv form — is a violation.
var shellToolKeys = map[string]bool{
	"name":             true,
	"description":      true,
	"command":          true,
	"args":             true,
	"required":         true,
	"patterns":         true,
	"defaults":         true,
	"optional":         true,
	"timeout_seconds":  true,
	"output_cap_bytes": true,
}

var shellToolNameRe = regexp.MustCompile(`^[a-z][a-z0-9_]*$`)

// ValidateShellToolsJSON validates src as a shell-tools.json config.
// It checks: top-level shape, per-tool required fields (name, description,
// command), the removed exec/argv forms, unknown fields, RE2 pattern
// compilation, snake_case tool names, and duplicate names.
func ValidateShellToolsJSON(src string) []Violation {
	var vs []Violation

	var root map[string]json.RawMessage
	if err := json.Unmarshal([]byte(src), &root); err != nil {
		return []Violation{{Field: "(file)", Message: "invalid JSON: " + err.Error()}}
	}
	for k := range root {
		if k != "tools" && k != "output_cap_bytes" {
			vs = append(vs, Violation{Field: k, Message: "unknown top-level field (allowed: tools, output_cap_bytes)"})
		}
	}

	var cfg shellToolsConfig
	if err := json.Unmarshal([]byte(src), &cfg); err != nil {
		return append(vs, Violation{Field: "(file)", Message: "invalid config shape: " + err.Error()})
	}
	if cfg.OutputCapBytes != nil && *cfg.OutputCapBytes < 0 {
		vs = append(vs, Violation{Field: "output_cap_bytes", Message: "must be >= 0"})
	}
	if len(cfg.Tools) == 0 {
		return append(vs, Violation{Field: "tools", Message: "required field missing or empty list"})
	}

	seen := map[string]int{}
	for i, tool := range cfg.Tools {
		at := func(field string) string { return fmt.Sprintf("tools[%d].%s", i, field) }

		for k := range tool {
			if shellToolKeys[k] {
				continue
			}
			msg := "unknown field"
			if k == "exec" || k == "argv" || k == "shell" {
				msg = "removed exec.*/argv form; shell-mcp expects command/args"
			}
			vs = append(vs, Violation{Field: at(k), Message: msg})
		}

		name := stringField(tool, "name")
		switch {
		case name == "":
			vs = append(vs, Violation{Field: at("name"), Message: "required field missing"})
		case !shellToolNameRe.MatchString(name):
			vs = append(vs, Violation{Field: at("name"), Message: fmt.Sprintf("%q must be snake_case (%s)", name, shellToolNameRe)})
		default:
			if prev, dup := seen[name]; dup {
				vs = append(vs, Violation{Field: at("name"), Message: fmt.Sprintf("duplicate tool name %q (first at tools[%d])", name, prev)})
			} else {
				seen[name] = i
			}
		}
		if stringField(tool, "description") == "" {
			vs = append(vs, Violation{Field: at("description"), Message: "required field missing"})
		}
		if stringField(tool, "command") == "" {
			vs = append(vs, Violation{Field: at("command"), Message: "required field missing"})
		}

		if raw, ok := tool["args"]; ok {
			var args []string
			if err := json.Unmarshal(raw, &args); err != nil {
				vs = append(vs, Violation{Field: at("args"), Message: "must be a list of strings"})
			}
		}
		if raw, ok := tool["required"]; ok {
			var req []string
			if err := json.Unmarshal(raw, &req); err != nil {
				vs = append(vs, Violation{Field: at("required"), Message: "must be a list of strings"})
			}
		}
		if raw, ok := tool["patterns"]; ok {
			var pats map[string]string
			if err := json.Unmarshal(raw, &pats); err != nil {
				vs = append(vs, Violation{Field: at("patterns"), Message: "must be a map of string to RE2 regexp"})
			} else {
				for key, pat := range pats {
					if _, err := regexp.Compile(pat); err != nil {
						vs = append(vs, Violation{Field: at("patterns." + key), Message: "invalid RE2 regexp: " + err.Error()})
					}
				}
			}
		}
		if raw, ok := tool["defaults"]; ok {
			var defs map[string]string
			if err := json.Unmarshal(raw, &defs); err != nil {
				vs = append(vs, Violation{Field: at("defaults"), Message: "must be a map of string to string"})
			}
		}
		for _, intField := range []string{"timeout_seconds", "output_cap_bytes"} {
			if raw, ok := tool[intField]; ok {
				var n int
				if err := json.Unmarshal(raw, &n); err != nil || n < 0 {
					vs = append(vs, Violation{Field: at(intField), Message: "must be a non-negative integer"})
				}
			}
		}
		if raw, ok := tool["optional"]; ok {
			var b bool
			if err := json.Unmarshal(raw, &b); err != nil {
				vs = append(vs, Violation{Field: at("optional"), Message: "must be a boolean"})
			}
		}
	}
	return vs
}

// stringField extracts a string field from a raw-message map; returns "" when
// absent or not a string.
func stringField(m map[string]json.RawMessage, key string) string {
	raw, ok := m[key]
	if !ok {
		return ""
	}
	var s string
	if err := json.Unmarshal(raw, &s); err != nil {
		return ""
	}
	return s
}

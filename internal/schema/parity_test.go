package schema

import (
	"bufio"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// TestSchemaYAMLParity asserts that the hand-maintained editor schemas in
// schemas/*.schema.yaml agree with the runtime validators in defs.go — the
// drift class behind audit finding 2 (valid, runtime-supported fields
// flagged as errors in editors). Every defs.go field must appear as a
// top-level property in the corresponding YAML schema, and every YAML
// property must either be a defs.go field or a declared nested-only block
// (validated outside the flat-schema scope).
//
// The YAML schemas stay hand-written because their descriptions carry
// operator guidance codegen would flatten; this test is the "cannot
// diverge" guarantee codegen would otherwise provide.
func TestSchemaYAMLParity(t *testing.T) {
	cases := []struct {
		file     string
		schema   Schema
		yamlOnly []string // nested blocks or editor-only fields, by design
	}{
		{file: "agent-1.schema.yaml", schema: AgentFrontmatterSchema},
		{file: "lifecycle-1.schema.yaml", schema: LifecycleSchema,
			yamlOnly: []string{"cache", "output", "hooks", "instances"}},
		{file: "skill-1.schema.yaml", schema: SkillSchema,
			yamlOnly: []string{"extract"}},
		{file: "toolset-1.schema.yaml", schema: ToolsetSchema},
		{file: "worker-1.schema.yaml", schema: WorkerSchema,
			yamlOnly: []string{"headers", "output"}},
		{file: "config-1.schema.yaml", schema: ConfigSchema,
			yamlOnly: []string{"notify"}},
		{file: "curing-1.schema.yaml", schema: CuringSchema,
			yamlOnly: []string{"output"}},
		{file: "tannery-1.schema.yaml", schema: TanneryConfigSchema,
			yamlOnly: []string{"routes", "queues", "webhooks"}},
	}

	for _, tc := range cases {
		t.Run(tc.file, func(t *testing.T) {
			path := filepath.Join("..", "..", "schemas", tc.file)
			props, err := topLevelProperties(path)
			if err != nil {
				t.Fatalf("read %s: %v", tc.file, err)
			}
			allowed := map[string]bool{}
			for _, k := range tc.yamlOnly {
				allowed[k] = true
			}
			for field := range tc.schema {
				if !props[field] {
					t.Errorf("defs.go field %q missing from %s properties", field, tc.file)
				}
			}
			for prop := range props {
				if _, inDefs := tc.schema[prop]; !inDefs && !allowed[prop] {
					t.Errorf("%s property %q has no defs.go field and is not a declared nested-only block", tc.file, prop)
				}
			}
		})
	}
}

var rePropKey = regexp.MustCompile(`^  ([a-zA-Z_][a-zA-Z0-9_]*):`)

// topLevelProperties returns the set of two-space-indented keys under the
// column-zero `properties:` block of a JSON-schema YAML file. Deeper
// indentation (nested schemas) is ignored; the block ends at the next
// column-zero key.
func topLevelProperties(path string) (map[string]bool, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	props := map[string]bool{}
	in := false
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := sc.Text()
		switch {
		case line == "properties:":
			in = true
		case in && len(line) > 0 && !strings.HasPrefix(line, " ") && !strings.HasPrefix(line, "#"):
			in = false
		case in:
			if m := rePropKey.FindStringSubmatch(line); m != nil {
				props[m[1]] = true
			}
		}
	}
	return props, sc.Err()
}

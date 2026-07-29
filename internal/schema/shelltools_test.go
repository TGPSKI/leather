package schema

import (
	"strings"
	"testing"
)

func TestValidateShellToolsJSON_Valid(t *testing.T) {
	src := `{
	  "output_cap_bytes": 4000,
	  "tools": [
	    {
	      "name": "git_log",
	      "description": "Recent history",
	      "command": "git",
	      "args": ["log", "--oneline", "-n", "20", "{{ref}}"],
	      "defaults": {"ref": "HEAD"},
	      "patterns": {"ref": "^[A-Za-z0-9._/-]+$"},
	      "timeout_seconds": 5
	    }
	  ]
	}`
	if vs := ValidateShellToolsJSON(src); len(vs) != 0 {
		t.Fatalf("expected clean, got %v", vs)
	}
}

func TestValidateShellToolsJSON_RemovedExecForm(t *testing.T) {
	src := `{"tools":[{"name":"x","description":"d","exec":{"argv":["echo"]}}]}`
	vs := ValidateShellToolsJSON(src)
	var sawExec, sawCommand bool
	for _, v := range vs {
		if v.Field == "tools[0].exec" && strings.Contains(v.Message, "removed exec") {
			sawExec = true
		}
		if v.Field == "tools[0].command" {
			sawCommand = true
		}
	}
	if !sawExec || !sawCommand {
		t.Fatalf("want exec-form and missing-command violations, got %v", vs)
	}
}

func TestValidateShellToolsJSON_FieldChecks(t *testing.T) {
	src := `{
	  "tools": [
	    {"name": "Bad-Name", "description": "d", "command": "true"},
	    {"name": "dup", "description": "d", "command": "true"},
	    {"name": "dup", "description": "d", "command": "true",
	     "patterns": {"a": "["}, "timeout_seconds": -1, "unknown_key": 1}
	  ]
	}`
	vs := ValidateShellToolsJSON(src)
	want := []string{
		"tools[0].name",            // not snake_case
		"tools[2].name",            // duplicate
		"tools[2].patterns.a",      // invalid RE2
		"tools[2].timeout_seconds", // negative
		"tools[2].unknown_key",     // unknown field
	}
	got := map[string]bool{}
	for _, v := range vs {
		got[v.Field] = true
	}
	for _, f := range want {
		if !got[f] {
			t.Errorf("missing violation for %s; got %v", f, vs)
		}
	}
}

func TestValidateShellToolsJSON_NotJSON(t *testing.T) {
	vs := ValidateShellToolsJSON("not json")
	if len(vs) != 1 || vs[0].Field != "(file)" {
		t.Fatalf("want single (file) violation, got %v", vs)
	}
}

func TestValidateShellToolsJSON_EmptyTools(t *testing.T) {
	vs := ValidateShellToolsJSON(`{"tools": []}`)
	if len(vs) != 1 || vs[0].Field != "tools" {
		t.Fatalf("want tools required violation, got %v", vs)
	}
}

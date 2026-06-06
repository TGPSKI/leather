package schema

import (
	"testing"
)

// --- ValidateFlat ---

func TestValidateFlat_RequiredMissing(t *testing.T) {
	s := Schema{
		"agent": {Type: TypeString, Required: true},
	}
	vs := ValidateFlat(map[string]string{}, map[string][]string{}, nil, s)
	if len(vs) != 1 || vs[0].Field != "agent" {
		t.Fatalf("expected 1 violation on 'agent', got %v", vs)
	}
}

func TestValidateFlat_RequiredPresent(t *testing.T) {
	s := Schema{"agent": {Type: TypeString, Required: true}}
	vs := ValidateFlat(map[string]string{"agent": "my-agent"}, map[string][]string{}, nil, s)
	if len(vs) != 0 {
		t.Fatalf("expected no violations, got %v", vs)
	}
}

func TestValidateFlat_RequiredList(t *testing.T) {
	s := Schema{"tools": {IsList: true, Required: true}}
	// empty list → violation
	vs := ValidateFlat(map[string]string{}, map[string][]string{}, nil, s)
	if len(vs) != 1 || vs[0].Field != "tools" {
		t.Fatalf("expected 1 violation on 'tools', got %v", vs)
	}
	// non-empty list → ok
	vs = ValidateFlat(map[string]string{}, map[string][]string{"tools": {"t1"}}, nil, s)
	if len(vs) != 0 {
		t.Fatalf("expected no violations, got %v", vs)
	}
}

func TestValidateFlat_FieldTypes(t *testing.T) {
	tests := []struct {
		name    string
		field   string
		ft      FieldType
		val     string
		wantErr bool
	}{
		{"bool ok true", "f", TypeBoolean, "true", false},
		{"bool ok false", "f", TypeBoolean, "false", false},
		{"bool bad", "f", TypeBoolean, "yes", true},
		{"int ok", "f", TypeInteger, "42", false},
		{"int bad", "f", TypeInteger, "abc", true},
		{"number ok", "f", TypeNumber, "0.7", false},
		{"number bad", "f", TypeNumber, "nope", true},
		{"duration ok", "f", TypeDuration, "30s", false},
		{"duration ok h", "f", TypeDuration, "1h30m", false},
		{"duration bad", "f", TypeDuration, "300", true},
		{"cron ok", "f", TypeCron, "0 9 * * 1", false},
		{"cron once", "f", TypeCron, "once", false},
		{"cron bad", "f", TypeCron, "bad-value", true},
		{"enum ok", "f", TypeEnum, "http_poll", false},
		{"enum bad", "f", TypeEnum, "ftp_poll", true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			s := Schema{tc.field: {Type: tc.ft, AllowedValues: []string{"http_poll"}}}
			vs := ValidateFlat(map[string]string{tc.field: tc.val}, map[string][]string{}, nil, s)
			if tc.wantErr && len(vs) == 0 {
				t.Errorf("expected violation, got none")
			}
			if !tc.wantErr && len(vs) != 0 {
				t.Errorf("expected no violation, got %v", vs)
			}
		})
	}
}

func TestValidateFlat_IntegerBounds(t *testing.T) {
	s := Schema{
		"rounds": {Type: TypeInteger, HasMin: true, IntMin: 1, HasMax: true, IntMax: 20},
	}
	for _, tc := range []struct {
		val     string
		wantErr bool
	}{
		{"0", true},
		{"1", false},
		{"20", false},
		{"21", true},
	} {
		vs := ValidateFlat(map[string]string{"rounds": tc.val}, nil, nil, s)
		if tc.wantErr && len(vs) == 0 {
			t.Errorf("val=%q expected violation, got none", tc.val)
		}
		if !tc.wantErr && len(vs) != 0 {
			t.Errorf("val=%q expected no violation, got %v", tc.val, vs)
		}
	}
}

func TestValidateFlat_OptionalAbsent(t *testing.T) {
	// Optional fields with no value must produce no violations.
	s := Schema{
		"timeout":     {Type: TypeDuration},
		"temperature": {Type: TypeNumber},
		"tool_rounds": {Type: TypeInteger, HasMin: true, IntMin: 1},
	}
	vs := ValidateFlat(map[string]string{}, nil, nil, s)
	if len(vs) != 0 {
		t.Fatalf("expected no violations for absent optional fields, got %v", vs)
	}
}

// --- ValidateAgentFrontmatter ---

func TestValidateAgentFrontmatter_Valid(t *testing.T) {
	src := `
name: my-agent
schedule: 0 9 * * 1
model: llama3
temperature: 0.5
tool_rounds: 3
tags: [prod]
`
	vs := ValidateAgentFrontmatter(src)
	if len(vs) != 0 {
		t.Errorf("expected no violations, got %v", vs)
	}
}

func TestValidateAgentFrontmatter_BadToolRounds(t *testing.T) {
	src := `
name: my-agent
tool_rounds: abc
`
	vs := ValidateAgentFrontmatter(src)
	found := false
	for _, v := range vs {
		if v.Field == "tool_rounds" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected tool_rounds violation, got %v", vs)
	}
}

// --- ValidateLifecycleYAML ---

func TestValidateLifecycleYAML_Valid(t *testing.T) {
	src := `
agent: my-agent
schedule: "*/5 * * * *"
model: llama3
timeout: 60s
max_tokens: 4096
`
	vs := ValidateLifecycleYAML(src)
	if len(vs) != 0 {
		t.Errorf("expected no violations, got %v", vs)
	}
}

func TestValidateLifecycleYAML_MissingAgent(t *testing.T) {
	src := `schedule: "0 * * * *"`
	vs := ValidateLifecycleYAML(src)
	found := false
	for _, v := range vs {
		if v.Field == "agent" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected 'agent' required violation, got %v", vs)
	}
}

func TestValidateLifecycleYAML_BadTimeout(t *testing.T) {
	src := `
agent: my-agent
timeout: 300
`
	vs := ValidateLifecycleYAML(src)
	found := false
	for _, v := range vs {
		if v.Field == "timeout" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected timeout violation, got %v", vs)
	}
}

// --- ValidateWorkerYAML ---

func TestValidateWorkerYAML_Valid(t *testing.T) {
	src := `
name: issue-poller
type: http_poll
interval: 5m
url: https://api.example.com/issues
`
	vs := ValidateWorkerYAML(src)
	if len(vs) != 0 {
		t.Errorf("expected no violations, got %v", vs)
	}
}

func TestValidateWorkerYAML_BadType(t *testing.T) {
	src := `
name: issue-poller
type: ftp_poll
interval: 5m
url: https://api.example.com/issues
`
	vs := ValidateWorkerYAML(src)
	found := false
	for _, v := range vs {
		if v.Field == "type" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected 'type' enum violation, got %v", vs)
	}
}

func TestValidateWorkerYAML_MissingRequired(t *testing.T) {
	vs := ValidateWorkerYAML(`name: poller`)
	fields := make(map[string]bool)
	for _, v := range vs {
		fields[v.Field] = true
	}
	for _, required := range []string{"type", "interval", "url"} {
		if !fields[required] {
			t.Errorf("expected violation for required field %q, got %v", required, vs)
		}
	}
}

// --- ValidateSkillYAML ---

func TestValidateSkillYAML_Valid(t *testing.T) {
	src := `
name: github-issues
tools:
  - list-issues
`
	vs := ValidateSkillYAML(src)
	if len(vs) != 0 {
		t.Errorf("expected no violations, got %v", vs)
	}
}

func TestValidateSkillYAML_MissingName(t *testing.T) {
	src := `tools: [list-issues]`
	vs := ValidateSkillYAML(src)
	found := false
	for _, v := range vs {
		if v.Field == "name" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected 'name' required violation, got %v", vs)
	}
}

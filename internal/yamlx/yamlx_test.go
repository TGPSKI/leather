package yamlx

import (
	"strings"
	"testing"
)

func TestParseFlat_Scalars(t *testing.T) {
	input := `
# comment
agent_dir: /home/user/.leather/agents
log_level: debug
max_tokens: 4096
summarize_threshold: 0.75
api: true
llm_timeout: 30s
`
	vals, _, err := ParseFlat(strings.NewReader(input))
	if err != nil {
		t.Fatalf("ParseFlat: %v", err)
	}
	cases := map[string]string{
		"agent_dir":           "/home/user/.leather/agents",
		"log_level":           "debug",
		"max_tokens":          "4096",
		"summarize_threshold": "0.75",
		"api":                 "true",
		"llm_timeout":         "30s",
	}
	for key, want := range cases {
		if got := vals[key]; got != want {
			t.Errorf("vals[%q] = %q, want %q", key, got, want)
		}
	}
}

func TestParseFlat_InlineList(t *testing.T) {
	_, lists, err := ParseFlat(strings.NewReader(`tags: [alpha, beta, gamma]`))
	if err != nil {
		t.Fatalf("ParseFlat: %v", err)
	}
	got := lists["tags"]
	if len(got) != 3 || got[0] != "alpha" || got[1] != "beta" || got[2] != "gamma" {
		t.Errorf("tags = %v, want [alpha beta gamma]", got)
	}
}

func TestParseFlat_QuotedStrings(t *testing.T) {
	input := `
endpoint: "http://localhost:11434"
name: 'my-agent'
`
	vals, _, err := ParseFlat(strings.NewReader(input))
	if err != nil {
		t.Fatalf("ParseFlat: %v", err)
	}
	if got := vals["endpoint"]; got != "http://localhost:11434" {
		t.Errorf("endpoint = %q, want http://localhost:11434", got)
	}
	if got := vals["name"]; got != "my-agent" {
		t.Errorf("name = %q, want my-agent", got)
	}
}

func TestParseFlat_InlineComment(t *testing.T) {
	vals, _, err := ParseFlat(strings.NewReader(`max_tokens: 8192 # default`))
	if err != nil {
		t.Fatalf("ParseFlat: %v", err)
	}
	if got := vals["max_tokens"]; got != "8192" {
		t.Errorf("max_tokens = %q, want 8192", got)
	}
}

func TestParseBlock_Scalars(t *testing.T) {
	src := `
# comment
agent: my-agent
schedule: "0 * * * *"
model: llama3
max_tokens: 2048
temperature: 0.7
`
	vals, lists := ParseBlock(src)
	cases := map[string]string{
		"agent":       "my-agent",
		"schedule":    "0 * * * *",
		"model":       "llama3",
		"max_tokens":  "2048",
		"temperature": "0.7",
	}
	for key, want := range cases {
		if got := vals[key]; got != want {
			t.Errorf("vals[%q] = %q, want %q", key, got, want)
		}
	}
	if len(lists) != 0 {
		t.Errorf("expected no lists, got %v", lists)
	}
}

func TestParseBlock_FlowList(t *testing.T) {
	_, lists := ParseBlock(`tags: [alpha, beta, gamma]`)
	got := lists["tags"]
	want := []string{"alpha", "beta", "gamma"}
	if len(got) != len(want) {
		t.Fatalf("tags = %v, want %v", got, want)
	}
	for i, v := range want {
		if got[i] != v {
			t.Errorf("tags[%d] = %q, want %q", i, got[i], v)
		}
	}
}

func TestParseBlock_BlockStyleList(t *testing.T) {
	src := `
model: llama3
tags:
  - alpha
  - beta
  - gamma
schedule: "* * * * *"
`
	vals, lists := ParseBlock(src)
	if got := vals["model"]; got != "llama3" {
		t.Errorf("model = %q, want llama3", got)
	}
	if got := vals["schedule"]; got != "* * * * *" {
		t.Errorf("schedule = %q, want '* * * * *'", got)
	}
	got := lists["tags"]
	want := []string{"alpha", "beta", "gamma"}
	if len(got) != len(want) {
		t.Fatalf("tags = %v, want %v", got, want)
	}
	for i, v := range want {
		if got[i] != v {
			t.Errorf("tags[%d] = %q, want %q", i, got[i], v)
		}
	}
}

func TestParseBlock_BlockStyleListAtColumnZero(t *testing.T) {
	// Block-style list items at column 0 (pre-trimmed content).
	src := "tags:\n- alpha\n- beta\nschedule: \"* * * * *\""
	vals, lists := ParseBlock(src)
	if got := vals["schedule"]; got != "* * * * *" {
		t.Errorf("schedule = %q, want '* * * * *'", got)
	}
	got := lists["tags"]
	want := []string{"alpha", "beta"}
	if len(got) != len(want) {
		t.Fatalf("tags = %v, want %v", got, want)
	}
	for i, v := range want {
		if got[i] != v {
			t.Errorf("tags[%d] = %q, want %q", i, got[i], v)
		}
	}
}

func TestParseBlock_FlowListDisable(t *testing.T) {
	_, lists := ParseBlock(`disable: [foo, bar]`)
	got := lists["disable"]
	want := []string{"foo", "bar"}
	if len(got) != len(want) {
		t.Fatalf("disable = %v, want %v", got, want)
	}
	for i, v := range want {
		if got[i] != v {
			t.Errorf("disable[%d] = %q, want %q", i, got[i], v)
		}
	}
}

func TestParseBlock_BoolDisable(t *testing.T) {
	vals, lists := ParseBlock(`disable: true`)
	if got := vals["disable"]; got != "true" {
		t.Errorf("disable = %q, want true", got)
	}
	if len(lists["disable"]) != 0 {
		t.Errorf("expected no disable list, got %v", lists["disable"])
	}
}

func TestParseBlock_QuotedValues(t *testing.T) {
	src := `
endpoint: "http://localhost:11434"
name: 'my-agent'
tag: "5m"
`
	vals, _ := ParseBlock(src)
	if got := vals["endpoint"]; got != "http://localhost:11434" {
		t.Errorf("endpoint = %q, want http://localhost:11434", got)
	}
	if got := vals["name"]; got != "my-agent" {
		t.Errorf("name = %q, want my-agent", got)
	}
	if got := vals["tag"]; got != "5m" {
		t.Errorf("tag = %q, want 5m", got)
	}
}

func TestParseBlock_InlineComment(t *testing.T) {
	vals, _ := ParseBlock(`max_tokens: 8192 # default`)
	if got := vals["max_tokens"]; got != "8192" {
		t.Errorf("max_tokens = %q, want 8192", got)
	}
}

func TestStripQuotes(t *testing.T) {
	tests := []struct {
		name, in, want string
	}{
		{name: "double", in: `"hi"`, want: "hi"},
		{name: "single", in: `'hi'`, want: "hi"},
		{name: "none", in: "hi", want: "hi"},
		{name: "mismatched", in: `"hi'`, want: `"hi'`},
		{name: "empty", in: "", want: ""},
		{name: "single char", in: `"`, want: `"`},
		{name: "inner quotes kept", in: `a"b`, want: `a"b`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := StripQuotes(tt.in); got != tt.want {
				t.Errorf("StripQuotes(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}

func TestSplitKV(t *testing.T) {
	tests := []struct {
		name         string
		line         string
		wantK, wantV string
		wantOK       bool
	}{
		{name: "simple", line: "key: value", wantK: "key", wantV: "value", wantOK: true},
		{name: "quoted value", line: `name: "my-agent"`, wantK: "name", wantV: "my-agent", wantOK: true},
		{name: "single quoted", line: "name: 'x'", wantK: "name", wantV: "x", wantOK: true},
		{name: "inline comment", line: "port: 8080 # default", wantK: "port", wantV: "8080", wantOK: true},
		{name: "no colon", line: "noseparator", wantOK: false},
		{name: "empty key", line: ": value", wantOK: false},
		{name: "empty value", line: "key:", wantK: "key", wantV: "", wantOK: true},
		{name: "value with colon", line: "url: http://x:11434", wantK: "url", wantV: "http://x:11434", wantOK: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			k, v, ok := SplitKV(tt.line)
			if ok != tt.wantOK {
				t.Fatalf("ok = %v, want %v", ok, tt.wantOK)
			}
			if !ok {
				return
			}
			if k != tt.wantK || v != tt.wantV {
				t.Errorf("SplitKV(%q) = (%q, %q), want (%q, %q)", tt.line, k, v, tt.wantK, tt.wantV)
			}
		})
	}
}

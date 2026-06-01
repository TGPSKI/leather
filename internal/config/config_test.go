package config

import (
	"flag"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestParseYAML_Scalars(t *testing.T) {
	input := `
# comment
agent_dir: /home/user/.leather/agents
log_level: debug
max_tokens: 4096
summarize_threshold: 0.75
api: true
llm_timeout: 30s
`
	vals, _, err := parseYAML(strings.NewReader(input))
	if err != nil {
		t.Fatalf("parseYAML: %v", err)
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

func TestParseYAML_InlineList(t *testing.T) {
	input := `tags: [alpha, beta, gamma]`
	_, lists, err := parseYAML(strings.NewReader(input))
	if err != nil {
		t.Fatalf("parseYAML: %v", err)
	}
	got := lists["tags"]
	if len(got) != 3 || got[0] != "alpha" || got[1] != "beta" || got[2] != "gamma" {
		t.Errorf("tags = %v, want [alpha beta gamma]", got)
	}
}

func TestParseYAML_QuotedStrings(t *testing.T) {
	input := `
endpoint: "http://localhost:11434"
name: 'my-agent'
`
	vals, _, err := parseYAML(strings.NewReader(input))
	if err != nil {
		t.Fatalf("parseYAML: %v", err)
	}
	if got := vals["endpoint"]; got != "http://localhost:11434" {
		t.Errorf("endpoint = %q, want http://localhost:11434", got)
	}
	if got := vals["name"]; got != "my-agent" {
		t.Errorf("name = %q, want my-agent", got)
	}
}

func TestParseYAML_InlineComment(t *testing.T) {
	input := `max_tokens: 8192 # default`
	vals, _, err := parseYAML(strings.NewReader(input))
	if err != nil {
		t.Fatalf("parseYAML: %v", err)
	}
	if got := vals["max_tokens"]; got != "8192" {
		t.Errorf("max_tokens = %q, want 8192", got)
	}
}

func TestApplyYAML_OverridesConfig(t *testing.T) {
	input := `
log_level: warn
max_tokens: 2048
llm_endpoint: http://remote:8080
`
	cfg, err := Load(flag.NewFlagSet("test", flag.ContinueOnError))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	cfg.MaxTokens = DefaultMaxTokens // reset to default
	if err := applyYAML(strings.NewReader(input), &cfg); err != nil {
		t.Fatalf("applyYAML: %v", err)
	}
	if cfg.MaxTokens != 2048 {
		t.Errorf("MaxTokens = %d, want 2048", cfg.MaxTokens)
	}
	if cfg.LLMEndpoint != "http://remote:8080" {
		t.Errorf("LLMEndpoint = %q, want http://remote:8080", cfg.LLMEndpoint)
	}
}

func TestLoad_Defaults(t *testing.T) {
	// Unset all LEATHER_* vars that might be set in the environment.
	for _, key := range []string{
		"LEATHER_MAX_TOKENS", "LEATHER_LOG_LEVEL", "LEATHER_LLM_ENDPOINT",
		"LEATHER_COMPLETION_RESERVE", "LEATHER_SUMMARIZE_THRESHOLD",
		"LEATHER_MAX_CONCURRENT_JOBS", "LEATHER_API",
	} {
		t.Setenv(key, "")
	}

	fs := flag.NewFlagSet("test", flag.ContinueOnError)
	BindFlags(fs)
	cfg, err := Load(fs)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.MaxTokens != DefaultMaxTokens {
		t.Errorf("MaxTokens = %d, want %d", cfg.MaxTokens, DefaultMaxTokens)
	}
	if cfg.LLMEndpoint != DefaultLLMEndpoint {
		t.Errorf("LLMEndpoint = %q, want %q", cfg.LLMEndpoint, DefaultLLMEndpoint)
	}
}

func TestLoad_EnvOverride(t *testing.T) {
	t.Setenv("LEATHER_MAX_TOKENS", "4096")
	t.Setenv("LEATHER_LOG_LEVEL", "debug")

	fs := flag.NewFlagSet("test", flag.ContinueOnError)
	BindFlags(fs)
	cfg, err := Load(fs)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.MaxTokens != 4096 {
		t.Errorf("MaxTokens = %d, want 4096", cfg.MaxTokens)
	}
	if cfg.LogLevel != "debug" {
		t.Errorf("LogLevel = %q, want debug", cfg.LogLevel)
	}
}

func TestLoad_YAMLFile(t *testing.T) {
	dir := t.TempDir()
	cfgFile := dir + "/config.yaml"
	content := "max_tokens: 1024\nllm_endpoint: http://localtest:9999\n"
	if err := os.WriteFile(cfgFile, []byte(content), 0600); err != nil {
		t.Fatal(err)
	}

	t.Setenv("LEATHER_CONFIG", cfgFile)
	t.Setenv("LEATHER_MAX_TOKENS", "") // clear env so YAML wins

	fs := flag.NewFlagSet("test", flag.ContinueOnError)
	BindFlags(fs)
	cfg, err := Load(fs)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.MaxTokens != 1024 {
		t.Errorf("MaxTokens = %d, want 1024", cfg.MaxTokens)
	}
	if cfg.LLMEndpoint != "http://localtest:9999" {
		t.Errorf("LLMEndpoint = %q, want http://localtest:9999", cfg.LLMEndpoint)
	}
}

func TestLoad_EnvShowContextOverridesYAML(t *testing.T) {
	dir := t.TempDir()
	cfgFile := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(cfgFile, []byte("show_context: true\n"), 0600); err != nil {
		t.Fatal(err)
	}

	t.Setenv("LEATHER_CONFIG", cfgFile)
	t.Setenv("LEATHER_SHOW_CONTEXT", "0")

	fs := flag.NewFlagSet("test", flag.ContinueOnError)
	BindFlags(fs)
	cfg, err := Load(fs)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.ShowContext {
		t.Fatal("ShowContext = true, want false from LEATHER_SHOW_CONTEXT=0 overriding YAML")
	}
}

// --- ParseBlock ---

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
	src := `tags: [alpha, beta, gamma]`
	_, lists := ParseBlock(src)
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
	// Block-style list items at column 0 (pre-trimmed content from splitInstanceBlocks).
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
	// disable: [name, ...] parses as a list.
	src := `disable: [foo, bar]`
	_, lists := ParseBlock(src)
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
	// disable: true parses as a scalar.
	src := `disable: true`
	vals, lists := ParseBlock(src)
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
	src := `max_tokens: 8192 # default`
	vals, _ := ParseBlock(src)
	if got := vals["max_tokens"]; got != "8192" {
		t.Errorf("max_tokens = %q, want 8192", got)
	}
}

// --- expandHome ---

func TestExpandHome(t *testing.T) {
	home, err := os.UserHomeDir()
	if err != nil {
		t.Skip("no home directory available")
	}
	cases := []struct {
		name  string
		input string
		want  string
	}{
		{"tilde prefix", "~/foo/bar", filepath.Join(home, "foo/bar")},
		{"absolute path unchanged", "/absolute/path", "/absolute/path"},
		{"relative path unchanged", "relative/path", "relative/path"},
		{"empty string unchanged", "", ""},
		{"tilde without slash unchanged", "~foo", "~foo"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := expandHome(tc.input)
			if got != tc.want {
				t.Errorf("expandHome(%q) = %q, want %q", tc.input, got, tc.want)
			}
		})
	}
}

// --- applyFlag (exercised via Load with explicitly-set flags) ---

func TestLoad_FlagOverrides(t *testing.T) {
	dir := t.TempDir()
	// Clear env vars so flags are the decisive layer.
	for _, key := range []string{
		"LEATHER_MAX_TOKENS", "LEATHER_LOG_LEVEL", "LEATHER_LLM_ENDPOINT",
		"LEATHER_TEMPERATURE", "LEATHER_LOG_FORMAT", "LEATHER_API",
		"LEATHER_COMPLETION_RESERVE", "LEATHER_SUMMARIZE_THRESHOLD",
		"LEATHER_LLM_TIMEOUT", "LEATHER_SCHEDULER_TICK", "LEATHER_MAX_CONCURRENT_JOBS",
		"LEATHER_RUN_DURATION", "LEATHER_MAX_JOBS", "LEATHER_STATE_DIR",
		"LEATHER_API_ADDR", "LEATHER_LOG_FILE", "LEATHER_PRETTY", "LEATHER_PRETTY_MODE", "LEATHER_STATS",
		"LEATHER_DEFAULT_TOOLSETS",
		"LEATHER_MODEL", "LEATHER_AGENT_DIR",
	} {
		t.Setenv(key, "")
	}

	fs := flag.NewFlagSet("test", flag.ContinueOnError)
	BindFlags(fs)
	if err := fs.Parse([]string{
		"--config", filepath.Join(dir, "nonexistent.yaml"), // no YAML loaded
		"--max-tokens", "1234",
		"--log-level", "debug",
		"--llm-endpoint", "http://flag-host:9999",
		"--temperature", "0.42",
		"--log-format", "json",
		"--api",
		"--completion-reserve", "512",
		"--summarize-threshold", "0.6",
		"--llm-timeout", "45s",
		"--scheduler-tick", "2m",
		"--max-concurrent-jobs", "8",
		"--run-duration", "1h",
		"--max-jobs", "100",
		"--state-dir", dir,
		"--api-addr", "127.0.0.1:8888",
		"--log-file", "/tmp/test.log",
		"--pretty",
		"--pretty-mode", "all",
		"--stats",
		"--default-toolsets", "release-read,release-write",
		"--model", "llama3-test",
		"--agent-dir", dir,
	}); err != nil {
		t.Fatalf("fs.Parse: %v", err)
	}

	cfg, err := Load(fs)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	cases := []struct {
		name string
		got  any
		want any
	}{
		{"MaxTokens", cfg.MaxTokens, 1234},
		{"LogLevel", string(cfg.LogLevel), "debug"},
		{"LLMEndpoint", cfg.LLMEndpoint, "http://flag-host:9999"},
		{"Temperature", cfg.Temperature, 0.42},
		{"LogFormat", cfg.LogFormat, "json"},
		{"API", cfg.API, true},
		{"CompletionReserve", cfg.CompletionReserve, 512},
		{"SummarizeThreshold", cfg.SummarizeThreshold, 0.6},
		{"LLMTimeout", cfg.LLMTimeout, 45 * time.Second},
		{"SchedulerTick", cfg.SchedulerTick, 2 * time.Minute},
		{"MaxConcurrentJobs", cfg.MaxConcurrentJobs, 8},
		{"RunDuration", cfg.RunDuration, time.Hour},
		{"MaxJobs", cfg.MaxJobs, 100},
		{"StateDir", cfg.StateDir, dir},
		{"APIAddr", cfg.APIAddr, "127.0.0.1:8888"},
		{"LogFile", cfg.LogFile, "/tmp/test.log"},
		{"Pretty", cfg.Pretty, true},
		{"PrettyMode", cfg.PrettyMode, "all"},
		{"Stats", cfg.Stats, true},
		{"DefaultToolsetsLen", len(cfg.DefaultToolsets), 2},
		{"Model", cfg.Model, "llama3-test"},
		{"AgentDir", cfg.AgentDir, dir},
	}
	for _, tc := range cases {
		if tc.got != tc.want {
			t.Errorf("%s: got %v, want %v", tc.name, tc.got, tc.want)
		}
	}
	if got, want := cfg.DefaultToolsets[0], "release-read"; got != want {
		t.Errorf("DefaultToolsets[0] = %q, want %q", got, want)
	}
	if got, want := cfg.DefaultToolsets[1], "release-write"; got != want {
		t.Errorf("DefaultToolsets[1] = %q, want %q", got, want)
	}
}

// --- env helper error paths ---

func TestEnvFloat_InvalidFallsBackToDefault(t *testing.T) {
	t.Setenv("LEATHER_TEMPERATURE", "not-a-float")
	got := envFloat("TEMPERATURE", 1.5)
	if got != 1.5 {
		t.Errorf("envFloat with invalid value = %v, want 1.5", got)
	}
}

func TestEnvBool_InvalidFallsBackToDefault(t *testing.T) {
	t.Setenv("LEATHER_API", "not-a-bool")
	got := envBool("API", true)
	if !got {
		t.Errorf("envBool with invalid value = %v, want true", got)
	}
}

func TestEnvDuration_InvalidFallsBackToDefault(t *testing.T) {
	t.Setenv("LEATHER_LLM_TIMEOUT", "not-a-duration")
	got := envDuration("LLM_TIMEOUT", 30*time.Second)
	if got != 30*time.Second {
		t.Errorf("envDuration with invalid value = %v, want 30s", got)
	}
}

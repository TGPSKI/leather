package cli

import (
	"bytes"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/TGPSKI/leather/internal/config"
	"github.com/TGPSKI/leather/internal/model"
)

// --- redact ---

func TestRedact_EmptyReturnsEmpty(t *testing.T) {
	if got := redact(""); got != "" {
		t.Errorf("redact(\"\") = %q, want \"\"", got)
	}
}

func TestRedact_ShortKeyFullyMasked(t *testing.T) {
	got := redact("abc")
	if got != "****" {
		t.Errorf("redact(\"abc\") = %q, want \"****\"", got)
	}
}

func TestRedact_LongKeyShowsPrefix(t *testing.T) {
	got := redact("sk-supersecret")
	if !strings.HasPrefix(got, "sk-s") {
		t.Errorf("redact(\"sk-supersecret\") = %q, want prefix \"sk-s\"", got)
	}
	if strings.Contains(got, "supersecret") {
		t.Errorf("redact should not expose full secret: %q", got)
	}
}

// --- buildDoctorRows ---

func TestBuildDoctorRows_LLMAPIKeyRedacted(t *testing.T) {
	cfg := model.Config{
		LLMAPIKey: "sk-abc1234",
		Sources:   map[string]string{"llm_api_key": "env"},
	}
	rows := buildDoctorRows(cfg)

	var apiKeyRow *doctorField
	for i := range rows {
		if rows[i].name == "llm_api_key" {
			apiKeyRow = &rows[i]
			break
		}
	}
	if apiKeyRow == nil {
		t.Fatal("llm_api_key row not found in doctor output")
	}
	if strings.Contains(apiKeyRow.value, "abc1234") {
		t.Errorf("llm_api_key value %q exposes secret", apiKeyRow.value)
	}
	if apiKeyRow.source != "env" {
		t.Errorf("source = %q, want env", apiKeyRow.source)
	}
}

func TestBuildDoctorRows_DefaultSourceLabel(t *testing.T) {
	cfg := model.Config{
		LLMEndpoint:       config.DefaultLLMEndpoint,
		MaxTokens:         config.DefaultMaxTokens,
		MaxConcurrentJobs: config.DefaultMaxConcurrentJobs,
	}
	rows := buildDoctorRows(cfg)

	defaults := map[string]bool{
		"llm_endpoint":        true,
		"max_tokens":          true,
		"max_concurrent_jobs": true,
	}
	for _, r := range rows {
		if defaults[r.name] && r.source != "default" {
			t.Errorf("row %q: source = %q, want \"default\"", r.name, r.source)
		}
	}
}

func TestBuildDoctorRows_OverriddenSourceLabel(t *testing.T) {
	cfg := model.Config{
		LLMEndpoint: "http://custom-endpoint:8080",
		MaxTokens:   999,
		LogFormat:   "json",
		Sources: map[string]string{
			"llm_endpoint": "yaml",
			"max_tokens":   "env",
			"log_format":   "flag",
		},
	}
	rows := buildDoctorRows(cfg)

	want := map[string]string{
		"llm_endpoint": "yaml",
		"max_tokens":   "env",
		"log_format":   "flag",
	}
	for _, r := range rows {
		if w, ok := want[r.name]; ok && r.source != w {
			t.Errorf("row %q: source = %q, want %q", r.name, r.source, w)
		}
	}
}

// TestRunDoctor_SourceAttribution goes through the real config.Load: the
// reported source must name the layer that actually supplied the value,
// including a YAML value equal to the built-in default (the issue #31
// failure: doctor labelled such values "default" and fabricated a config
// problem that cost real debugging time).
func TestRunDoctor_SourceAttribution(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "config.yaml")
	// completion_reserve deliberately equals DefaultCompletionReserve.
	yaml := "max_tokens: 24000\ncompletion_reserve: 1024\n"
	if err := os.WriteFile(cfgPath, []byte(yaml), 0644); err != nil {
		t.Fatal(err)
	}
	// Pin the env layer: one var set, the yaml-covered ones cleared.
	t.Setenv("LEATHER_LOG_LEVEL", "debug")
	t.Setenv("LEATHER_MAX_TOKENS", "")
	t.Setenv("LEATHER_COMPLETION_RESERVE", "")

	var out bytes.Buffer
	if code := RunDoctor([]string{"--config", cfgPath, "--api-addr", "127.0.0.1:9999"}, &out, io.Discard); code != 0 {
		t.Fatalf("exit code = %d, want 0", code)
	}

	want := map[string]string{
		"max_tokens":         "24000.*yaml",
		"completion_reserve": "1024.*yaml", // equal to default, still yaml
		"log_level":          "debug.*env",
		"api_addr":           "127.0.0.1:9999.*flag",
		"scheduler_tick":     "default", // untouched everywhere
	}
	for key, pat := range want {
		re := regexp.MustCompile(`(?m)^` + key + `\s+.*` + pat)
		if !re.MatchString(out.String()) {
			t.Errorf("doctor output missing %q matching %q\n%s", key, pat, out.String())
		}
	}
}

// --- RunDoctor ---

func TestRunDoctor_ExitsZeroWithDefaultConfig(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(cfgPath, []byte("agent_dir: agents\n"), 0644); err != nil {
		t.Fatal(err)
	}

	var out bytes.Buffer
	code := RunDoctor([]string{"--config", cfgPath}, &out, io.Discard)
	if code != 0 {
		t.Errorf("exit code = %d, want 0", code)
	}
}

func TestRunDoctor_OutputContainsHeaderAndRows(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(cfgPath, []byte(""), 0644); err != nil {
		t.Fatal(err)
	}

	var out bytes.Buffer
	RunDoctor([]string{"--config", cfgPath}, &out, io.Discard)

	stdout := out.String()
	for _, want := range []string{"KEY", "VALUE", "SOURCE", "llm_endpoint", "model", "log_level"} {
		if !strings.Contains(stdout, want) {
			t.Errorf("output missing %q\nfull:\n%s", want, stdout)
		}
	}
}

func TestRunDoctor_SecretRedactedInOutput(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(cfgPath, []byte(""), 0644); err != nil {
		t.Fatal(err)
	}

	t.Setenv("LEATHER_LLM_API_KEY", "sk-topsecret999")

	var out bytes.Buffer
	RunDoctor([]string{"--config", cfgPath}, &out, io.Discard)

	stdout := out.String()
	if strings.Contains(stdout, "topsecret999") {
		t.Errorf("doctor output exposes raw API key:\n%s", stdout)
	}
	if !strings.Contains(stdout, "sk-t") {
		t.Errorf("doctor output missing masked prefix:\n%s", stdout)
	}
}

func TestRunDoctor_ConfigFileValuesAppearInOutput(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(cfgPath, []byte("llm_endpoint: http://myhost:9999\nlog_format: json\n"), 0644); err != nil {
		t.Fatal(err)
	}

	var out bytes.Buffer
	code := RunDoctor([]string{"--config", cfgPath}, &out, io.Discard)
	if code != 0 {
		t.Fatalf("exit code = %d", code)
	}

	stdout := out.String()
	if !strings.Contains(stdout, "http://myhost:9999") {
		t.Errorf("output missing configured llm_endpoint:\n%s", stdout)
	}
	if !strings.Contains(stdout, "json") {
		t.Errorf("output missing configured log_format:\n%s", stdout)
	}
}

func TestRun_Doctor_Dispatches(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(cfgPath, []byte(""), 0644); err != nil {
		t.Fatal(err)
	}

	var out bytes.Buffer
	code := Run([]string{"doctor", "--config", cfgPath}, &out, io.Discard, "dev", "none")
	if code != 0 {
		t.Errorf("Run(doctor) exit code = %d", code)
	}
}

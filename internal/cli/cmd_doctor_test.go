package cli

import (
	"bytes"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/tgpski/leather/internal/config"
	"github.com/tgpski/leather/internal/model"
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
	cfg := model.Config{LLMAPIKey: "sk-abc1234"}
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
	if apiKeyRow.source != "config/env/flag" {
		t.Errorf("source = %q, want config/env/flag", apiKeyRow.source)
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
	}
	rows := buildDoctorRows(cfg)

	overridden := map[string]bool{
		"llm_endpoint": true,
		"max_tokens":   true,
		"log_format":   true,
	}
	for _, r := range rows {
		if overridden[r.name] && r.source != "config/env/flag" {
			t.Errorf("row %q: source = %q, want \"config/env/flag\"", r.name, r.source)
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

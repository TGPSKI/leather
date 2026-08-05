package cli

import (
	"fmt"
	"io"
	"strings"

	"github.com/TGPSKI/leather/internal/config"
	"github.com/TGPSKI/leather/internal/model"
)

// doctorField is one row in the doctor output table.
type doctorField struct {
	name   string
	value  string
	source string
}

// redact replaces a non-empty secret value with a masked string that shows
// only the first four characters to confirm which credential is loaded without
// revealing the full token.
func redact(s string) string {
	if s == "" {
		return ""
	}
	if len(s) <= 4 {
		return "****"
	}
	return s[:4] + strings.Repeat("*", len(s)-4)
}

// RunDoctor prints the effective configuration with source attribution and
// redacts secret-bearing values.
//
// Usage: leather doctor [flags]
func RunDoctor(args []string, stdout, stderr io.Writer) int {
	fs := newFlagSet("doctor", stderr)
	config.BindFlags(fs)
	if !parseFlags(fs, args) {
		return 2
	}

	cfg, err := config.Load(fs)
	if err != nil {
		fmt.Fprintf(stderr, "leather doctor: %v\n", err)
		return 1
	}

	rows := buildDoctorRows(cfg)
	printDoctorTable(stdout, rows)
	return 0
}

// buildDoctorRows converts a resolved Config into a labelled, redacted row list.
func buildDoctorRows(cfg model.Config) []doctorField {
	// src reports the layer that actually supplied the key's value, recorded
	// during config.Load (issue #31: the old val != default heuristic labelled
	// explicitly-set-to-default values "default" and could not say which layer
	// won, which sent real debugging sessions in the wrong direction).
	src := func(key string) string {
		if s, ok := cfg.Sources[key]; ok {
			return s
		}
		return "default"
	}

	return []doctorField{
		// -- identity --
		{"config_file", cfg.ConfigFile, src("config_file")},
		{"agent_dir", cfg.AgentDir, src("agent_dir")},
		{"state_dir", cfg.StateDir, src("state_dir")},

		// -- model --
		{"model", cfg.Model, src("model")},
		{"temperature", fmt.Sprintf("%.2g", cfg.Temperature), src("temperature")},
		{"llm_endpoint", cfg.LLMEndpoint, src("llm_endpoint")},
		{"llm_timeout", cfg.LLMTimeout.String(), src("llm_timeout")},
		{"tool_timeout", cfg.ToolTimeout.String(), src("tool_timeout")},
		{"llm_api_key", redact(cfg.LLMAPIKey), src("llm_api_key")},

		// -- token budget --
		{"max_tokens", fmt.Sprintf("%d", cfg.MaxTokens), src("max_tokens")},
		{"completion_reserve", fmt.Sprintf("%d", cfg.CompletionReserve), src("completion_reserve")},
		{"reasoning_reserve", fmt.Sprintf("%d", cfg.ReasoningReserve), src("reasoning_reserve")},
		{"summarize_threshold", fmt.Sprintf("%.2f", cfg.SummarizeThreshold), src("summarize_threshold")},

		// -- scheduler --
		{"scheduler_tick", cfg.SchedulerTick.String(), src("scheduler_tick")},
		{"max_concurrent_jobs", fmt.Sprintf("%d", cfg.MaxConcurrentJobs), src("max_concurrent_jobs")},
		{"max_tool_rounds", fmt.Sprintf("%d", cfg.MaxToolRounds), src("max_tool_rounds")},

		// -- logging --
		{"log_level", string(cfg.LogLevel), src("log_level")},
		{"log_format", cfg.LogFormat, src("log_format")},
		{"log_file", cfg.LogFile, src("log_file")},

		// -- directories --
		{"tool_dir", cfg.ToolDir, src("tool_dir")},
		{"worker_dir", cfg.WorkerDir, src("worker_dir")},
		{"cache_dir", cfg.CacheDir, src("cache_dir")},
		{"mcp_servers_file", cfg.MCPServersFile, src("mcp_servers_file")},
		{"tannery", cfg.TanneryFile, src("tannery")},

		// -- API --
		{"api", fmt.Sprintf("%v", cfg.API), src("api")},
		{"api_addr", cfg.APIAddr, src("api_addr")},
	}
}

// printDoctorTable writes aligned columns to w.
func printDoctorTable(w io.Writer, rows []doctorField) {
	const nameWidth = 22
	const valWidth = 40

	fmt.Fprintf(w, "%-*s  %-*s  %s\n", nameWidth, "KEY", valWidth, "VALUE", "SOURCE")
	fmt.Fprintf(w, "%s  %s  %s\n",
		strings.Repeat("-", nameWidth),
		strings.Repeat("-", valWidth),
		strings.Repeat("-", 16))

	for _, r := range rows {
		val := r.value
		if val == "" {
			val = "(empty)"
		}
		fmt.Fprintf(w, "%-*s  %-*s  %s\n", nameWidth, r.name, valWidth, val, r.source)
	}
}

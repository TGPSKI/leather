package cli

import (
	"fmt"
	"io"
	"strings"

	"github.com/tgpski/leather/internal/config"
	"github.com/tgpski/leather/internal/model"
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
	// source attribution helpers
	src := func(val, def string) string {
		if val != def {
			return "config/env/flag"
		}
		return "default"
	}
	srcBool := func(val, def bool) string {
		if val != def {
			return "config/env/flag"
		}
		return "default"
	}
	srcInt := func(val, def int) string {
		if val != def {
			return "config/env/flag"
		}
		return "default"
	}

	return []doctorField{
		// -- identity --
		{"config_file", cfg.ConfigFile, src(cfg.ConfigFile, "")},
		{"agent_dir", cfg.AgentDir, src(cfg.AgentDir, "")},
		{"state_dir", cfg.StateDir, src(cfg.StateDir, "")},

		// -- model --
		{"model", cfg.Model, src(cfg.Model, "")},
		{"llm_endpoint", cfg.LLMEndpoint, src(cfg.LLMEndpoint, config.DefaultLLMEndpoint)},
		{"llm_timeout", cfg.LLMTimeout.String(), src(cfg.LLMTimeout.String(), config.DefaultLLMTimeout.String())},
		{"llm_api_key", redact(cfg.LLMAPIKey), src(cfg.LLMAPIKey, "")},

		// -- token budget --
		{"max_tokens", fmt.Sprintf("%d", cfg.MaxTokens), srcInt(cfg.MaxTokens, config.DefaultMaxTokens)},
		{"completion_reserve", fmt.Sprintf("%d", cfg.CompletionReserve), srcInt(cfg.CompletionReserve, config.DefaultCompletionReserve)},
		{"summarize_threshold", fmt.Sprintf("%.2f", cfg.SummarizeThreshold), src(fmt.Sprintf("%.2f", cfg.SummarizeThreshold), fmt.Sprintf("%.2f", config.DefaultSummarizeThreshold))},

		// -- scheduler --
		{"scheduler_tick", cfg.SchedulerTick.String(), src(cfg.SchedulerTick.String(), config.DefaultSchedulerTick.String())},
		{"max_concurrent_jobs", fmt.Sprintf("%d", cfg.MaxConcurrentJobs), srcInt(cfg.MaxConcurrentJobs, config.DefaultMaxConcurrentJobs)},
		{"max_tool_rounds", fmt.Sprintf("%d", cfg.MaxToolRounds), srcInt(cfg.MaxToolRounds, config.DefaultMaxToolRounds)},

		// -- logging --
		{"log_level", string(cfg.LogLevel), src(string(cfg.LogLevel), config.DefaultLogLevel)},
		{"log_format", cfg.LogFormat, src(cfg.LogFormat, config.DefaultLogFormat)},
		{"log_file", cfg.LogFile, src(cfg.LogFile, "")},

		// -- directories --
		{"tool_dir", cfg.ToolDir, src(cfg.ToolDir, "")},
		{"worker_dir", cfg.WorkerDir, src(cfg.WorkerDir, "")},
		{"cache_dir", cfg.CacheDir, src(cfg.CacheDir, "")},
		{"mcp_servers_file", cfg.MCPServersFile, src(cfg.MCPServersFile, "")},
		{"tannery", cfg.TanneryFile, src(cfg.TanneryFile, "")},

		// -- API --
		{"api", fmt.Sprintf("%v", cfg.API), srcBool(cfg.API, config.DefaultAPI)},
		{"api_addr", cfg.APIAddr, src(cfg.APIAddr, config.DefaultAPIAddr)},
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

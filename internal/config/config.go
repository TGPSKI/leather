package config

import (
	"bytes"
	"context"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/tgpski/leather/internal/model"
	"github.com/tgpski/leather/internal/secret"
	"github.com/tgpski/leather/internal/yamlx"
)

// Load merges configuration from (highest to lowest priority):
//
//  1. CLI flags explicitly set on fs
//  2. LEATHER_* environment variables
//  3. YAML config file at cfg.ConfigFile
//  4. Built-in defaults
//
// fs must have been populated by BindFlags before calling Load.
func Load(fs *flag.FlagSet) (model.Config, error) {
	agentDir, configFile, stateDir, _ := resolvePaths()

	// Start from env/defaults layer.
	cfg := model.Config{
		AgentDir:           envString("AGENT_DIR", agentDir),
		ConfigFile:         envString("CONFIG", configFile),
		LogLevel:           model.LogLevel(envString("LOG_LEVEL", DefaultLogLevel)),
		LogFormat:          envString("LOG_FORMAT", DefaultLogFormat),
		Model:              envString("MODEL", DefaultModel),
		Temperature:        envFloat("TEMPERATURE", DefaultTemperature),
		MaxTokens:          envInt("MAX_TOKENS", DefaultMaxTokens),
		CompletionReserve:  envInt("COMPLETION_RESERVE", DefaultCompletionReserve),
		SummarizeThreshold: envFloat("SUMMARIZE_THRESHOLD", DefaultSummarizeThreshold),
		LLMEndpoint:        envString("LLM_ENDPOINT", DefaultLLMEndpoint),
		LLMTimeout:         envDuration("LLM_TIMEOUT", DefaultLLMTimeout),
		SchedulerTick:      envDuration("SCHEDULER_TICK", DefaultSchedulerTick),
		MaxConcurrentJobs:  envInt("MAX_CONCURRENT_JOBS", DefaultMaxConcurrentJobs),
		RunDuration:        envDuration("RUN_DURATION", 0),
		MaxJobs:            envInt("MAX_JOBS", 0),
		StateDir:           envString("STATE_DIR", stateDir),
		API:                envBool("API", DefaultAPI),
		APIAddr:            envString("API_ADDR", DefaultAPIAddr),
		LogFile:            envString("LOG_FILE", ""),
		Pretty:             envBool("PRETTY", false),
		PrettyMode:         envString("PRETTY_MODE", DefaultPrettyMode),
		Stats:              envBool("STATS", false),
		TokensPerTurn:      envBool("TOKENS_PER_TURN", false),
		ShowVars:           envBool("SHOW_VARS", false),
		ShowContext:        envBool("SHOW_CONTEXT", false),
		PersistRuns:        envBool("PERSIST_RUNS", false),
		RunHistoryDir:      envString("RUN_HISTORY_DIR", ""),
		RunMaxBytes:        envInt64("RUN_MAX_BYTES", DefaultRunMaxBytes),
		ReplayFile:         envString("REPLAY", ""),
		ReplayLiveDir:      envString("REPLAY_LIVE", ""),
		ReplaySpeed:        envFloat("REPLAY_SPEED", DefaultReplaySpeed),
		ToolDir:            envString("TOOL_DIR", ""),
		DefaultToolsets:    envCSV("DEFAULT_TOOLSETS"),
		MaxToolRounds:      envInt("MAX_TOOL_ROUNDS", DefaultMaxToolRounds),
		WorkerDir:          envString("WORKER_DIR", ""),
		CacheDir:           envString("CACHE_DIR", ""),
		MCPServersFile:     envString("MCP_SERVERS_FILE", ""),
		Loop:               envInt("LOOP", 1),
		TanneryFile:        envString("TANNERY", ""),
	}

	// LLM API key: inline form may come from --llm-api-key or
	// LEATHER_LLM_API_KEY. The YAML loader (below) may override with the
	// richer { pass:..., env:... } form before resolution.
	llmKeyRef := secret.Ref{Value: envString("LLM_API_KEY", "")}

	// Pre-scan CLI flags for --config override before loading the YAML file.
	fs.Visit(func(f *flag.Flag) {
		if f.Name == "config" {
			cfg.ConfigFile = f.Value.String()
		}
	})

	// Layer 3: YAML config file (silently skipped if not found).
	if err := loadYAMLFile(cfg.ConfigFile, &cfg); err != nil && !os.IsNotExist(err) {
		return model.Config{}, fmt.Errorf("config/Load: reading %s: %w", cfg.ConfigFile, err)
	}
	applyEnvOverrides(&cfg)

	// Pull any llm_api_key block from the YAML; it may supply Pass / Env.
	if cfg.ConfigFile != "" {
		if data, err := os.ReadFile(cfg.ConfigFile); err == nil {
			yamlRef := parseLLMAPIKeyBlock(string(data))
			// YAML enriches the ref but does not replace an env-set inline value.
			if llmKeyRef.Value == "" {
				llmKeyRef.Value = yamlRef.Value
			}
			llmKeyRef.Pass = yamlRef.Pass
			llmKeyRef.Env = yamlRef.Env
		}
	}

	// Layer 1: CLI flags override everything.
	fs.Visit(func(f *flag.Flag) {
		applyFlag(f, &cfg)
		if f.Name == "llm-api-key" {
			// --llm-api-key always wins as inline value.
			llmKeyRef = secret.Ref{Value: f.Value.String()}
		}
	})

	// Resolve the LLM API key via inline > pass > env, in that order.
	// Empty result is acceptable (local endpoints often need no auth).
	if !llmKeyRef.IsZero() {
		key, err := secret.Resolve(context.Background(), llmKeyRef)
		if err != nil {
			return model.Config{}, fmt.Errorf("config/Load: resolving llm_api_key: %w", err)
		}
		cfg.LLMAPIKey = key
	}

	return cfg, nil
}

func applyEnvOverrides(cfg *model.Config) {
	cfg.AgentDir = envString("AGENT_DIR", cfg.AgentDir)
	cfg.LogLevel = model.LogLevel(envString("LOG_LEVEL", string(cfg.LogLevel)))
	cfg.LogFormat = envString("LOG_FORMAT", cfg.LogFormat)
	cfg.Model = envString("MODEL", cfg.Model)
	cfg.Temperature = envFloat("TEMPERATURE", cfg.Temperature)
	cfg.MaxTokens = envInt("MAX_TOKENS", cfg.MaxTokens)
	cfg.CompletionReserve = envInt("COMPLETION_RESERVE", cfg.CompletionReserve)
	cfg.SummarizeThreshold = envFloat("SUMMARIZE_THRESHOLD", cfg.SummarizeThreshold)
	cfg.LLMEndpoint = envString("LLM_ENDPOINT", cfg.LLMEndpoint)
	cfg.LLMTimeout = envDuration("LLM_TIMEOUT", cfg.LLMTimeout)
	cfg.SchedulerTick = envDuration("SCHEDULER_TICK", cfg.SchedulerTick)
	cfg.MaxConcurrentJobs = envInt("MAX_CONCURRENT_JOBS", cfg.MaxConcurrentJobs)
	cfg.RunDuration = envDuration("RUN_DURATION", cfg.RunDuration)
	cfg.MaxJobs = envInt("MAX_JOBS", cfg.MaxJobs)
	cfg.StateDir = envString("STATE_DIR", cfg.StateDir)
	cfg.API = envBool("API", cfg.API)
	cfg.APIAddr = envString("API_ADDR", cfg.APIAddr)
	cfg.LogFile = envString("LOG_FILE", cfg.LogFile)
	cfg.Pretty = envBool("PRETTY", cfg.Pretty)
	cfg.PrettyMode = envString("PRETTY_MODE", cfg.PrettyMode)
	cfg.Stats = envBool("STATS", cfg.Stats)
	cfg.TokensPerTurn = envBool("TOKENS_PER_TURN", cfg.TokensPerTurn)
	cfg.ShowVars = envBool("SHOW_VARS", cfg.ShowVars)
	cfg.ShowContext = envBool("SHOW_CONTEXT", cfg.ShowContext)
	cfg.PersistRuns = envBool("PERSIST_RUNS", cfg.PersistRuns)
	cfg.RunHistoryDir = envString("RUN_HISTORY_DIR", cfg.RunHistoryDir)
	cfg.RunMaxBytes = envInt64("RUN_MAX_BYTES", cfg.RunMaxBytes)
	cfg.ReplayFile = envString("REPLAY", cfg.ReplayFile)
	cfg.ReplayLiveDir = envString("REPLAY_LIVE", cfg.ReplayLiveDir)
	cfg.ReplaySpeed = envFloat("REPLAY_SPEED", cfg.ReplaySpeed)
	cfg.ToolDir = envString("TOOL_DIR", cfg.ToolDir)
	if v := envCSV("DEFAULT_TOOLSETS"); len(v) > 0 {
		cfg.DefaultToolsets = v
	}
	cfg.MaxToolRounds = envInt("MAX_TOOL_ROUNDS", cfg.MaxToolRounds)
	cfg.WorkerDir = envString("WORKER_DIR", cfg.WorkerDir)
	cfg.CacheDir = envString("CACHE_DIR", cfg.CacheDir)
	cfg.MCPServersFile = envString("MCP_SERVERS_FILE", cfg.MCPServersFile)
	cfg.Loop = envInt("LOOP", cfg.Loop)
	cfg.TanneryFile = envString("TANNERY", cfg.TanneryFile)
}

// BindFlags registers all leather flags on fs with their env-resolved defaults.
// Call this before fs.Parse so that defaults appear in --help output.
func BindFlags(fs *flag.FlagSet) {
	agentDir, configFile, stateDir, _ := resolvePaths()

	fs.String("config", envString("CONFIG", configFile), "path to config file (LEATHER_CONFIG)")
	fs.String("agent-dir", envString("AGENT_DIR", agentDir), "directory for *.agent.md files (LEATHER_AGENT_DIR)")
	fs.String("model", envString("MODEL", DefaultModel), "global default model name (LEATHER_MODEL)")
	fs.Float64("temperature", envFloat("TEMPERATURE", DefaultTemperature), "global default sampling temperature (LEATHER_TEMPERATURE)")
	fs.String("log-level", envString("LOG_LEVEL", DefaultLogLevel), "log verbosity: debug, info, warn, error (LEATHER_LOG_LEVEL)")
	fs.String("log-format", envString("LOG_FORMAT", DefaultLogFormat), "log format: text, json (LEATHER_LOG_FORMAT)")
	fs.Int("max-tokens", envInt("MAX_TOKENS", DefaultMaxTokens), "global token budget ceiling (LEATHER_MAX_TOKENS)")
	fs.Int("completion-reserve", envInt("COMPLETION_RESERVE", DefaultCompletionReserve), "tokens reserved for model completion (LEATHER_COMPLETION_RESERVE)")
	fs.Float64("summarize-threshold", envFloat("SUMMARIZE_THRESHOLD", DefaultSummarizeThreshold), "summarization trigger fraction (LEATHER_SUMMARIZE_THRESHOLD)")
	fs.String("llm-endpoint", envString("LLM_ENDPOINT", DefaultLLMEndpoint), "LLM base URL (LEATHER_LLM_ENDPOINT)")
	fs.Duration("llm-timeout", envDuration("LLM_TIMEOUT", DefaultLLMTimeout), "LLM request timeout (LEATHER_LLM_TIMEOUT)")
	fs.Duration("scheduler-tick", envDuration("SCHEDULER_TICK", DefaultSchedulerTick), "scheduler wake interval (LEATHER_SCHEDULER_TICK)")
	fs.Int("max-concurrent-jobs", envInt("MAX_CONCURRENT_JOBS", DefaultMaxConcurrentJobs), "max simultaneous jobs (LEATHER_MAX_CONCURRENT_JOBS)")
	fs.Duration("run-duration", envDuration("RUN_DURATION", 0), "exit cleanly after this duration, 0=unlimited (LEATHER_RUN_DURATION)")
	fs.Int("max-jobs", envInt("MAX_JOBS", 0), "exit cleanly after this many completed jobs, 0=unlimited (LEATHER_MAX_JOBS)")
	fs.String("state-dir", envString("STATE_DIR", stateDir), "job state directory (LEATHER_STATE_DIR)")
	fs.Bool("api", envBool("API", DefaultAPI), "enable HTTP status API (LEATHER_API)")
	fs.String("api-addr", envString("API_ADDR", DefaultAPIAddr), "HTTP API bind address (LEATHER_API_ADDR)")
	fs.String("log-file", envString("LOG_FILE", ""), "write full structured logs to file; tees with stderr unless --pretty (LEATHER_LOG_FILE)")
	fs.Bool("pretty", envBool("PRETTY", false), "render turns-only output to console; suppress structured log from console (LEATHER_PRETTY)")
	fs.String("pretty-mode", envString("PRETTY_MODE", DefaultPrettyMode), "pretty console rendering: messages or all (LEATHER_PRETTY_MODE)")
	fs.Bool("stats", envBool("STATS", false), "show per-turn token counts and a summary at shutdown (LEATHER_STATS)")
	fs.Bool("tokens-per-turn", envBool("TOKENS_PER_TURN", false), "print token usage after each individual turn response in pretty mode (LEATHER_TOKENS_PER_TURN)")
	fs.Bool("show-vars", envBool("SHOW_VARS", false), "print extracted turn variables as timeline events in pretty mode (LEATHER_SHOW_VARS)")
	fs.Bool("show-context", envBool("SHOW_CONTEXT", false), "print the exact message window and tool exposure before each LLM call (LEATHER_SHOW_CONTEXT)")
	fs.Bool("persist-runs", envBool("PERSIST_RUNS", false), "persist run records to JSONL files (LEATHER_PERSIST_RUNS)")
	fs.String("run-history-dir", envString("RUN_HISTORY_DIR", ""), "directory for per-agent JSONL run logs; default <state-dir>/runs (LEATHER_RUN_HISTORY_DIR)")
	fs.Int64("run-max-bytes", envInt64("RUN_MAX_BYTES", DefaultRunMaxBytes), "rotate run log at this size in bytes (LEATHER_RUN_MAX_BYTES)")
	fs.String("replay", envString("REPLAY", ""), "start in replay mode: path to a snapshot JSON file (LEATHER_REPLAY)")
	fs.String("replay-live", envString("REPLAY_LIVE", ""), "start in live replay mode: path to a JSONL runs directory (LEATHER_REPLAY_LIVE)")
	fs.Float64("replay-speed", envFloat("REPLAY_SPEED", DefaultReplaySpeed), "live replay speed multiplier (LEATHER_REPLAY_SPEED)")
	fs.String("tool-dir", envString("TOOL_DIR", ""), "directory containing *.skill.yaml tool definitions (LEATHER_TOOL_DIR)")
	fs.String("default-toolsets", strings.Join(envCSV("DEFAULT_TOOLSETS"), ","), "comma-separated global default toolsets applied to every agent (LEATHER_DEFAULT_TOOLSETS)")
	fs.Int("max-tool-rounds", envInt("MAX_TOOL_ROUNDS", DefaultMaxToolRounds), "global default max tool call cycles per agent run (LEATHER_MAX_TOOL_ROUNDS)")
	fs.String("worker-dir", envString("WORKER_DIR", ""), "directory containing *.worker.yaml worker definitions (LEATHER_WORKER_DIR)")
	fs.String("cache-dir", envString("CACHE_DIR", ""), "directory for response cache JSON files (LEATHER_CACHE_DIR)")
	fs.String("mcp-servers-file", envString("MCP_SERVERS_FILE", ""), "path to mcp-servers.yaml (LEATHER_MCP_SERVERS_FILE)")
	fs.Int("loop", envInt("LOOP", 1), "repeat the run command N times (LEATHER_LOOP)")
	fs.String("tannery", envString("TANNERY", ""), "path to tannery.yaml; enables tannery mode (LEATHER_TANNERY)")
	fs.String("llm-api-key", envString("LLM_API_KEY", ""), "bearer token for the LLM endpoint; prefer llm_api_key: { pass:..., env:... } in config.yaml (LEATHER_LLM_API_KEY)")
}

// loadYAMLFile reads cfg values from path, overwriting only keys that are present.
func loadYAMLFile(path string, cfg *model.Config) error {
	b, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	if err := applyYAML(bytes.NewReader(b), cfg); err != nil {
		return err
	}
	cfg.NotifyBackends = parseNotifyBackends(string(b))
	if limits := parseToolRateLimits(string(b)); len(limits) > 0 {
		cfg.ToolRateLimits = limits
	}
	return nil
}

// applyYAML reads YAML from r and overlays matching keys onto cfg.
func applyYAML(r io.Reader, cfg *model.Config) error {
	vals, lists, err := yamlx.ParseFlat(r)
	if err != nil {
		return err
	}
	strVal := func(key string) (string, bool) {
		v, ok := vals[key]
		return v, ok && v != ""
	}
	if v, ok := strVal("agent_dir"); ok {
		cfg.AgentDir = v
	}
	if v, ok := strVal("model"); ok {
		cfg.Model = v
	}
	if v, ok := strVal("log_level"); ok {
		cfg.LogLevel = model.LogLevel(v)
	}
	if v, ok := strVal("log_format"); ok {
		cfg.LogFormat = v
	}
	if v, ok := strVal("llm_endpoint"); ok {
		cfg.LLMEndpoint = v
	}
	if v, ok := strVal("api_addr"); ok {
		cfg.APIAddr = v
	}
	if v, ok := strVal("state_dir"); ok {
		cfg.StateDir = v
	}
	if v, ok := strVal("max_tokens"); ok {
		if n, err := strconv.Atoi(v); err == nil {
			cfg.MaxTokens = n
		}
	}
	if v, ok := strVal("completion_reserve"); ok {
		if n, err := strconv.Atoi(v); err == nil {
			cfg.CompletionReserve = n
		}
	}
	if v, ok := strVal("max_concurrent_jobs"); ok {
		if n, err := strconv.Atoi(v); err == nil {
			cfg.MaxConcurrentJobs = n
		}
	}
	if v, ok := strVal("summarize_threshold"); ok {
		if f, err := strconv.ParseFloat(v, 64); err == nil {
			cfg.SummarizeThreshold = f
		}
	}
	if v, ok := strVal("temperature"); ok {
		if f, err := strconv.ParseFloat(v, 64); err == nil {
			cfg.Temperature = f
		}
	}
	if v, ok := strVal("llm_timeout"); ok {
		if d, err := time.ParseDuration(v); err == nil {
			cfg.LLMTimeout = d
		}
	}
	if v, ok := strVal("scheduler_tick"); ok {
		if d, err := time.ParseDuration(v); err == nil {
			cfg.SchedulerTick = d
		}
	}
	if v, ok := strVal("run_duration"); ok {
		if d, err := time.ParseDuration(v); err == nil {
			cfg.RunDuration = d
		}
	}
	if v, ok := strVal("max_jobs"); ok {
		if n, err := strconv.Atoi(v); err == nil {
			cfg.MaxJobs = n
		}
	}
	if v, ok := strVal("api"); ok {
		if b, err := strconv.ParseBool(v); err == nil {
			cfg.API = b
		}
	}
	if v, ok := strVal("log_file"); ok {
		cfg.LogFile = v
	}
	if v, ok := strVal("pretty"); ok {
		if b, err := strconv.ParseBool(v); err == nil {
			cfg.Pretty = b
		}
	}
	if v, ok := strVal("pretty_mode"); ok {
		cfg.PrettyMode = v
	}
	if v, ok := strVal("stats"); ok {
		if b, err := strconv.ParseBool(v); err == nil {
			cfg.Stats = b
		}
	}
	if v, ok := strVal("tokens_per_turn"); ok {
		if b, err := strconv.ParseBool(v); err == nil {
			cfg.TokensPerTurn = b
		}
	}
	if v, ok := strVal("show_vars"); ok {
		if b, err := strconv.ParseBool(v); err == nil {
			cfg.ShowVars = b
		}
	}
	if v, ok := strVal("show_context"); ok {
		if b, err := strconv.ParseBool(v); err == nil {
			cfg.ShowContext = b
		}
	}
	if v, ok := strVal("persist_runs"); ok {
		if b, err := strconv.ParseBool(v); err == nil {
			cfg.PersistRuns = b
		}
	}
	if v, ok := strVal("run_history_dir"); ok {
		cfg.RunHistoryDir = v
	}
	if v, ok := strVal("run_max_bytes"); ok {
		if n, err := strconv.ParseInt(v, 10, 64); err == nil {
			cfg.RunMaxBytes = n
		}
	}
	if v, ok := strVal("tool_dir"); ok {
		cfg.ToolDir = v
	}
	if items := lists["default_toolsets"]; len(items) > 0 {
		cfg.DefaultToolsets = append([]string(nil), items...)
	}
	if v, ok := strVal("max_tool_rounds"); ok {
		if n, err := strconv.Atoi(v); err == nil {
			cfg.MaxToolRounds = n
		}
	}
	if v, ok := strVal("worker_dir"); ok {
		cfg.WorkerDir = v
	}
	if v, ok := strVal("cache_dir"); ok {
		cfg.CacheDir = v
	}
	if v, ok := strVal("mcp_servers_file"); ok {
		cfg.MCPServersFile = v
	}
	if v, ok := strVal("loop"); ok {
		if n, err := strconv.Atoi(v); err == nil {
			cfg.Loop = n
		}
	}
	return nil
}

// applyFlag copies an explicitly-set flag value into cfg.
// It is called via fs.Visit, which only visits flags that were explicitly set.
func applyFlag(f *flag.Flag, cfg *model.Config) {
	v := f.Value.String()
	switch f.Name {
	case "config":
		cfg.ConfigFile = v
	case "agent-dir":
		cfg.AgentDir = v
	case "model":
		cfg.Model = v
	case "temperature":
		if f64, err := strconv.ParseFloat(v, 64); err == nil {
			cfg.Temperature = f64
		}
	case "log-level":
		cfg.LogLevel = model.LogLevel(v)
	case "log-format":
		cfg.LogFormat = v
	case "max-tokens":
		if n, err := strconv.Atoi(v); err == nil {
			cfg.MaxTokens = n
		}
	case "completion-reserve":
		if n, err := strconv.Atoi(v); err == nil {
			cfg.CompletionReserve = n
		}
	case "summarize-threshold":
		if f64, err := strconv.ParseFloat(v, 64); err == nil {
			cfg.SummarizeThreshold = f64
		}
	case "llm-endpoint":
		cfg.LLMEndpoint = v
	case "llm-timeout":
		if d, err := time.ParseDuration(v); err == nil {
			cfg.LLMTimeout = d
		}
	case "scheduler-tick":
		if d, err := time.ParseDuration(v); err == nil {
			cfg.SchedulerTick = d
		}
	case "run-duration":
		if d, err := time.ParseDuration(v); err == nil {
			cfg.RunDuration = d
		}
	case "max-concurrent-jobs":
		if n, err := strconv.Atoi(v); err == nil {
			cfg.MaxConcurrentJobs = n
		}
	case "max-jobs":
		if n, err := strconv.Atoi(v); err == nil {
			cfg.MaxJobs = n
		}
	case "state-dir":
		cfg.StateDir = v
	case "api":
		if b, err := strconv.ParseBool(v); err == nil {
			cfg.API = b
		}
	case "api-addr":
		cfg.APIAddr = v
	case "log-file":
		cfg.LogFile = v
	case "pretty":
		if b, err := strconv.ParseBool(v); err == nil {
			cfg.Pretty = b
		}
	case "pretty-mode":
		cfg.PrettyMode = v
	case "stats":
		if b, err := strconv.ParseBool(v); err == nil {
			cfg.Stats = b
		}
	case "tokens-per-turn":
		if b, err := strconv.ParseBool(v); err == nil {
			cfg.TokensPerTurn = b
		}
	case "show-vars":
		if b, err := strconv.ParseBool(v); err == nil {
			cfg.ShowVars = b
		}
	case "show-context":
		if b, err := strconv.ParseBool(v); err == nil {
			cfg.ShowContext = b
		}
	case "persist-runs":
		if b, err := strconv.ParseBool(v); err == nil {
			cfg.PersistRuns = b
		}
	case "run-history-dir":
		cfg.RunHistoryDir = v
	case "run-max-bytes":
		if n, err := strconv.ParseInt(v, 10, 64); err == nil {
			cfg.RunMaxBytes = n
		}
	case "replay":
		cfg.ReplayFile = v
	case "replay-live":
		cfg.ReplayLiveDir = v
	case "replay-speed":
		if f64, err := strconv.ParseFloat(v, 64); err == nil {
			cfg.ReplaySpeed = f64
		}
	case "tool-dir":
		cfg.ToolDir = v
	case "default-toolsets":
		cfg.DefaultToolsets = splitCSV(v)
	case "max-tool-rounds":
		if n, err := strconv.Atoi(v); err == nil {
			cfg.MaxToolRounds = n
		}
	case "worker-dir":
		cfg.WorkerDir = v
	case "cache-dir":
		cfg.CacheDir = v
	case "mcp-servers-file":
		cfg.MCPServersFile = v
	case "loop":
		if n, err := strconv.Atoi(v); err == nil {
			cfg.Loop = n
		}
	case "tannery":
		cfg.TanneryFile = v
	}
}

// resolvePaths returns the default ~/.leather/* paths resolved at call time.
func resolvePaths() (agentDir, configFile, stateDir, logsDir string) {
	home, err := os.UserHomeDir()
	if err != nil {
		home = "."
	}
	base := filepath.Join(home, ".leather")
	return filepath.Join(base, "agents"),
		filepath.Join(base, "config.yaml"),
		filepath.Join(base, ".state"),
		filepath.Join(base, "logs")
}

// expandHome replaces a leading ~ with the user's home directory.
func expandHome(path string) string {
	if !strings.HasPrefix(path, "~/") {
		return path
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return path
	}
	return filepath.Join(home, path[2:])
}

// parseNotifyBackends extracts the notify.backends list from a raw config YAML string.
//
// The expected structure is:
//
//	notify:
//	  backends:
//	    - name: telegram-main
//	      type: telegram
//	      chat_id: "..."
//	      token:
//	        pass: leather/telegram/bot-token
//	        env:  LEATHER_TELEGRAM_BOT_TOKEN
//	    - name: signal-home
//	      type: signal
//	      from: "+1..."
//	      to:   "+1..."
//	      api_url: "http://127.0.0.1:8080"
//	      token:
//	        pass: leather/signal/api-key
//	        env:  LEATHER_SIGNAL_API_KEY
func parseNotifyBackends(src string) []model.NotifyBackendConfig {
	// Locate the notify: block.
	lines := strings.Split(src, "\n")
	start := -1
	for i, line := range lines {
		if strings.TrimSpace(line) == "notify:" {
			start = i + 1
			break
		}
	}
	if start < 0 {
		return nil
	}

	// Collect lines inside the notify: block (stops at next top-level key).
	var notifyLines []string
	for i := start; i < len(lines); i++ {
		line := lines[i]
		if line == "" || strings.TrimSpace(line) == "" {
			notifyLines = append(notifyLines, "")
			continue
		}
		// Top-level key (no leading whitespace, not blank): block ends.
		if len(line) > 0 && line[0] != ' ' && line[0] != '\t' {
			break
		}
		notifyLines = append(notifyLines, line)
	}

	// Locate the backends: sub-block.
	backendStart := -1
	for i, line := range notifyLines {
		if strings.TrimSpace(line) == "backends:" {
			backendStart = i + 1
			break
		}
	}
	if backendStart < 0 {
		return nil
	}

	// Split into per-item blocks separated by "- name:" lines.
	type rawItem struct{ lines []string }
	var rawItems []rawItem
	var cur []string
	for _, line := range notifyLines[backendStart:] {
		stripped := strings.TrimSpace(line)
		if strings.HasPrefix(stripped, "- ") {
			if cur != nil {
				rawItems = append(rawItems, rawItem{cur})
			}
			cur = []string{strings.TrimPrefix(stripped, "- ")}
		} else if cur != nil {
			cur = append(cur, stripped)
		}
	}
	if cur != nil {
		rawItems = append(rawItems, rawItem{cur})
	}

	var backends []model.NotifyBackendConfig
	for _, item := range rawItems {
		b := parseNotifyBackendItem(item.lines)
		if b.Name != "" && b.Type != "" {
			backends = append(backends, b)
		}
	}
	return backends
}

// parseNotifyBackendItem parses one backend item from its trimmed line slice.
func parseNotifyBackendItem(lines []string) model.NotifyBackendConfig {
	var b model.NotifyBackendConfig
	inToken := false
	for _, line := range lines {
		if line == "" {
			continue
		}
		if strings.TrimSpace(line) == "token:" {
			inToken = true
			continue
		}
		k, v, ok := yamlx.SplitKV(line)
		if !ok {
			continue
		}
		if inToken {
			switch k {
			case "pass":
				b.Token.Pass = v
			case "env":
				b.Token.Env = v
			default:
				// Unknown token sub-key: fall through to top-level fields.
				inToken = false
			}
			if inToken {
				continue
			}
		}
		switch k {
		case "name":
			b.Name = v
		case "type":
			b.Type = v
		case "chat_id":
			b.ChatID = v
		case "from":
			b.From = v
		case "to":
			b.To = v
		case "group_id":
			b.GroupID = v
		case "api_url":
			b.APIURL = v
		}
	}
	return b
}

// parseToolRateLimits extracts the tools.rate_limits map from raw YAML source.
// It looks for a block of the form:
//
//	tools:
//	  rate_limits:
//	    api.github.com: 5000/h
//	    example.com: 10/s
//
// Returns nil when the block is absent.
func parseToolRateLimits(src string) map[string]string {
	lines := strings.Split(src, "\n")

	// Find "tools:" top-level key.
	toolsStart := -1
	for i, line := range lines {
		if strings.TrimSpace(line) == "tools:" {
			toolsStart = i + 1
			break
		}
	}
	if toolsStart < 0 {
		return nil
	}

	// Collect indented lines inside the tools: block.
	var toolsLines []string
	for i := toolsStart; i < len(lines); i++ {
		line := lines[i]
		if line == "" || strings.TrimSpace(line) == "" {
			toolsLines = append(toolsLines, "")
			continue
		}
		if len(line) > 0 && line[0] != ' ' && line[0] != '\t' {
			break
		}
		toolsLines = append(toolsLines, strings.TrimSpace(line))
	}

	// Find "rate_limits:" sub-block.
	rlStart := -1
	for i, line := range toolsLines {
		if line == "rate_limits:" {
			rlStart = i + 1
			break
		}
	}
	if rlStart < 0 {
		return nil
	}

	limits := make(map[string]string)
	for _, line := range toolsLines[rlStart:] {
		if line == "" {
			continue
		}
		k, v, ok := yamlx.SplitKV(line)
		if !ok || v == "" {
			continue
		}
		limits[k] = v
	}
	if len(limits) == 0 {
		return nil
	}
	return limits
}

// parseLLMAPIKeyBlock extracts the llm_api_key field from raw YAML source.
//
// Two forms are accepted:
//
//	llm_api_key: "sk-abc"
//
//	llm_api_key:
//	  pass: openai/api-key
//	  env: OPENAI_API_KEY
//
// Returns a zero secret.Ref when no such field exists.
func parseLLMAPIKeyBlock(src string) secret.Ref {
	lines := strings.Split(src, "\n")
	for i, line := range lines {
		// Only consider top-level (zero-indent) keys.
		if len(line) > 0 && (line[0] == ' ' || line[0] == '\t') {
			continue
		}
		k, v, ok := yamlx.SplitKV(line)
		if !ok || k != "llm_api_key" {
			continue
		}
		if v != "" {
			return secret.Ref{Value: v}
		}
		// Sub-block form: scan indented lines for pass: / env:.
		var ref secret.Ref
		for j := i + 1; j < len(lines); j++ {
			sub := lines[j]
			if sub == "" || strings.TrimSpace(sub) == "" {
				continue
			}
			if sub[0] != ' ' && sub[0] != '\t' {
				break
			}
			sk, sv, sok := yamlx.SplitKV(sub)
			if !sok {
				continue
			}
			switch sk {
			case "pass":
				ref.Pass = sv
			case "env":
				ref.Env = sv
			}
		}
		return ref
	}
	return secret.Ref{}
}

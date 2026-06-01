// Package config loads and merges leather configuration from multiple sources.
package config

import "time"

// Built-in defaults for all configuration parameters.
// Path defaults (AgentDir, ConfigFile, StateDir, LogsDir) are resolved at load
// time via os.UserHomeDir and are not listed here as constants.
const (
	DefaultModel                      = ""
	DefaultTemperature                = 0.7
	DefaultMaxTokens                  = 8192
	DefaultCompletionReserve          = 1024
	DefaultSummarizeThreshold         = 0.85
	DefaultLLMEndpoint                = "http://localhost:11434"
	DefaultLLMTimeout                 = 60 * time.Second
	DefaultSchedulerTick              = time.Minute
	DefaultMaxConcurrentJobs          = 4
	DefaultLogLevel                   = "info"
	DefaultLogFormat                  = "text"
	DefaultPrettyMode                 = "all"
	DefaultAPI                        = false
	DefaultAPIAddr                    = "127.0.0.1:7749"
	DefaultRunMaxBytes        int64   = 10 * 1024 * 1024 // 10 MB
	DefaultReplaySpeed        float64 = 1.0
	DefaultMaxToolRounds              = 5
)

package model

import "strings"

// reasoningModelReserves maps a substring of a known reasoning-model name to
// a suggested completion_reserve large enough for its <think> trace to fit
// alongside an actual answer. Matched case-insensitively against Agent.Model,
// since model names vary by serving backend (Ollama tags, full HF paths, etc.).
// This is a small, leather-internal list — not an external manifest — and is
// meant to unblock common cases, not to be exhaustive.
var reasoningModelReserves = map[string]int{ //nolint:gochecknoglobals // static lookup table, not mutated after init
	"qwen3":       8192,
	"qwq":         8192,
	"deepseek-r1": 8192,
}

// LookupReserve returns a suggested completion_reserve for modelName and
// whether modelName matched a known reasoning model. Leather applies this
// only when no explicit per-agent CompletionReserve override is set — an
// explicit override always wins over this auto-sized default.
func LookupReserve(modelName string) (int, bool) {
	lower := strings.ToLower(modelName)
	for substr, reserve := range reasoningModelReserves {
		if strings.Contains(lower, substr) {
			return reserve, true
		}
	}
	return 0, false
}

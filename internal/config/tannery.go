package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/tgpski/leather/internal/model"
	"github.com/tgpski/leather/internal/yamlx"
)

// TanneryConfig holds tannery-specific settings loaded from tannery.yaml.
// This is distinct from the main Config in config.go.
type TanneryConfig struct {
	// HideDir is the root directory for hide storage.
	HideDir string `json:"hide_dir"`
	// CuringDir is the directory containing *.curing.yaml definition files.
	CuringDir string `json:"curing_dir"`
	// ArtifactDir is the root directory for artifact storage.
	ArtifactDir string `json:"artifact_dir"`
	// Routes is the ordered list of intake event routing rules.
	Routes []model.TanneryRoute `json:"routes"`
	// Queues maps queue name to concurrency/backpressure settings.
	Queues map[string]model.QueueConcurrencyConfig `json:"queues"`
	// Webhooks is the list of registered webhook endpoints.
	Webhooks []model.WebhookConfig `json:"webhooks"`
}

// LoadTannery parses the YAML file at path and returns TanneryConfig.
// Returns zero-value TanneryConfig (not error) when path does not exist.
// All relative paths are resolved relative to the directory of path.
// Secrets matching "{{env:VAR}}" are expanded from os.Getenv(VAR).
func LoadTannery(path string) (TanneryConfig, error) {
	if path == "" {
		return TanneryConfig{}, nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return TanneryConfig{}, nil
		}
		return TanneryConfig{}, fmt.Errorf("config/LoadTannery: %w", err)
	}

	cfg, err := parseTanneryYAML(string(data))
	if err != nil {
		return TanneryConfig{}, fmt.Errorf("config/LoadTannery: parse %s: %w", path, err)
	}

	// Resolve relative paths to the directory of the config file.
	base := filepath.Dir(path)
	cfg.HideDir = resolvePath(base, cfg.HideDir)
	cfg.CuringDir = resolvePath(base, cfg.CuringDir)
	cfg.ArtifactDir = resolvePath(base, cfg.ArtifactDir)

	// Expand {{env:VAR}} secrets in webhook configs.
	for i := range cfg.Webhooks {
		resolved, err := expandEnvSecret(cfg.Webhooks[i].Secret)
		if err != nil {
			return TanneryConfig{}, fmt.Errorf("config/LoadTannery: webhook %q secret: %w", cfg.Webhooks[i].Name, err)
		}
		cfg.Webhooks[i].Secret = resolved
	}

	return cfg, nil
}

// resolvePath returns path resolved against base; absolute paths are returned unchanged.
func resolvePath(base, path string) string {
	if path == "" || filepath.IsAbs(path) {
		return path
	}
	return filepath.Join(base, path)
}

// expandEnvSecret replaces {{env:VAR}} with os.Getenv("VAR") in s.
// Only the first {{env:...}} token per field is replaced.
// Returns an error if {{env:VAR}} is specified but the environment variable is unset.
func expandEnvSecret(s string) (string, error) {
	const prefix = "{{env:"
	const suffix = "}}"
	for {
		start := strings.Index(s, prefix)
		if start < 0 {
			break
		}
		end := strings.Index(s[start:], suffix)
		if end < 0 {
			break
		}
		end += start
		varName := s[start+len(prefix) : end]
		val := os.Getenv(varName)
		if val == "" {
			return "", fmt.Errorf("environment variable %q is unset or empty", varName)
		}
		s = s[:start] + val + s[end+len(suffix):]
	}
	return s, nil
}

// parseTanneryYAML parses tannery.yaml using the stdlib-only line-by-line approach.
// It handles three nested block types: routes (list-of-maps), queues (map-of-maps),
// and webhooks (list-of-maps).
func parseTanneryYAML(src string) (TanneryConfig, error) {
	var cfg TanneryConfig

	// Block tracking state.
	const (
		blockNone     = ""
		blockRoutes   = "routes"
		blockQueues   = "queues"
		blockWebhooks = "webhooks"
	)
	block := blockNone

	// Current in-progress objects; appended when a new peer begins or the block ends.
	var curRoute *model.TanneryRoute
	var curQueue string // current queue name key
	var curQCC *model.QueueConcurrencyConfig
	var curWebhook *model.WebhookConfig
	inMatchBlock := false // inside a route's match: sub-block

	flushRoute := func() {
		if curRoute != nil {
			if cfg.Routes == nil {
				cfg.Routes = []model.TanneryRoute{}
			}
			cfg.Routes = append(cfg.Routes, *curRoute)
			curRoute = nil
		}
		inMatchBlock = false
	}
	flushQueue := func() {
		if curQCC != nil && curQueue != "" {
			if cfg.Queues == nil {
				cfg.Queues = map[string]model.QueueConcurrencyConfig{}
			}
			cfg.Queues[curQueue] = *curQCC
			curQCC = nil
			curQueue = ""
		}
	}
	flushWebhook := func() {
		if curWebhook != nil {
			if cfg.Webhooks == nil {
				cfg.Webhooks = []model.WebhookConfig{}
			}
			cfg.Webhooks = append(cfg.Webhooks, *curWebhook)
			curWebhook = nil
		}
	}

	lines := strings.Split(src, "\n")
	for _, raw := range lines {
		line := strings.TrimRight(raw, "\r")
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") || trimmed == "---" {
			continue
		}

		// Determine indentation level.
		indent := len(line) - len(strings.TrimLeft(line, " \t"))
		isTopLevel := indent == 0

		// Top-level keys reset block state.
		if isTopLevel {
			k, _, _ := yamlx.SplitKV(trimmed)
			switch k {
			case "hide_dir", "curing_dir", "artifact_dir":
				flushRoute()
				flushQueue()
				flushWebhook()
				block = blockNone
				_, v, ok := yamlx.SplitKV(trimmed)
				if ok {
					switch k {
					case "hide_dir":
						cfg.HideDir = v
					case "curing_dir":
						cfg.CuringDir = v
					case "artifact_dir":
						cfg.ArtifactDir = v
					}
				}
				continue
			case "routes":
				flushRoute()
				flushQueue()
				flushWebhook()
				block = blockRoutes
				continue
			case "queues":
				flushRoute()
				flushQueue()
				flushWebhook()
				block = blockQueues
				continue
			case "webhooks":
				flushRoute()
				flushQueue()
				flushWebhook()
				block = blockWebhooks
				continue
			}
			continue
		}

		// Indented lines belong to the current block.
		switch block {
		case blockRoutes:
			if err := parseTanneryRouteLine(trimmed, indent, &curRoute, &inMatchBlock, &flushRoute); err != nil {
				return TanneryConfig{}, err
			}
		case blockQueues:
			parseTanneryQueueLine(trimmed, indent, &curQueue, &curQCC, flushQueue)
		case blockWebhooks:
			if err := parseTanneryWebhookLine(trimmed, indent, &curWebhook, &flushWebhook); err != nil {
				return TanneryConfig{}, err
			}
		}
	}

	// Flush any trailing in-progress objects.
	flushRoute()
	flushQueue()
	flushWebhook()

	return cfg, nil
}

// parseTanneryRouteLine parses one indented line within the routes: block.
func parseTanneryRouteLine(trimmed string, indent int, cur **model.TanneryRoute, inMatch *bool, flush *func()) error {
	// A "- name:" line at indent==2 begins a new route.
	if strings.HasPrefix(trimmed, "- ") {
		(*flush)()
		inner := strings.TrimSpace(trimmed[2:])
		*cur = &model.TanneryRoute{}
		*inMatch = false
		if inner == "" {
			return nil
		}
		// Could be "- name: value" on the same line.
		k, v, ok := yamlx.SplitKV(inner)
		if ok && k == "name" {
			(*cur).Name = v
		}
		return nil
	}
	if *cur == nil {
		return nil
	}
	k, v, ok := yamlx.SplitKV(trimmed)
	if !ok {
		return nil
	}
	// match: sub-block
	if k == "match" {
		*inMatch = true
		return nil
	}
	if *inMatch {
		// Sub-keys inside match: are source and event_type.
		switch k {
		case "source":
			(*cur).Match.Source = v
		case "event_type":
			(*cur).Match.EventType = v
		default:
			// Any non-match key ends the match sub-block.
			*inMatch = false
			applyRouteKey(*cur, k, v)
		}
		return nil
	}
	applyRouteKey(*cur, k, v)
	return nil
}

func applyRouteKey(r *model.TanneryRoute, k, v string) {
	switch k {
	case "name":
		r.Name = v
	case "hide_kind":
		r.HideKind = v
	case "curing":
		r.Curing = v
	case "queue":
		r.Queue = v
	case "queue_pattern":
		r.QueuePattern = v
	}
}

// parseTanneryQueueLine parses one indented line within the queues: block.
// queues is a map-of-maps: the queue name is a top-level key under queues:.
func parseTanneryQueueLine(trimmed string, indent int, curName *string, cur **model.QueueConcurrencyConfig, flush func()) {
	k, v, ok := yamlx.SplitKV(trimmed)
	if !ok {
		return
	}
	// A key at indent==2 is a queue name; deeper keys are its fields.
	if indent <= 2 {
		flush()
		*curName = k
		*cur = &model.QueueConcurrencyConfig{}
		return
	}
	if *cur == nil {
		return
	}
	switch k {
	case "concurrency":
		n, err := strconv.Atoi(v)
		if err == nil {
			(*cur).Concurrency = n
		}
	case "max_attempts":
		n, err := strconv.Atoi(v)
		if err == nil {
			(*cur).MaxAttempts = n
		}
	case "max_depth":
		n, err := strconv.Atoi(v)
		if err == nil {
			(*cur).MaxDepth = n
		}
	}
}

// parseTanneryWebhookLine parses one indented line within the webhooks: block.
func parseTanneryWebhookLine(trimmed string, indent int, cur **model.WebhookConfig, flush *func()) error {
	if strings.HasPrefix(trimmed, "- ") {
		(*flush)()
		inner := strings.TrimSpace(trimmed[2:])
		*cur = &model.WebhookConfig{}
		if inner == "" {
			return nil
		}
		k, v, ok := yamlx.SplitKV(inner)
		if ok && k == "name" {
			(*cur).Name = v
		}
		return nil
	}
	if *cur == nil {
		return nil
	}
	k, v, ok := yamlx.SplitKV(trimmed)
	if !ok {
		return nil
	}
	switch k {
	case "name":
		(*cur).Name = v
	case "path":
		(*cur).Path = v
	case "source":
		(*cur).Source = v
	case "secret":
		(*cur).Secret = v
	case "max_body_bytes":
		n, err := strconv.ParseInt(v, 10, 64)
		if err != nil {
			return fmt.Errorf("invalid max_body_bytes %q: %w", v, err)
		}
		(*cur).MaxBodyBytes = n
	}
	return nil
}

// ValidateTannery verifies that every route references a loaded curing definition
// and a declared queue. Routes with queue_pattern skip the static queue check.
// Returns nil on success; returns a combined error on failure.
// Fail-fast at serve startup — callers should treat a non-nil return as fatal.
func ValidateTannery(cfg TanneryConfig, defs []model.CuringDefinition) error {
	defByName := make(map[string]bool, len(defs))
	curingPrefixes := make([]string, 0, len(defs))
	for _, d := range defs {
		defByName[d.Name] = true
		if d.QueuePrefix != "" {
			curingPrefixes = append(curingPrefixes, d.QueuePrefix)
		}
	}

	var problems []string
	for _, r := range cfg.Routes {
		if !defByName[r.Curing] {
			problems = append(problems, fmt.Sprintf(
				"route %q: curing %q not found in curing_dir", r.Name, r.Curing))
		}
		// Routes with queue_pattern create queues dynamically; no static queue required.
		if r.QueuePattern == "" {
			if _, ok := cfg.Queues[r.Queue]; !ok {
				problems = append(problems, fmt.Sprintf(
					"route %q: queue %q not declared in queues:", r.Name, r.Queue))
			}
		} else {
			// T2.19: queue_pattern routes feed a single-use queue created at
			// match time. The literal prefix (text before the first {{) must
			// match one of the declared curing queue_prefix values, otherwise
			// the worker pool will never poll the resulting queue name.
			literal := r.QueuePattern
			if idx := strings.Index(literal, "{{"); idx >= 0 {
				literal = literal[:idx]
			}
			if literal == "" {
				problems = append(problems, fmt.Sprintf(
					"route %q: queue_pattern %q has no literal prefix before the first {{ token",
					r.Name, r.QueuePattern))
			} else {
				matched := false
				for _, p := range curingPrefixes {
					if strings.HasPrefix(literal, p) || strings.HasPrefix(p, literal) {
						matched = true
						break
					}
				}
				if !matched && len(curingPrefixes) > 0 {
					problems = append(problems, fmt.Sprintf(
						"route %q: queue_pattern literal prefix %q does not match any curing queue_prefix (declared prefixes: %s)",
						r.Name, literal, strings.Join(curingPrefixes, ", ")))
				}
			}
		}
	}
	if len(cfg.Routes) > 0 && len(defs) == 0 {
		problems = append(problems, "routes are configured but curing_dir loaded zero definitions")
	}
	if len(problems) > 0 {
		return fmt.Errorf("config/ValidateTannery: %s", strings.Join(problems, "; "))
	}
	return nil
}

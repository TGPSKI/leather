// Package curing loads curing workflow definitions and provides a first-match router
// for mapping intake events to curing workflows.
package curing

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/tgpski/leather/internal/model"
)

// LoadDir reads all *.curing.yaml files from dir and returns validated
// CuringDefinitions. If dir is empty or does not exist, an empty slice is
// returned without error. Individual files that fail to parse or validate are
// collected and returned as a combined error.
func LoadDir(dir string) ([]model.CuringDefinition, error) {
	if dir == "" {
		return nil, nil
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("curing/LoadDir: %w", err)
	}

	var defs []model.CuringDefinition
	var errs []string
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".curing.yaml") {
			continue
		}
		path := filepath.Join(dir, e.Name())
		def, err := parseCuringFile(path)
		if err != nil {
			errs = append(errs, fmt.Sprintf("%s: %v", e.Name(), err))
			continue
		}
		defs = append(defs, def)
	}
	if len(errs) > 0 {
		return defs, fmt.Errorf("curing/LoadDir: %s", strings.Join(errs, "; "))
	}
	return defs, nil
}

// parseCuringFile reads one *.curing.yaml file and returns a validated CuringDefinition.
func parseCuringFile(path string) (model.CuringDefinition, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return model.CuringDefinition{}, fmt.Errorf("parseCuringFile: %w", err)
	}
	def, err := parseCuringYAML(string(data))
	if err != nil {
		return model.CuringDefinition{}, err
	}
	def.SourcePath = path
	return def, nil
}

// parseCuringYAML parses the YAML content of a *.curing.yaml file using the
// stdlib-only line-by-line approach consistent with the worker/loader pattern.
func parseCuringYAML(src string) (model.CuringDefinition, error) {
	var def model.CuringDefinition

	// Track output sub-block state.
	inHideTypes := false
	inOutput := false

	// Track whether timeout_seconds was explicitly set.
	timeoutSet := false

	lines := strings.Split(src, "\n")
	for _, line := range lines {
		// Detect top-level vs indented context.
		trimmed := strings.TrimLeft(line, " \t")
		isIndented := len(line) > 0 && (line[0] == ' ' || line[0] == '\t')

		// Top-level key resets sub-block tracking.
		if !isIndented && trimmed != "" && !strings.HasPrefix(trimmed, "#") {
			if trimmed != "hide_types:" {
				inHideTypes = false
			}
			if trimmed != "output:" {
				inOutput = false
			}
		}

		// List items for hide_types.
		if inHideTypes && strings.HasPrefix(trimmed, "- ") {
			val := strings.TrimSpace(strings.TrimPrefix(trimmed, "- "))
			if val != "" {
				def.HideTypes = append(def.HideTypes, val)
			}
			continue
		}

		// Output sub-block keys.
		if inOutput && isIndented {
			k, v, ok := splitKV(trimmed)
			if !ok {
				continue
			}
			switch k {
			case "notify":
				def.Output.Notify = v
			case "queue":
				def.Output.Queue = v
			}
			continue
		}

		k, v, ok := splitKV(trimmed)
		if !ok {
			continue
		}

		switch k {
		case "hide_types":
			// "hide_types: value" on the same line is unusual; treat as block start.
			inHideTypes = true
			inOutput = false
		case "output":
			// "output: value" on the same line is unusual; treat as block start.
			inOutput = true
			inHideTypes = false
		case "name":
			inHideTypes = false
			inOutput = false
			def.Name = v
		case "description":
			inHideTypes = false
			inOutput = false
			def.Description = v
		case "agent":
			inHideTypes = false
			inOutput = false
			def.Agent = v
		case "queue":
			inHideTypes = false
			inOutput = false
			def.Queue = v
		case "page_size_bytes":
			inHideTypes = false
			inOutput = false
			n, err := strconv.Atoi(v)
			if err != nil {
				return model.CuringDefinition{}, fmt.Errorf("invalid page_size_bytes %q: %w", v, err)
			}
			def.PageSizeBytes = n
		case "max_attempts":
			inHideTypes = false
			inOutput = false
			n, err := strconv.Atoi(v)
			if err != nil {
				return model.CuringDefinition{}, fmt.Errorf("invalid max_attempts %q: %w", v, err)
			}
			def.MaxAttempts = n
		case "collect_size":
			inHideTypes = false
			inOutput = false
			n, err := strconv.Atoi(v)
			if err != nil {
				return model.CuringDefinition{}, fmt.Errorf("invalid collect_size %q: %w", v, err)
			}
			def.CollectSize = n
		case "collect_by":
			inHideTypes = false
			inOutput = false
			def.CollectBy = v
		case "queue_prefix":
			inHideTypes = false
			inOutput = false
			def.QueuePrefix = v
		case "timeout_seconds":
			inHideTypes = false
			inOutput = false
			n, err := strconv.Atoi(v)
			if err != nil {
				return model.CuringDefinition{}, fmt.Errorf("invalid timeout_seconds %q: %w", v, err)
			}
			def.TimeoutSeconds = n
			timeoutSet = true
		default:
			inHideTypes = false
			inOutput = false
		}
	}

	// Validate required fields.
	var errs []string
	if def.Name == "" {
		errs = append(errs, "missing required field: name")
	}
	if def.Agent == "" {
		errs = append(errs, "missing required field: agent")
	}
	if def.Queue == "" && def.QueuePrefix == "" {
		errs = append(errs, "missing required field: queue or queue_prefix")
	}
	if def.Queue != "" && def.QueuePrefix != "" {
		errs = append(errs, "queue and queue_prefix are mutually exclusive")
	}
	if len(errs) > 0 {
		return model.CuringDefinition{}, fmt.Errorf("%s", strings.Join(errs, "; "))
	}

	// Apply defaults for absent/zero fields.
	if def.PageSizeBytes == 0 {
		def.PageSizeBytes = 3800
	}
	if def.MaxAttempts == 0 {
		def.MaxAttempts = 3
	}
	// Only apply the default timeout when the field was absent.
	// An explicit timeout_seconds: 0 means "no timeout" and must be preserved.
	if !timeoutSet {
		def.TimeoutSeconds = 900
	}

	return def, nil
}

// splitKV splits "key: value" into (key, value, true).
// Returns ("", "", false) when no colon is found.
func splitKV(line string) (key, value string, ok bool) {
	idx := strings.Index(line, ":")
	if idx < 0 {
		return "", "", false
	}
	key = strings.TrimSpace(line[:idx])
	value = strings.TrimSpace(line[idx+1:])
	// Strip surrounding quotes from value.
	if len(value) >= 2 && ((value[0] == '"' && value[len(value)-1] == '"') ||
		(value[0] == '\'' && value[len(value)-1] == '\'')) {
		value = value[1 : len(value)-1]
	}
	if key == "" {
		return "", "", false
	}
	return key, value, true
}

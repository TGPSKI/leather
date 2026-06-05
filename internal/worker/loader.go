// Package worker loads and runs polling workers that push items into named queues.
//
// Workers are described by *.worker.yaml files.  LoadDir reads all files from a
// directory and returns validated WorkerDefinitions.  Definitions whose "type"
// field is not "http_poll" are rejected at load time.
//
// Supported YAML schema (all fields except name, type, interval, url, and
// output.queue are optional):
//
//	name: my-poller
//	type: http_poll
//	interval: 5m
//	url: "https://api.example.com/items"
//	headers:
//	  Authorization: "Bearer {{env:MY_TOKEN}}"
//	output:
//	  queue: my-queue
//	  dedup_key: number
package worker

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/tgpski/leather/internal/model"
	"github.com/tgpski/leather/internal/yamlx"
)

// supportedTypes is the set of worker types leather can run.
var supportedTypes = map[string]bool{
	"http_poll": true,
}

// LoadDir reads all *.worker.yaml files from dir and returns validated
// WorkerDefinitions.  If dir is empty or does not exist, an empty slice is
// returned without error.  Individual files that fail to parse are collected
// and returned as a combined error.
func LoadDir(dir string) ([]model.WorkerDefinition, error) {
	if dir == "" {
		return nil, nil
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("worker/LoadDir: %w", err)
	}

	var defs []model.WorkerDefinition
	var errs []string
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".worker.yaml") {
			continue
		}
		path := filepath.Join(dir, e.Name())
		def, err := parseWorkerFile(path)
		if err != nil {
			errs = append(errs, fmt.Sprintf("%s: %v", e.Name(), err))
			continue
		}
		defs = append(defs, def)
	}
	if len(errs) > 0 {
		return defs, fmt.Errorf("worker/LoadDir: %s", strings.Join(errs, "; "))
	}
	return defs, nil
}

// parseWorkerFile reads one *.worker.yaml file and returns a validated
// WorkerDefinition.
func parseWorkerFile(path string) (model.WorkerDefinition, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return model.WorkerDefinition{}, err
	}
	return parseWorkerYAML(string(data))
}

// parseWorkerYAML parses a worker YAML document using the stdlib-only block
// parser from internal/yamlx.
func parseWorkerYAML(src string) (model.WorkerDefinition, error) {
	vals, _ := yamlx.ParseBlock(src)

	name := vals["name"]
	if name == "" {
		return model.WorkerDefinition{}, fmt.Errorf("missing required field: name")
	}
	typ := vals["type"]
	if !supportedTypes[typ] {
		return model.WorkerDefinition{}, fmt.Errorf("unsupported worker type %q (supported: http_poll)", typ)
	}
	url := vals["url"]
	if url == "" {
		return model.WorkerDefinition{}, fmt.Errorf("missing required field: url")
	}
	intervalStr := vals["interval"]
	if intervalStr == "" {
		return model.WorkerDefinition{}, fmt.Errorf("missing required field: interval")
	}
	interval, err := time.ParseDuration(intervalStr)
	if err != nil {
		return model.WorkerDefinition{}, fmt.Errorf("invalid interval %q: %w", intervalStr, err)
	}

	// Parse output sub-block by scanning for indented keys.
	outputQueue, outputDedup := parseOutputBlock(src)
	if outputQueue == "" {
		return model.WorkerDefinition{}, fmt.Errorf("missing required field: output.queue")
	}

	// Parse headers sub-block.
	headers := parseHeadersBlock(src)

	return model.WorkerDefinition{
		Name:     name,
		Type:     typ,
		Interval: interval,
		URL:      url,
		Headers:  headers,
		Output: model.WorkerOutput{
			Queue:    outputQueue,
			DedupKey: outputDedup,
		},
	}, nil
}

// parseOutputBlock scans src for the "output:" sub-block and returns
// the queue and dedup_key values.
func parseOutputBlock(src string) (queue, dedupKey string) {
	inOutput := false
	for _, line := range strings.Split(src, "\n") {
		trimmed := strings.TrimLeft(line, " \t")
		if trimmed == "output:" {
			inOutput = true
			continue
		}
		if inOutput {
			// Stop at the next top-level key (no leading whitespace).
			if len(line) > 0 && line[0] != ' ' && line[0] != '\t' {
				break
			}
			k, v, ok := splitKV(trimmed)
			if !ok {
				continue
			}
			switch k {
			case "queue":
				queue = v
			case "dedup_key":
				dedupKey = strings.TrimPrefix(v, ".") // strip leading dot
			}
		}
	}
	return
}

// parseHeadersBlock scans src for the "headers:" sub-block and returns
// the key/value pairs found there.
func parseHeadersBlock(src string) map[string]string {
	headers := map[string]string{}
	inHeaders := false
	for _, line := range strings.Split(src, "\n") {
		trimmed := strings.TrimLeft(line, " \t")
		if trimmed == "headers:" {
			inHeaders = true
			continue
		}
		if inHeaders {
			if len(line) > 0 && line[0] != ' ' && line[0] != '\t' {
				break
			}
			k, v, ok := splitKV(trimmed)
			if !ok {
				continue
			}
			headers[k] = v
		}
	}
	if len(headers) == 0 {
		return nil
	}
	return headers
}

// splitKV splits "key: value" into (key, value, true).
// Returns ("", "", false) if the line has no colon.
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
	return key, value, key != ""
}

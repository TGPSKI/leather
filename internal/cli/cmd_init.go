package cli

import (
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/tgpski/leather/internal/schema"
)

const initConfigYAML = `agent_dir: agents
state_dir: .state
log_level: info

# LLM endpoint and model come from LEATHER_LLM_ENDPOINT / LEATHER_MODEL env vars.
# Defaults: http://localhost:11434 (override via env or set below).
# llm_endpoint: http://localhost:11434

max_tokens: 4096
completion_reserve: 512
summarize_threshold: 0.85

scheduler_tick: 1m
max_concurrent_jobs: 4
`

const initAgentMD = `---
name: my-agent
---

You are a helpful assistant. Respond with a single sentence confirming
that your leather agent is correctly configured and ready to run.
`

const initLifecycleYAML = `agent: my-agent
max_tokens: 4096

instances:
  - name: my-agent-hourly
    schedule: "0 * * * *"
    prompt: Confirm the agent is running.
`

const initMakefile = `# Makefile — leather project

LEATHER ?= leather

.PHONY: run validate clean

run:
	$(LEATHER) run --config config.yaml agents/my-agent.agent.md

validate:
	$(LEATHER) validate --config config.yaml

clean:
	rm -rf .state
`

// initFile writes content to path, failing if the file already exists unless
// overwrite is true. Reports written/skipped/error lines to out/errOut.
// Returns true if the file was successfully written.
func initFile(path, content string, overwrite bool, out, errOut io.Writer) bool {
	if !overwrite {
		if _, err := os.Stat(path); err == nil {
			fmt.Fprintf(errOut, "leather init: %s already exists (use --overwrite to replace)\n", path)
			return false
		}
	}
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		fmt.Fprintf(errOut, "leather init: mkdir %s: %v\n", filepath.Dir(path), err)
		return false
	}
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		fmt.Fprintf(errOut, "leather init: write %s: %v\n", path, err)
		return false
	}
	fmt.Fprintf(out, "created: %s\n", path)
	return true
}

// RunInit scaffolds a new leather project directory with a config, example
// agent, and Makefile. Fails closed on existing files unless --overwrite is set.
//
// Usage: leather init [--dir <path>] [--overwrite]
func RunInit(args []string, stdout, stderr io.Writer) int {
	fs := newFlagSet("init", stderr)

	defaultDir := "~/.leather"
	if home, err := os.UserHomeDir(); err == nil {
		defaultDir = filepath.Join(home, ".leather")
	}

	dir := fs.String("dir", defaultDir, "target directory for the new project (created if absent)")
	overwrite := fs.Bool("overwrite", false, "overwrite existing files")
	if !parseFlags(fs, args) {
		return 2
	}

	target, err := filepath.Abs(*dir)
	if err != nil {
		fmt.Fprintf(stderr, "leather init: %v\n", err)
		return 1
	}

	files := []struct {
		rel     string
		content string
	}{
		{"config.yaml", initConfigYAML},
		{"agents/my-agent.agent.md", initAgentMD},
		{"agents/my-agent.lifecycle.yaml", initLifecycleYAML},
		{"Makefile", initMakefile},
	}

	exitCode := 0
	for _, f := range files {
		path := filepath.Join(target, f.rel)
		if !initFile(path, f.content, *overwrite, stdout, stderr) {
			exitCode = 1
		}
	}
	if exitCode != 0 {
		return exitCode
	}

	// Validate the scaffolded files using schema checks (syntax only — no
	// runtime model resolution, since the user supplies LEATHER_MODEL at run
	// time, not at init time).
	if code := runInitValidate(target, stderr); code != 0 {
		fmt.Fprintf(stderr, "leather init: validation failed — scaffolded files may be malformed\n")
		return code
	}

	fmt.Fprintf(stdout, "\nProject initialised in %s\n", target)
	fmt.Fprintf(stdout, "  leather run --config config.yaml agents/my-agent.agent.md\n")
	return 0
}

// runInitValidate schema-validates the scaffolded config and agent files.
// It does not perform runtime model resolution so init succeeds without LEATHER_MODEL set.
func runInitValidate(target string, errOut io.Writer) int {
	type check struct {
		rel      string
		validate func(string) []schema.Violation
	}
	checks := []check{
		{"config.yaml", schema.ValidateConfigYAML},
		{"agents/my-agent.lifecycle.yaml", schema.ValidateLifecycleYAML},
	}
	exitCode := 0
	for _, c := range checks {
		path := filepath.Join(target, c.rel)
		data, err := os.ReadFile(path)
		if err != nil {
			fmt.Fprintf(errOut, "leather init: read %s: %v\n", c.rel, err)
			exitCode = 1
			continue
		}
		viols := c.validate(string(data))
		for _, v := range viols {
			fmt.Fprintf(errOut, "leather init: %s: field %q: %s\n", c.rel, v.Field, v.Message)
			exitCode = 1
		}
	}
	return exitCode
}

package mcp

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/tgpski/leather/internal/model"
)

// resolveEnvVars resolves a slice of MCPEnvVar into "KEY=VALUE" strings
// suitable for appending to exec.Cmd.Env. Resolution order per entry:
//  1. pass-store: runs `pass show <path>` if Pass is non-empty.
//  2. environment variable: reads os.Getenv(Env) if Env is non-empty.
//  3. Error when neither source yields a non-empty value.
//
// Secret values are never logged; only the variable name and pass path are.
func resolveEnvVars(ctx context.Context, vars []model.MCPEnvVar) ([]string, error) {
	out := make([]string, 0, len(vars))
	for _, v := range vars {
		val, err := resolveEnvVar(ctx, v)
		if err != nil {
			return nil, fmt.Errorf("mcp/resolveEnvVars: %w", err)
		}
		out = append(out, v.Name+"="+val)
	}
	return out, nil
}

// resolveEnvVar resolves a single MCPEnvVar to its string value.
func resolveEnvVar(ctx context.Context, v model.MCPEnvVar) (string, error) {
	if v.Pass != "" {
		val, err := runPass(ctx, v.Pass)
		if err == nil && val != "" {
			return val, nil
		}
		// pass failed or returned empty — fall through to env var.
	}
	if v.Env != "" {
		if val := os.Getenv(v.Env); val != "" {
			return val, nil
		}
	}
	return "", fmt.Errorf("env var %q: no value resolved (pass=%q env=%q)", v.Name, v.Pass, v.Env)
}

// runPass runs `pass show path` and returns the first line of stdout.
func runPass(ctx context.Context, path string) (string, error) {
	passBin, err := exec.LookPath("pass")
	if err != nil {
		return "", fmt.Errorf("pass binary not in PATH: %w", err)
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, passBin, "show", path).Output() //nolint:gosec // path is operator-supplied config, not user input
	if err != nil {
		return "", fmt.Errorf("pass show %q: %w", path, err)
	}
	lines := strings.SplitN(strings.TrimRight(string(out), "\n"), "\n", 2)
	return strings.TrimSpace(lines[0]), nil
}

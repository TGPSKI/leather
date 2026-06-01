// Package secret resolves credentials from operator-friendly sources
// without ever passing the resolved value through structured logs.
//
// A Ref describes a credential lookup with three optional sources, tried
// in order:
//
//  1. Inline literal (Value) — useful for tests and bring-up; never the
//     recommended production form.
//  2. Unix pass-store path (Pass) — runs `pass show <path>` and returns
//     the first non-empty line of stdout.
//  3. Environment variable name (Env) — read via os.Getenv.
//
// Pass and Env work as a fallback pair: if Pass is set but `pass show`
// fails or returns empty, Env is consulted next. This matches the
// existing MCPEnvVar resolution semantics in internal/mcp/env.go.
package secret

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"
)

// Ref describes a single credential lookup.
//
// At most one of Value / Pass / Env should be the primary source;
// Env may additionally be supplied as a fallback when Pass is set.
type Ref struct {
	// Value is an inline literal secret. When non-empty it short-circuits
	// resolution. Use only for tests, fixtures, or operator bring-up.
	Value string
	// Pass is the Unix pass-store path (e.g. "openai/api-key").
	Pass string
	// Env is the environment variable name to read.
	Env string
}

// IsZero reports whether r has no source configured.
func (r Ref) IsZero() bool {
	return r.Value == "" && r.Pass == "" && r.Env == ""
}

// Resolve returns the resolved secret value for r.
//
// Resolution order:
//  1. r.Value (inline literal) — returned as-is when non-empty.
//  2. r.Pass — `pass show <path>`, first line of stdout.
//  3. r.Env — os.Getenv(r.Env).
//
// Returns ("", nil) when r.IsZero() — callers treat absence as "no auth".
// Returns a non-nil error only when a configured source was tried and
// produced nothing usable.
func Resolve(ctx context.Context, r Ref) (string, error) {
	if r.Value != "" {
		return r.Value, nil
	}
	if r.IsZero() {
		return "", nil
	}
	if r.Pass != "" {
		val, err := runPass(ctx, r.Pass)
		if err == nil && val != "" {
			return val, nil
		}
		// pass failed or returned empty — fall through to env var.
	}
	if r.Env != "" {
		if val := os.Getenv(r.Env); val != "" {
			return val, nil
		}
	}
	return "", fmt.Errorf("secret/Resolve: no value (pass=%q env=%q)", r.Pass, r.Env)
}

// runPass runs `pass show path` and returns the first line of stdout.
// Times out after 5 seconds to avoid hanging on a stuck gpg-agent prompt.
func runPass(ctx context.Context, path string) (string, error) {
	passBin, err := exec.LookPath("pass")
	if err != nil {
		return "", fmt.Errorf("pass binary not in PATH: %w", err)
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, passBin, "show", path).Output() //nolint:gosec // path is operator-supplied config
	if err != nil {
		return "", fmt.Errorf("pass show %q: %w", path, err)
	}
	lines := strings.SplitN(strings.TrimRight(string(out), "\n"), "\n", 2)
	return strings.TrimSpace(lines[0]), nil
}

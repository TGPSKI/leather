package notify

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/tgpski/leather/internal/model"
)

// resolve returns the secret value from a SecretRef using the resolution order:
//  1. Unix pass-store (preferred): runs `pass show <path>` via os/exec.
//  2. Environment variable (fallback): os.Getenv(ref.Env).
//  3. Error when both are absent or both return empty strings.
//
// The resolved value is never logged. The pass path (but not the value) is
// logged at debug level by callers where a logger is available.
func resolve(ctx context.Context, ref model.SecretRef) (string, error) {
	if ref.Pass != "" {
		val, err := resolvePass(ctx, ref.Pass)
		if err == nil && val != "" {
			return val, nil
		}
		// pass failed or returned empty — fall through to env var.
	}
	if ref.Env != "" {
		if val := os.Getenv(ref.Env); val != "" {
			return val, nil
		}
	}
	return "", fmt.Errorf("notify/secret: no value resolved (pass=%q env=%q)", ref.Pass, ref.Env)
}

// resolvePass runs `pass show path` and returns the first line of stdout.
// It uses exec.LookPath to locate the pass binary; if not found, it returns
// an error so the caller can fall back to the env var.
func resolvePass(ctx context.Context, path string) (string, error) {
	passBin, err := exec.LookPath("pass")
	if err != nil {
		return "", fmt.Errorf("notify/secret: pass binary not in PATH: %w", err)
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, passBin, "show", path).Output() //nolint:gosec // path is operator-supplied config, not user input
	if err != nil {
		return "", fmt.Errorf("notify/secret: pass show %q: %w", path, err)
	}
	// pass prints additional metadata after the first line; only the token matters.
	lines := strings.SplitN(strings.TrimRight(string(out), "\n"), "\n", 2)
	return strings.TrimSpace(lines[0]), nil
}

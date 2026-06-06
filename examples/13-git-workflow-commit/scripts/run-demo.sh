#!/usr/bin/env bash
# run-demo.sh — end-to-end demo for 13-git-workflow-commit.
#
# Two-phase demo in a temporary git repository:
#
#   Phase 1 — brand-new files (untracked)
#     Creates 4 new source files with enough content that the overview diff
#     will be capped. Validates that the planner calls git_file_diff for large
#     new files and writes accurate per-file commit messages.
#
#   Phase 2 — modifications (tracked files with edits)
#     Edits two of those files. Validates that the planner handles unstaged
#     modifications and composes context-aware update messages.
#
# Between phases the git log is printed so you can inspect the results before
# the next workflow run starts.
#
# Requirements:
#   leather and shell-mcp on PATH (or LEATHER / SHELLMCP env vars)
#   LEATHER_LLM_ENDPOINT  — local OpenAI-compatible endpoint
#   LEATHER_MODEL         — model served by that endpoint
#   LEATHER_GIT_SIGNING_KEY — GPG key ID on the current keyring
#
# Usage (from examples/):
#   LEATHER_GIT_SIGNING_KEY=<key-id> make 13
#   LEATHER_GIT_SIGNING_KEY=<key-id> bash 13-git-workflow-commit/scripts/run-demo.sh

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
EXAMPLE_DIR="$(cd "${SCRIPT_DIR}/.." && pwd)"

LEATHER="${LEATHER:-leather}"
SHELLMCP="${SHELLMCP:-shell-mcp}"
LEATHER_LLM_ENDPOINT="${LEATHER_LLM_ENDPOINT:-http://localhost:11434}"
LEATHER_MODEL="${LEATHER_MODEL:-llama3}"
LEATHER_GIT_SIGNING_KEY="${LEATHER_GIT_SIGNING_KEY:-}"

# ---------------------------------------------------------------------------
# Pre-flight
# ---------------------------------------------------------------------------

fail() { printf '\n\033[31merror:\033[0m %s\n' "$*" >&2; exit 2; }

[ -n "$LEATHER_GIT_SIGNING_KEY" ] || fail "LEATHER_GIT_SIGNING_KEY is not set.
  List available keys: gpg --list-secret-keys --keyid-format SHORT
  Then: export LEATHER_GIT_SIGNING_KEY=<key-id> && make 13"

command -v "$LEATHER"  >/dev/null 2>&1 || fail "leather binary not found. Run: make build"
command -v "$SHELLMCP" >/dev/null 2>&1 || fail "shell-mcp binary not found. Run: make build-shell-mcp"
gpg --list-secret-keys "$LEATHER_GIT_SIGNING_KEY" >/dev/null 2>&1 || \
  fail "GPG key $LEATHER_GIT_SIGNING_KEY not found in secret keyring."

# ---------------------------------------------------------------------------
# Temp repo
# ---------------------------------------------------------------------------

WORK_DIR="$(mktemp -d /tmp/leather-git-demo.XXXXXX)"
DEMO_CLEANED=0
cleanup() {
  if [ "$DEMO_CLEANED" = "0" ]; then
    printf '\n\033[90mcleaning up %s\033[0m\n' "$WORK_DIR"
    rm -rf "$WORK_DIR"
  fi
}
trap cleanup EXIT

printf '\n\033[1m13-git-workflow-commit demo\033[0m\n'
printf '  LLM:         %s  (%s)\n' "$LEATHER_LLM_ENDPOINT" "$LEATHER_MODEL"
printf '  signing key: %s\n' "$LEATHER_GIT_SIGNING_KEY"
printf '  work dir:    %s\n\n' "$WORK_DIR"

git -C "$WORK_DIR" init -q
git -C "$WORK_DIR" config user.email "demo@leather.local"
git -C "$WORK_DIR" config user.name  "Leather Demo"
git -C "$WORK_DIR" config commit.gpgsign false
git -C "$WORK_DIR" config user.signingkey "$LEATHER_GIT_SIGNING_KEY"

# Seed HEAD so git commands work.
printf '# leather demo repo\n' > "$WORK_DIR/README.md"
git -C "$WORK_DIR" add README.md
git -C "$WORK_DIR" -c user.email=demo@leather.local -c user.name="Leather Demo" commit -q -m "init: seed repo"

run_workflow() {
  local label="$1"
  printf '\n\033[1m==> %s\033[0m\n\n' "$label"
  mkdir -p "${EXAMPLE_DIR}/.state"
  cd "$WORK_DIR"
  printf 'Commit all changed files in cwd: %s\nSIGNING_KEY: %s\n' \
    "$WORK_DIR" "$LEATHER_GIT_SIGNING_KEY" | \
    LEATHER_LLM_ENDPOINT="$LEATHER_LLM_ENDPOINT" \
    LEATHER_MODEL="$LEATHER_MODEL" \
    "$LEATHER" workflow run \
      --config  "${EXAMPLE_DIR}/config.yaml" \
      --tannery "${EXAMPLE_DIR}/tannery.yaml" \
      --kind    cli.git.commit_all \
      --source  cli \
      --settle  2s
}

show_log() {
  printf '\n\033[1mgit log:\033[0m\n'
  git -C "$WORK_DIR" log --oneline | head -20
  printf '\n\033[1mgit status:\033[0m\n'
  git -C "$WORK_DIR" status --short
}

pause() {
  printf '\n\033[90m%s\033[0m\n' "$1"
  printf '\033[90mPress Enter to continue, or Ctrl-C to keep the repo and exit.\033[0m\n'
  read -r _
}

# ---------------------------------------------------------------------------
# Phase 1: brand-new files (untracked)
#   Sizes are intentionally large enough to exceed LEATHER_GIT_DIFF_LINES=20
#   so the planner must call git_file_diff to read the full content.
# ---------------------------------------------------------------------------

printf '\n\033[33mPhase 1: brand-new files (untracked)\033[0m\n'
printf 'Creating 4 new source files with 30-50 lines each...\n'

cat > "$WORK_DIR/server.go" <<'GO'
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"
)

type Server struct {
	mux    *http.ServeMux
	addr   string
	logger *log.Logger
}

func NewServer(addr string) *Server {
	s := &Server{
		mux:    http.NewServeMux(),
		addr:   addr,
		logger: log.New(os.Stdout, "[server] ", log.LstdFlags),
	}
	s.mux.HandleFunc("/health", s.handleHealth)
	s.mux.HandleFunc("/ready",  s.handleReady)
	return s
}

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
}

func (s *Server) handleReady(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"status": "ready"})
}

func (s *Server) Run(ctx context.Context) error {
	srv := &http.Server{Addr: s.addr, Handler: s.mux}
	go func() {
		<-ctx.Done()
		shutCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		srv.Shutdown(shutCtx)
	}()
	s.logger.Printf("listening on %s", s.addr)
	if err := srv.ListenAndServe(); err != http.ErrServerClosed {
		return fmt.Errorf("server: %w", err)
	}
	return nil
}

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	addr := os.Getenv("ADDR")
	if addr == "" {
		addr = ":8080"
	}
	if err := NewServer(addr).Run(ctx); err != nil {
		log.Fatalf("fatal: %v", err)
	}
}
GO

cat > "$WORK_DIR/config.go" <<'GO'
package main

import (
	"fmt"
	"os"
	"strconv"
	"time"
)

// Config holds all runtime parameters parsed from environment variables.
type Config struct {
	Addr            string
	ReadTimeout     time.Duration
	WriteTimeout    time.Duration
	ShutdownTimeout time.Duration
	MaxBodyBytes    int64
	LogLevel        string
}

func LoadConfig() (*Config, error) {
	c := &Config{
		Addr:            getenv("ADDR", ":8080"),
		ReadTimeout:     mustDuration("READ_TIMEOUT", "5s"),
		WriteTimeout:    mustDuration("WRITE_TIMEOUT", "10s"),
		ShutdownTimeout: mustDuration("SHUTDOWN_TIMEOUT", "15s"),
		MaxBodyBytes:    mustInt64("MAX_BODY_BYTES", 1<<20),
		LogLevel:        getenv("LOG_LEVEL", "info"),
	}
	if c.ReadTimeout > c.WriteTimeout {
		return nil, fmt.Errorf("READ_TIMEOUT must not exceed WRITE_TIMEOUT")
	}
	return c, nil
}

func getenv(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func mustDuration(key, def string) time.Duration {
	raw := getenv(key, def)
	d, err := time.ParseDuration(raw)
	if err != nil {
		panic(fmt.Sprintf("bad duration %s=%q: %v", key, raw, err))
	}
	return d
}

func mustInt64(key string, def int64) int64 {
	raw := os.Getenv(key)
	if raw == "" {
		return def
	}
	v, err := strconv.ParseInt(raw, 10, 64)
	if err != nil {
		panic(fmt.Sprintf("bad int64 %s=%q: %v", key, raw, err))
	}
	return v
}
GO

cat > "$WORK_DIR/metrics.go" <<'GO'
package main

import (
	"expvar"
	"net/http"
	"sync/atomic"
	"time"
)

var (
	requestsTotal   = expvar.NewInt("requests_total")
	requestsActive  = expvar.NewInt("requests_active")
	errorsTotal     = expvar.NewInt("errors_total")
	startTime       = time.Now()
)

type metricsMiddleware struct {
	next http.Handler
}

func WithMetrics(next http.Handler) http.Handler {
	return &metricsMiddleware{next: next}
}

func (m *metricsMiddleware) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	requestsTotal.Add(1)
	requestsActive.Add(1)
	defer requestsActive.Add(-1)

	rw := &responseWriter{ResponseWriter: w, code: http.StatusOK}
	m.next.ServeHTTP(rw, r)

	if rw.code >= 500 {
		errorsTotal.Add(1)
	}
}

type responseWriter struct {
	http.ResponseWriter
	code    int
	written atomic.Bool
}

func (rw *responseWriter) WriteHeader(code int) {
	if rw.written.CompareAndSwap(false, true) {
		rw.code = code
		rw.ResponseWriter.WriteHeader(code)
	}
}

func uptimeSeconds() float64 {
	return time.Since(startTime).Seconds()
}
GO

cat > "$WORK_DIR/Makefile" <<'MAKE'
.PHONY: build test lint clean run

build:
	CGO_ENABLED=0 go build -o ./app ./...

test:
	go test -race ./...

lint:
	golangci-lint run

clean:
	rm -f app

run: build
	./app
MAKE

printf 'files created: server.go (%d lines)  config.go (%d lines)  metrics.go (%d lines)  Makefile\n' \
  "$(wc -l < "$WORK_DIR/server.go")" \
  "$(wc -l < "$WORK_DIR/config.go")" \
  "$(wc -l < "$WORK_DIR/metrics.go")"

run_workflow "Phase 1: commit new files (planner reads full content via git_file_diff)"
show_log
pause "Phase 1 complete. Review commits above."

# ---------------------------------------------------------------------------
# Phase 2: modifications to existing files
#   Edits server.go (add middleware wiring) and config.go (add validation).
#   README.md gets a section added. Tests both the unstaged-modification path
#   and a smaller diff where no git_file_diff call is needed.
# ---------------------------------------------------------------------------

printf '\n\033[33mPhase 2: modifications to committed files\033[0m\n'
printf 'Editing server.go, config.go, and README.md...\n'

# Patch server.go: wire metrics middleware into NewServer
sed -i 's|s.mux.HandleFunc("/health", s.handleHealth)|s.mux.HandleFunc("/health", s.handleHealth)\n\ts.mux.Handle("/debug/vars", WithMetrics(http.DefaultServeMux))|' \
  "$WORK_DIR/server.go"

# Patch config.go: add a Validate method
cat >> "$WORK_DIR/config.go" <<'GO'

// Validate returns an error if any Config field is out of range.
func (c *Config) Validate() error {
	if c.MaxBodyBytes <= 0 {
		return fmt.Errorf("MAX_BODY_BYTES must be positive, got %d", c.MaxBodyBytes)
	}
	if c.LogLevel != "debug" && c.LogLevel != "info" && c.LogLevel != "warn" && c.LogLevel != "error" {
		return fmt.Errorf("LOG_LEVEL must be one of debug|info|warn|error, got %q", c.LogLevel)
	}
	return nil
}
GO

# Patch README with a usage section
cat >> "$WORK_DIR/README.md" <<'MD'

## Usage

```bash
make build
ADDR=:9090 LOG_LEVEL=debug ./app
```

Environment variables: `ADDR`, `READ_TIMEOUT`, `WRITE_TIMEOUT`,
`SHUTDOWN_TIMEOUT`, `MAX_BODY_BYTES`, `LOG_LEVEL`.
MD

run_workflow "Phase 2: commit modifications (server wiring, config validation, usage docs)"
show_log

printf '\n\033[90mDemo repo is at %s\033[0m\n' "$WORK_DIR"
pause "Demo complete. Press Enter to clean up."
DEMO_CLEANED=1
rm -rf "$WORK_DIR"
printf '\033[90mcleaned up.\033[0m\n'

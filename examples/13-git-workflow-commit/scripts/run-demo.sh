#!/usr/bin/env bash
# run-demo.sh — end-to-end demo for 13-git-workflow-commit.
#
# Creates a temporary git repository, populates it with several sample files,
# runs `leather workflow run` to have the planner inspect the changes and
# enqueue per-file GPG-signed commits, then pauses so you can inspect the
# result before cleanup.
#
# Requirements:
#   - leather binary on PATH (or set LEATHER env var)
#   - shell-mcp binary on PATH (or set SHELLMCP env var)
#   - LEATHER_LLM_ENDPOINT pointing at a local OpenAI-compatible endpoint
#   - LEATHER_MODEL set to a model served by that endpoint
#   - LEATHER_GIT_SIGNING_KEY set to a GPG key ID on the current keyring
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

# --- pre-flight ---------------------------------------------------------------

if [ -z "$LEATHER_GIT_SIGNING_KEY" ]; then
  printf '\n\033[31merror:\033[0m LEATHER_GIT_SIGNING_KEY is not set.\n' >&2
  printf '  Set it to a GPG key ID on your keyring:\n' >&2
  printf '    export LEATHER_GIT_SIGNING_KEY=$(gpg --list-secret-keys --keyid-format SHORT | grep sec | awk '"'"'{print $2}'"'"' | cut -d/ -f2 | head -1)\n' >&2
  printf '    make 13\n' >&2
  exit 2
fi

if ! command -v "$LEATHER" >/dev/null 2>&1; then
  printf '\n\033[31merror:\033[0m leather binary not found. Build it first:\n  make build\n' >&2
  exit 2
fi

if ! command -v "$SHELLMCP" >/dev/null 2>&1; then
  printf '\n\033[31merror:\033[0m shell-mcp binary not found. Build it first:\n  make build-shell-mcp\n' >&2
  exit 2
fi

if ! gpg --list-secret-keys "$LEATHER_GIT_SIGNING_KEY" >/dev/null 2>&1; then
  printf '\n\033[31merror:\033[0m GPG key %s not found in secret keyring.\n' "$LEATHER_GIT_SIGNING_KEY" >&2
  printf '  List available keys: gpg --list-secret-keys --keyid-format SHORT\n' >&2
  exit 2
fi

# --- temp repo setup ----------------------------------------------------------

WORK_DIR="$(mktemp -d /tmp/leather-git-demo.XXXXXX)"
cleanup() {
  printf '\n\033[90mcleaning up %s\033[0m\n' "$WORK_DIR"
  rm -rf "$WORK_DIR"
}

printf '\n\033[1m13-git-workflow-commit demo\033[0m\n'
printf '  LLM: %s  model: %s\n' "$LEATHER_LLM_ENDPOINT" "$LEATHER_MODEL"
printf '  signing key: %s\n' "$LEATHER_GIT_SIGNING_KEY"
printf '  work dir: %s\n\n' "$WORK_DIR"

# Init bare git repo in temp dir.
git -C "$WORK_DIR" init -q
git -C "$WORK_DIR" config user.email "demo@leather.local"
git -C "$WORK_DIR" config user.name "Leather Demo"
git -C "$WORK_DIR" config commit.gpgsign false   # base commits unsigned for speed
git -C "$WORK_DIR" config user.signingkey "$LEATHER_GIT_SIGNING_KEY"

# Seed an initial commit so HEAD exists.
printf '# leather demo repo\n' > "$WORK_DIR/README.md"
git -C "$WORK_DIR" add README.md
git -C "$WORK_DIR" commit -q -m "init"

# Write sample changed files.
cat > "$WORK_DIR/api.go" <<'GOEOF'
package main

import "net/http"

func handleHealth(w http.ResponseWriter, r *http.Request) {
    w.WriteHeader(http.StatusOK)
    w.Write([]byte("ok"))
}
GOEOF

cat > "$WORK_DIR/config.yaml" <<'YAMLEOF'
server:
  port: 8080
  timeout: 30s
database:
  host: localhost
  port: 5432
  name: appdb
YAMLEOF

cat > "$WORK_DIR/worker.go" <<'GOEOF'
package main

import (
    "context"
    "log"
    "time"
)

func runWorker(ctx context.Context) {
    ticker := time.NewTicker(5 * time.Second)
    defer ticker.Stop()
    for {
        select {
        case <-ctx.Done():
            return
        case <-ticker.C:
            log.Println("worker: tick")
        }
    }
}
GOEOF

cat > "$WORK_DIR/Makefile" <<'MKEOF'
.PHONY: build test

build:
	go build ./...

test:
	go test ./...
MKEOF

printf 'created 4 sample files in %s\n' "$WORK_DIR"
printf '  api.go  config.yaml  worker.go  Makefile\n\n'

# --- run workflow -------------------------------------------------------------

printf '==> running leather workflow run\n\n'

# The workflow run needs its state dir; use a subdir of the example .state.
mkdir -p "${EXAMPLE_DIR}/.state"

# mcp-servers.yaml references shell-mcp by bare command name; ensure it's on PATH.
export PATH="${PATH}"

# Run from WORK_DIR so git tool calls see the sample repo.
cd "$WORK_DIR"

printf 'Commit all changed files in cwd: %s\nSIGNING_KEY: %s\n' \
  "$WORK_DIR" "$LEATHER_GIT_SIGNING_KEY" | \
  LEATHER_LLM_ENDPOINT="$LEATHER_LLM_ENDPOINT" \
  LEATHER_MODEL="$LEATHER_MODEL" \
  "$LEATHER" workflow run \
    --config "${EXAMPLE_DIR}/config.yaml" \
    --tannery "${EXAMPLE_DIR}/tannery.yaml" \
    --kind cli.git.commit_all \
    --source cli \
    --settle 2s

# --- post-run inspection ------------------------------------------------------

printf '\n\033[1mgit log in demo repo:\033[0m\n'
git -C "$WORK_DIR" log --oneline --show-signature 2>/dev/null | head -20 || \
  git -C "$WORK_DIR" log --oneline | head -20

printf '\n\033[1mgit status:\033[0m\n'
git -C "$WORK_DIR" status --short

printf '\n\033[90mDemo repo is at %s\033[0m\n' "$WORK_DIR"
printf '\033[90mPress Enter to clean up, or Ctrl-C to keep the repo and inspect it.\033[0m\n'
read -r _

trap cleanup EXIT

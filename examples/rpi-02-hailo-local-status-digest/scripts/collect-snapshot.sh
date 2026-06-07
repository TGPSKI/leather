#!/usr/bin/env bash
set -u

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
OUT="$ROOT/.snapshot/status.txt"
LEATHER_ROOT="${LEATHER_ROOT:-}"
if [ -z "$LEATHER_ROOT" ]; then
  LEATHER_ROOT="$(git -C "$ROOT" rev-parse --show-toplevel 2>/dev/null || true)"
fi
if [ -z "$LEATHER_ROOT" ]; then
  LEATHER_ROOT="$(cd "$ROOT/../.." && pwd)"
fi

mkdir -p "$ROOT/.snapshot"

section() {
  printf '\n## %s\n' "$1" >> "$OUT"
}

run() {
  local label="$1"
  shift
  printf '\n$ %s\n' "$*" >> "$OUT"
  if "$@" >> "$OUT" 2>&1; then
    printf '[exit=0]\n' >> "$OUT"
  else
    local code=$?
    printf '[exit=%s]\n' "$code" >> "$OUT"
  fi
}

: > "$OUT"

section "snapshot"
printf 'generated_at=%s\n' "$(date -Is)" >> "$OUT"
printf 'host=%s\n' "$(hostname)" >> "$OUT"
printf 'pwd=%s\n' "$ROOT" >> "$OUT"

section "system"
run "kernel" uname -srmo
run "uptime" uptime -p
run "loadavg" cat /proc/loadavg

if command -v free >/dev/null 2>&1; then
  run "memory" free -m
fi

run "disk-root" df -h /

section "endpoint"
if [ -n "${LEATHER_LLM_ENDPOINT:-}" ]; then
  printf 'LEATHER_LLM_ENDPOINT=%s\n' "$LEATHER_LLM_ENDPOINT" >> "$OUT"
else
  printf 'LEATHER_LLM_ENDPOINT=<unset>\n' >> "$OUT"
fi

if [ -n "${LEATHER_MODEL:-}" ]; then
  printf 'LEATHER_MODEL=%s\n' "$LEATHER_MODEL" >> "$OUT"
else
  printf 'LEATHER_MODEL=<unset>\n' >> "$OUT"
fi

if command -v curl >/dev/null 2>&1; then
  run "proxy-health" curl -fsS "${LEATHER_LLM_ENDPOINT:-http://localhost:8080}/health"
  run "hailo-tags" bash -lc 'curl -fsS http://127.0.0.1:8000/api/tags | head -c 800'
fi

section "leather-repo"
run "git-branch" git -C "$LEATHER_ROOT" branch --show-current
run "git-status-short" git -C "$LEATHER_ROOT" status --short
run "example-count" bash -lc "find '$LEATHER_ROOT/examples' -maxdepth 1 -type d | wc -l"

section "example-state"
run "state-files" bash -lc "find '$ROOT/.state' -type f 2>/dev/null | wc -l"
run "snapshot-bytes" wc -c "$OUT"

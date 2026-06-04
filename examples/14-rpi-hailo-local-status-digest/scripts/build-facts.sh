#!/usr/bin/env bash
set -u

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
OUT="$ROOT/.snapshot/facts.env"
LEATHER_ROOT="${LEATHER_ROOT:-}"
if [ -z "$LEATHER_ROOT" ]; then
  LEATHER_ROOT="$(git -C "$ROOT" rev-parse --show-toplevel 2>/dev/null || true)"
fi
if [ -z "$LEATHER_ROOT" ]; then
  LEATHER_ROOT="$(cd "$ROOT/../.." && pwd)"
fi

endpoint="${LEATHER_LLM_ENDPOINT:-http://localhost:8080}"
model="${LEATHER_MODEL:-qwen3:1.7b}"
mkdir -p "$ROOT/.snapshot"

statuses=""
checks=""

clean() {
  printf '%s' "$1" \
    | tr '\n\r' '  ' \
    | sed 's/[\\"]/ /g; s/[{}[\]]/( /g; s/[[:space:]]\+/ /g; s/^ //; s/ $//; s/;/,/g' \
    | cut -c1-300
}

add_check() {
  local name="$1"
  local status="$2"
  local observation="$3"
  statuses="${statuses} ${status}"
  checks="${checks}CHECK_${name}=${status} - $(clean "$observation"); "
}

load="$(cut -d' ' -f1-3 /proc/loadavg 2>/dev/null || echo unknown)"
add_check "system_load" "ok" "load averages are ${load// /, }"

disk_pct="$(df -P / | awk 'NR==2 {gsub("%","",$5); print $5}')"
if [ -z "$disk_pct" ]; then
  add_check "root_disk" "watch" "root filesystem usage could not be read"
elif [ "$disk_pct" -ge 90 ]; then
  add_check "root_disk" "action" "root filesystem is ${disk_pct}% used"
elif [ "$disk_pct" -ge 80 ]; then
  add_check "root_disk" "watch" "root filesystem is ${disk_pct}% used"
else
  add_check "root_disk" "ok" "root filesystem is ${disk_pct}% used"
fi

if command -v curl >/dev/null 2>&1 && curl -fsS "${endpoint%/}/health" >/dev/null 2>&1; then
  add_check "proxy_health" "ok" "OpenAI compatibility proxy health endpoint responded"
else
  add_check "proxy_health" "action" "OpenAI compatibility proxy health check failed"
fi

if command -v curl >/dev/null 2>&1 && curl -fsS "http://127.0.0.1:8000/api/tags" >/dev/null 2>&1; then
  add_check "hailo_tags" "ok" "Hailo-Ollama model list endpoint responded"
else
  add_check "hailo_tags" "action" "Hailo-Ollama model list endpoint failed"
fi

if [ -d "$LEATHER_ROOT/.git" ]; then
  changed="$(git -C "$LEATHER_ROOT" status --short 2>/dev/null | wc -l | tr -d ' ')"
  if [ "$changed" -gt 0 ]; then
    add_check "leather_git_status" "watch" "Leather repo has $changed changed or untracked paths"
  else
    add_check "leather_git_status" "ok" "Leather repo is clean"
  fi
else
  add_check "leather_git_status" "watch" "Leather source directory is not a Git checkout on this device"
fi

state_entries="$(find "$ROOT/.state" -type f 2>/dev/null | wc -l | tr -d ' ')"
add_check "example_state_files" "ok" "example state currently has $state_entries filesystem entries"

deterministic_status="ok"
case " $statuses " in
  *" action "*) deterministic_status="action" ;;
  *" watch "*) deterministic_status="watch" ;;
esac

should_notify="false"
[ "$deterministic_status" = "action" ] && should_notify="true"

{
  printf 'DETERMINISTIC_STATUS=%s\n' "$deterministic_status"
  printf 'SHOULD_NOTIFY=%s\n' "$should_notify"
  printf 'NOTIFY_RULE=%s\n' "notify only on action status; watch is recorded but quiet"
  printf 'CHECKS=%s\n' "$checks"
  printf 'ENDPOINT=%s\n' "$(clean "$endpoint")"
  printf 'MODEL=%s\n' "$(clean "$model")"
} > "$OUT"

cat "$OUT"

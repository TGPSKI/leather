#!/usr/bin/env bash
set -u

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
LEATHER_ROOT="${LEATHER_ROOT:-}"
if [ -z "$LEATHER_ROOT" ]; then
  LEATHER_ROOT="$(git -C "$ROOT" rev-parse --show-toplevel 2>/dev/null || true)"
fi
if [ -z "$LEATHER_ROOT" ]; then
  LEATHER_ROOT="$(cd "$ROOT/../.." && pwd)"
fi
OUT="$ROOT/sample/status.snapshot.txt"

endpoint="${LEATHER_LLM_ENDPOINT:-http://localhost:8080}"
model="${LEATHER_MODEL:-qwen3:1.7b}"

statuses=()

add_check() {
  local name="$1"
  local status="$2"
  local observation="$3"
  statuses+=("$status")
  printf 'CHECK %s: %s - %s\n' "$name" "$status" "$observation" >> "$OUT"
}

overall_status() {
  local s
  for s in "${statuses[@]}"; do
    [ "$s" = "action" ] && echo action && return
  done
  for s in "${statuses[@]}"; do
    [ "$s" = "watch" ] && echo watch && return
  done
  echo ok
}

mkdir -p "$ROOT/sample"
: > "$OUT"

printf 'LOCAL_STATUS_SNAPSHOT\n' >> "$OUT"
printf 'generated_at=%s\n' "$(date -Is)" >> "$OUT"
printf 'host=%s\n' "$(hostname)" >> "$OUT"
printf 'endpoint=%s\n' "$endpoint" >> "$OUT"
printf 'model=%s\n' "$model" >> "$OUT"

load="$(cut -d' ' -f1-3 /proc/loadavg 2>/dev/null || echo unknown)"
add_check "system_load" "ok" "load averages are $load"

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

if command -v curl >/dev/null 2>&1 && curl -fsS "$endpoint/health" >/dev/null 2>&1; then
  add_check "proxy_health" "ok" "OpenAI compatibility proxy health endpoint responded"
else
  add_check "proxy_health" "action" "OpenAI compatibility proxy health endpoint did not respond"
fi

if command -v curl >/dev/null 2>&1 && curl -fsS "http://127.0.0.1:8000/api/tags" >/dev/null 2>&1; then
  add_check "hailo_tags" "ok" "Hailo-Ollama model list endpoint responded"
else
  add_check "hailo_tags" "action" "Hailo-Ollama model list endpoint did not respond"
fi

if command -v systemctl >/dev/null 2>&1 && systemctl --user list-unit-files hailo-ollama-serve.service >/dev/null 2>&1; then
  if systemctl --user is-active --quiet hailo-ollama-serve.service; then
    add_check "hailo_ollama_service" "ok" "hailo-ollama-serve user service is active"
  else
    svc_state="$(systemctl --user is-active hailo-ollama-serve.service 2>/dev/null || true)"
    add_check "hailo_ollama_service" "action" "hailo-ollama-serve user service is ${svc_state:-not-active}"
  fi
else
  add_check "hailo_ollama_service" "watch" "hailo-ollama-serve user service is not installed or user systemd is unavailable"
fi

if [ -d "$LEATHER_ROOT/.git" ]; then
  changed="$(git -C "$LEATHER_ROOT" status --short 2>/dev/null | wc -l | tr -d ' ')"
  if [ "$changed" -gt 0 ]; then
    add_check "leather_git_status" "watch" "Leather Git checkout has $changed changed or untracked paths"
  else
    add_check "leather_git_status" "ok" "Leather Git checkout is clean"
  fi
else
  add_check "leather_git_status" "watch" "Leather source directory is not a Git checkout on this device"
fi

state_entries="$(find "$ROOT/.state" -type f 2>/dev/null | wc -l | tr -d ' ')"
add_check "example_state_files" "ok" "example state currently has $state_entries files"

status="$(overall_status)"
notify=false
[ "$status" = "action" ] && notify=true

{
  printf 'DETERMINISTIC_STATUS=%s\n' "$status"
  printf 'SHOULD_NOTIFY=%s\n' "$notify"
  printf 'NOTIFY_RULE=notify only on action status; watch is recorded but quiet\n'
} >> "$OUT"

cat "$OUT"

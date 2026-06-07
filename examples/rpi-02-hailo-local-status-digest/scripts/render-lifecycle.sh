#!/usr/bin/env bash
set -u

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
FACTS="$ROOT/.snapshot/facts.env"
OUT="$ROOT/agents/local-status.lifecycle.yaml"

if [ ! -f "$FACTS" ]; then
  echo "missing $FACTS; run make snapshot first" >&2
  exit 1
fi

deterministic_status="$(awk -F= '$1=="DETERMINISTIC_STATUS"{print substr($0, index($0,"=")+1)}' "$FACTS")"
should_notify="$(awk -F= '$1=="SHOULD_NOTIFY"{print substr($0, index($0,"=")+1)}' "$FACTS")"
checks="$(awk -F= '$1=="CHECKS"{print substr($0, index($0,"=")+1)}' "$FACTS")"
model="$(awk -F= '$1=="MODEL"{print substr($0, index($0,"=")+1)}' "$FACTS")"
[ -z "$model" ] && model="qwen3:1.7b"

mkdir -p "$ROOT/agents"
{
  printf 'agent: local-status\n'
  printf 'max_tokens: 2048\n'
  printf 'instances:\n'
  printf '  - name: local-status-digest-30s\n'
  printf '    schedule: "*/30 * * * * *"\n'
  printf '    model: %s\n' "$model"
  printf '    prompt: |\n'
  printf '      Create a local operational status digest from this evidence only.\n'
  printf '      DETERMINISTIC_STATUS=%s\n' "$deterministic_status"
  printf '      SHOULD_NOTIFY=%s\n' "$should_notify"
  printf '      %s\n' "$checks"
} > "$OUT"

echo "wrote $OUT"

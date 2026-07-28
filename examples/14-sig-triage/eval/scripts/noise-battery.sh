#!/usr/bin/env bash
# Noise battery — JOB #1 from the handoff: the empirical draw-noise floor.
#
#   bash eval/scripts/noise-battery.sh <35b|4b>
#
# Two families of repeats, nothing varied inside a family:
#   H-2..H-6   frozen-cache repeats of arm H (rules + forced fetch, the committed
#              match prompt) against the rig's frozen analyze cache. Same battery
#              config as the archived <rig>-H, so the archived cell is a sixth
#              draw for free. Isolates MATCH-stage draw noise; AUC-per-repeat
#              comes off the archived logprobs afterwards.
#   A-3..A-7   fresh-analyze repeats of arm A (full pipeline, no cache).
#              Decomposes match-stage vs full-pipeline variance against the
#              frozen family. (A-2 already exists but is a frozen-cache draw —
#              its manifest records the 35b-A cache — so fresh repeats start
#              at A-3 and the archived <rig>-A is the family's sixth draw.)
#
# Skips any tag whose archive is already complete (>=225 answered rows), so the
# battery is resumable. Per-rig lock, same discipline as run-battery.sh.
set -u
RIG="${1:?usage: noise-battery.sh <35b|4b>}"
EX="$(cd "$(dirname "$0")/../.." && pwd)"
TMP="${BATTERY_TMP:-${EX}/eval/.battery}"; mkdir -p "$TMP"
cd "$EX"

LOCK="$TMP/.lock-$RIG"
if ! mkdir "$LOCK" 2>/dev/null; then
  echo "a battery is already running on $RIG (lock: $LOCK) -- remove it if stale" >&2; exit 2
fi
trap 'rmdir "$LOCK" 2>/dev/null' EXIT

case "$RIG" in
  35b) export LEATHER_LLM_ENDPOINT=http://127.0.0.1:8000
       export LEATHER_MODEL=qwen36-35b-a3b-nvfp4
       export CONCURRENCY=8; export LP_PORT=8011 ;;
  4b)  export LEATHER_LLM_ENDPOINT=http://10.0.0.64:8000
       export LEATHER_MODEL=/home/tyler/llm/models/Qwen3-4B-Instruct-2507-AWQ
       export CONCURRENCY=4; export LP_PORT=8021 ;;
  *)   echo "unknown rig $RIG" >&2; exit 2 ;;
esac
export LEATHER=../../leather SHELLMCP=../../shell-mcp
export STATE_SUFFIX="-$RIG" LOGPROB=1

CACHE="eval/results/runs/${RIG}-A/analyze-notes.jsonl"
[ "$(grep -c . "$CACHE" 2>/dev/null || echo 0)" = 250 ] || {
  echo "no complete analyze cache for $RIG" >&2; exit 2; }

AGENTS="$TMP/agents-noise-$RIG"; rm -rf "$AGENTS"; command cp -rf agents "$AGENTS"
export LEATHER_AGENT_DIR="$AGENTS"

complete() {
  [ -f "eval/results/runs/$1/predictions.jsonl" ] || return 1
  python3 -c "
import json,sys
rows=[json.loads(l) for l in open('eval/results/runs/$1/predictions.jsonl') if l.strip()]
ok=sum(1 for r in rows if not (r.get('predicted')=='unknown' and r.get('confidence')=='no-output'))
sys.exit(0 if len(rows)==250 and ok >= 225 else 1)" 2>/dev/null
}

run_one() { # tag force cache
  local tag="$1" force="$2" cache="$3"
  if complete "$tag"; then echo "  $tag already complete — skipping"; return 0; fi
  # Both families run the COMMITTED match prompt: agents/match.agent.md, untouched.
  command cp -f agents/match.agent.md "$AGENTS/match.agent.md"
  unset SIG_INDEX CURING_DIR
  if [ -n "$cache" ]; then export ANALYZE_CACHE="$cache"; else unset ANALYZE_CACHE; fi
  RUN_TAG="$tag" FORCE_TOOL="$force" \
    bash eval/run-eval.sh > "$TMP/noise-$tag.log" 2>&1
  printf '%-10s acc=%-7s\n' "$tag" \
    "$(grep -oE 'overall accuracy[^:]*: [0-9.]+%' "$TMP/noise-$tag.log" | grep -oE '[0-9.]+%')"
  bash eval/scripts/verify-run.sh --archive "eval/results/runs/$tag" 2>&1 |
    grep -E '^\s+\[(FAIL|SKIP)\]' | sed 's/^/           /'
}

for i in 2 3 4 5 6; do echo "=== $RIG / H-$i (frozen cache) ==="; run_one "${RIG}-H-$i" 1 "$CACHE"; done
for i in 3 4 5 6 7; do echo "=== $RIG / A-$i (fresh analyze) ==="; run_one "${RIG}-A-$i" 0 ""; done
echo "DONE-NOISE $RIG"

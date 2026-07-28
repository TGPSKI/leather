#!/usr/bin/env bash
# Overnight curiosity battery — OPTIONAL. No archived cell is invalidated by the
# runner fix (scanned: zero scope refusals outside the quarantined wreck), so
# nothing here is unfinished business. What it buys, per rig:
#
#   4b:  T2c-2   T2c ORIGINAL wording (match.T2c0.agent.md) under the FIXED
#                runner — does the recoverable refusal alone rescue the config
#                that dead-lettered 214/250? Pairs vs 4b-T2c as PROMPT-DIFF
#                (the 4B wording delta, for free).
#        T2cr    record-without-clear — the review-requested control isolating
#                the carrier from the clearing. Pairs vs T2 (carrier cost) and
#                reads against T2c (clearing itself).
#        F2-2    replication of the unresolved stage-split cell (force=0).
#        + noise-battery.sh 4b  (H-2..6 frozen, A-3..7 fresh — the 4B noise floor)
#
#   35b: T2cr    same control at reference scale.
#        F2-2    stage-split replication (force=1, as the original 35b-F2 ran).
#        T2c-2-2 second hardened-wording draw — pairs vs T2c-2 as a true REPEAT,
#                firming the p=0.073 wording effect one way or the other.
#
# Usage:  bash eval/scripts/overnight-battery.sh <35b|4b|both>
# Gates on preflight per rig. Skips any tag whose archive is complete (>=225
# answered rows). Uses the per-rig mkdir lock, so it will refuse to fight a
# battery already running on the same rig.
set -u
WHICH="${1:-both}"
EX="$(cd "$(dirname "$0")/../.." && pwd)"
TMP="${BATTERY_TMP:-${EX}/eval/.battery}"; mkdir -p "$TMP"
cd "$EX"

complete() {
  [ -f "eval/results/runs/$1/predictions.jsonl" ] || return 1
  python3 -c "
import json,sys
rows=[json.loads(l) for l in open('eval/results/runs/$1/predictions.jsonl') if l.strip()]
ok=sum(1 for r in rows if not (r.get('predicted')=='unknown' and r.get('confidence')=='no-output'))
sys.exit(0 if len(rows)==250 and ok >= 225 else 1)" 2>/dev/null
}

run_rig() { # rig
  local RIG="$1"
  case "$RIG" in
    35b) export LEATHER_LLM_ENDPOINT=http://127.0.0.1:8000
         export LEATHER_MODEL=qwen36-35b-a3b-nvfp4
         export CONCURRENCY=8 LP_PORT=8011 ;;
    4b)  export LEATHER_LLM_ENDPOINT=http://10.0.0.64:8000
         export LEATHER_MODEL=/home/tyler/llm/models/Qwen3-4B-Instruct-2507-AWQ
         export CONCURRENCY=4 LP_PORT=8021 ;;
  esac
  export LEATHER=../../leather SHELLMCP=../../shell-mcp
  export STATE_SUFFIX="-$RIG" LOGPROB=1

  PRIMARY_ENDPOINT="$LEATHER_LLM_ENDPOINT" PRIMARY_MODEL="$LEATHER_MODEL" \
    bash eval/scripts/preflight.sh > "$TMP/overnight-preflight-$RIG.log" 2>&1 || {
      echo "$RIG: PREFLIGHT RED — refusing to spend the night (see $TMP/overnight-preflight-$RIG.log)" >&2
      return 2; }
  echo "$RIG: preflight green"

  local LOCK="$TMP/.lock-$RIG"
  if ! mkdir "$LOCK" 2>/dev/null; then
    echo "$RIG: a battery is already running (lock: $LOCK)" >&2; return 2
  fi
  trap 'rmdir "'"$LOCK"'" 2>/dev/null' RETURN

  local CACHE="eval/results/runs/${RIG}-A/analyze-notes.jsonl"
  local AGENTS="$TMP/agents-overnight-$RIG"
  rm -rf "$AGENTS"; command cp -rf agents "$AGENTS"
  export LEATHER_AGENT_DIR="$AGENTS"

  run_cell() { # tag file force idx curings cache
    local tag="$1" file="$2" force="$3" idx="$4" curings="$5" cache="$6"
    if complete "$tag"; then echo "  $tag already complete — skipping"; return 0; fi
    command cp -f "$file" "$AGENTS/match.agent.md"
    if [ -n "$idx" ]; then export SIG_INDEX="$idx"; else unset SIG_INDEX; fi
    if [ -n "$curings" ]; then export CURING_DIR="$curings"; else unset CURING_DIR; fi
    if [ -n "$cache" ]; then export ANALYZE_CACHE="$cache"; else unset ANALYZE_CACHE; fi
    RUN_TAG="$tag" FORCE_TOOL="$force" \
      bash eval/run-eval.sh > "$TMP/overnight-$tag.log" 2>&1
    printf '%-12s acc=%-7s\n' "$tag" \
      "$(grep -oE 'overall accuracy[^:]*: [0-9.]+%' "$TMP/overnight-$tag.log" | grep -oE '[0-9.]+%')"
    bash eval/scripts/verify-run.sh --archive "eval/results/runs/$tag" 2>&1 |
      grep -E '^\s+\[(FAIL|SKIP)\]' | sed 's/^/             /'
  }

  case "$RIG" in
    4b)
      echo "=== 4b / T2c-2 (original wording, fixed runner) ==="
      run_cell 4b-T2c-2 eval/ablation/match.T2c0.agent.md 0 "" "" "$CACHE"
      echo "=== 4b / T2cr (record, no clear) ==="
      run_cell 4b-T2cr  eval/ablation/match.T2cr.agent.md 0 "" "" "$CACHE"
      echo "=== 4b / F2-2 (stage-split replication, unforced) ==="
      run_cell 4b-F2-2  eval/ablation/match.F2.agent.md   0 sigs.index.seeded.tsv curings-oneshot ""
      ;;
    35b)
      echo "=== 35b / T2cr (record, no clear) ==="
      run_cell 35b-T2cr   eval/ablation/match.T2cr.agent.md 0 "" "" "$CACHE"
      echo "=== 35b / F2-2 (stage-split replication, forced as original) ==="
      run_cell 35b-F2-2   eval/ablation/match.F2.agent.md   1 sigs.index.seeded.tsv curings-oneshot ""
      echo "=== 35b / T2c-2-2 (hardened-wording repeat) ==="
      run_cell 35b-T2c-2-2 eval/ablation/match.T2c.agent.md 0 "" "" "$CACHE"
      ;;
  esac
  rmdir "$LOCK" 2>/dev/null; trap - RETURN
  if [ "$RIG" = 4b ]; then
    echo "=== 4b / noise family (frozen H x5, fresh A x5) ==="
    bash eval/scripts/noise-battery.sh 4b
  fi
  echo "DONE-OVERNIGHT $RIG"
}

case "$WHICH" in
  both) run_rig 35b & P35=$!; run_rig 4b & P4=$!
        wait "$P35"; wait "$P4" ;;
  35b|4b) run_rig "$WHICH" ;;
  *) echo "usage: overnight-battery.sh <35b|4b|both>" >&2; exit 2 ;;
esac
echo "overnight complete — read verdicts with:"
echo "  python3 eval/scripts/paired-verdicts.py"

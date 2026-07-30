#!/usr/bin/env bash
# Confirmatory battery — executes the T1 registration EXACTLY.
#
#   Registration of record: eval/ablation/preregistration.md, frozen at main
#   commit 96cc418 (2026-07-29). Signed: 3 replications per arm-side (5x
#   bump only on the pre-declared 1-point boundary trigger); Holm across the
#   six primary contrasts. This script runs the 11 registered arms x 3
#   replications on the 4B rig, tags 4b-<ARM>-c1..c3, and NEVER touches the
#   exploratory archives.
#
#   Wave-ordered (all arms at c1, then c2, then c3) so an interrupted battery
#   still leaves every contrast with an equal number of fresh draws.
#   Resumable: complete archives (>=225 answered rows) are skipped.
#
#   Usage:  bash eval/scripts/confirmatory-battery.sh 4b
#
# Arm configs are copied verbatim from run-battery.sh / overnight-battery.sh
# and cross-checked against the exploratory archives' run-manifest.json.
set -u
RIG="${1:?usage: confirmatory-battery.sh 4b}"
[ "$RIG" = 4b ] || { echo "registration covers the 4b rig only" >&2; exit 2; }

EX="$(cd "$(dirname "$0")/../.." && pwd)"
TMP="${BATTERY_TMP:-${EX}/eval/.battery}"; mkdir -p "$TMP"
cd "$EX"

LOCK="$TMP/.lock-$RIG"
if ! mkdir "$LOCK" 2>/dev/null; then
  echo "a battery is already running on $RIG (lock: $LOCK) -- remove it if stale" >&2; exit 2
fi
trap 'rmdir "$LOCK" 2>/dev/null' EXIT INT TERM

export LEATHER_LLM_ENDPOINT=http://10.0.0.64:8000
export LEATHER_MODEL=/home/tyler/llm/models/Qwen3-4B-Instruct-2507-AWQ
export CONCURRENCY=4 LP_PORT=8021
export LEATHER=../../leather SHELLMCP=../../shell-mcp
export STATE_SUFFIX="-$RIG" LOGPROB=1

echo "confirmatory battery -- registration 96cc418 -- $(date -Is)"

CACHE="eval/results/runs/${RIG}-A/analyze-notes.jsonl"
[ "$(grep -c . "$CACHE" 2>/dev/null || echo 0)" = 250 ] || {
  echo "no complete analyze cache for $RIG" >&2; exit 2; }

AGENTS="$TMP/agents-conf-$RIG"; rm -rf "$AGENTS"; command cp -rf agents "$AGENTS"
export LEATHER_AGENT_DIR="$AGENTS"

# P1/P2 splice the catalog into the user turn from THIS rig's analyze notes
# (same build as run-battery.sh).
build_pcache() {
  python3 - "$1" "$CACHE" "$RIG" <<'PY'
import json, sys, os
tag, base, rig = sys.argv[1], sys.argv[2], sys.argv[3]
cat = open('sigs.reference.yaml').read().rstrip()
block = ("The SIG feature catalog follows. Match the issue's signals against it.\n\n"
         "```yaml\n" + cat + "\n```")
os.makedirs("eval/.caches", exist_ok=True)
out = f"eval/.caches/analyze-cache-{tag}-{rig}.jsonl"
with open(out, "w") as f:
    for l in open(base):
        r = json.loads(l)
        r["note"] = (block + "\n\n" + r["note"]) if tag == "P1" else (r["note"] + "\n\n" + block)
        f.write(json.dumps(r) + "\n")
print(out)
PY
}
PCACHE1="$(build_pcache P1 | tail -1)"
PCACHE2="$(build_pcache P2 | tail -1)"

complete() {
  [ -f "eval/results/runs/$1/predictions.jsonl" ] || return 1
  python3 -c "
import json,sys
rows=[json.loads(l) for l in open('eval/results/runs/$1/predictions.jsonl') if l.strip()]
ok=sum(1 for r in rows if not (r.get('predicted')=='unknown' and r.get('confidence')=='no-output'))
sys.exit(0 if len(rows)==250 and ok >= 225 else 1)" 2>/dev/null
}

run_cell() { # tag file force idx curings cache
  local tag="$1" file="$2" force="$3" idx="$4" curings="$5" cache="$6"
  if complete "$tag"; then echo "  $tag already complete -- skipping"; return 0; fi
  command cp -f "$file" "$AGENTS/match.agent.md"
  [ "$(sha256sum "$file" | cut -d' ' -f1)" = "$(sha256sum "$AGENTS/match.agent.md" | cut -d' ' -f1)" ] \
    || { echo "ABORT $tag: agent copy did not take" >&2; return 1; }
  if [ -n "$idx" ]; then export SIG_INDEX="$idx"; else unset SIG_INDEX; fi
  if [ -n "$curings" ]; then export CURING_DIR="$curings"; else unset CURING_DIR; fi
  if [ -n "$cache" ]; then export ANALYZE_CACHE="$cache"; else unset ANALYZE_CACHE; fi
  RUN_TAG="$tag" FORCE_TOOL="$force" \
    bash eval/run-eval.sh > "$TMP/conf-$tag.log" 2>&1
  printf '%-12s acc=%-7s\n' "$tag" \
    "$(grep -oE 'overall accuracy[^:]*: [0-9.]+%' "$TMP/conf-$tag.log" | grep -oE '[0-9.]+%')"
  bash eval/scripts/verify-run.sh --archive "eval/results/runs/$tag" 2>&1 |
    grep -E '^\s+\[(FAIL|SKIP)\]' | sed 's/^/             /'
}

# arm | file | force | idx | curings | cache   (registered arms only)
run_arm() { # arm rep
  local a="$1" tag="${RIG}-$1-c$2"
  case "$a" in
    A0)   run_cell "$tag" eval/ablation/match.A0.agent.md   0 ""                    ""             "$CACHE" ;;
    B)    run_cell "$tag" eval/ablation/match.B.agent.md    0 ""                    ""             "$CACHE" ;;
    G)    run_cell "$tag" eval/ablation/match.G.agent.md    1 sigs.index.seeded.tsv ""             "$CACHE" ;;
    E2)   run_cell "$tag" eval/ablation/match.E2.agent.md   1 sigs.index.seeded.tsv ""             "$CACHE" ;;
    P1)   run_cell "$tag" eval/ablation/match.B.agent.md    0 ""                    curings-nopage "$PCACHE1" ;;
    P2)   run_cell "$tag" eval/ablation/match.B.agent.md    0 ""                    curings-nopage "$PCACHE2" ;;
    T2)   run_cell "$tag" eval/ablation/match.T2.agent.md   0 ""                    ""             "$CACHE" ;;
    T3)   run_cell "$tag" eval/ablation/match.T3.agent.md   0 ""                    ""             "$CACHE" ;;
    S1)   run_cell "$tag" eval/ablation/match.S1.agent.md   0 ""                    curings-s1     "$CACHE" ;;
    T2c)  run_cell "$tag" eval/ablation/match.T2c.agent.md  0 ""                    ""             "$CACHE" ;;
    T2cr) run_cell "$tag" eval/ablation/match.T2cr.agent.md 0 ""                    ""             "$CACHE" ;;
    *) echo "unregistered arm $a" >&2; return 1 ;;
  esac
}

# Defaults ARE the registration: 11 registered arms x 3 replications. The two
# overrides exist to execute Amendment 1 DECISION 4 (signed 2026-07-30) — the
# boundary trigger fired on contrast #2, bumping G and E2 to 5 draws each:
#   BUMP_ARMS="G E2" BUMP_REPS="4 5" bash eval/scripts/confirmatory-battery.sh 4b
# They change nothing when unset; every cell still runs the same run_cell path,
# completeness skip, agent-sha check, and verify-run pass.
ARMS="${BUMP_ARMS:-A0 B G E2 P1 P2 T2 T3 S1 T2c T2cr}"
REPS="${BUMP_REPS:-1 2 3}"
for rep in $REPS; do
  echo "=== WAVE c$rep ($(date -Is)) ==="
  for a in $ARMS; do
    echo "--- $RIG/$a c$rep ---"
    run_arm "$a" "$rep"
  done
done
echo "DONE-CONFIRMATORY $RIG $(date -Is)"

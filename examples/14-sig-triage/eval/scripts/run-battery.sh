#!/usr/bin/env bash
# Final battery — every remaining cell, per rig, in one runner.
#
#   bash battery-final.sh <35b|4b>
#
# Cells (skipped automatically when an archive with 250 rows already exists):
#   A0  rules, no tool offered                      4B only (35B done: 84.8)
#   D   catalog force-fetched                       4B only (35B done: 81.2; 4B killed mid-run)
#   Dn  forced fetch, no fetch instruction          4B only (35B done: 78.0)
#   H   rules + forced fetch                        4B only (35B done: 86.0)
#   T2  fetch turn -> decide turn                   BOTH — leather's per-turn tool scope
#   T3  survey -> retrieve -> decide                BOTH
#   G   narrowed lookup returning catalog entries   BOTH — the LEP-0009 fix
#   P1  catalog in user turn, BEFORE the issue      BOTH — non-paginating curing
#   P2  catalog in user turn, AFTER the issue       BOTH — non-paginating curing
#   F   ONE stage, everything stuffed                BOTH — the only cell that removes a
#                                                    stage, so the only test of the split
#
# P1/P2 use eval/curings-nopage (page_size_bytes 32000). Their 7215-byte payload exceeds the
# 6000 default, which silently switched them into reflection mode last time and made them
# measure a third delivery mechanism, badly.
set -u
RIG="${1:?usage: battery-final.sh <35b|4b>}"
EX="$(cd "$(dirname "$0")/../.." && pwd)"
TMP="${BATTERY_TMP:-${EX}/eval/.battery}"; mkdir -p "$TMP"
cd "$EX"

# A per-rig LOCK, not a pgrep guard. Matching on command lines is unreliable here:
# `pgrep -f "battery-final.sh 35b"` matches this process AND any shell whose command
# string happens to contain the same text (the launching tool wrapper did), so the
# guard refused to start the run it exists to protect. mkdir is atomic; the trap
# releases it on any exit path.
LOCK="$TMP/.lock-$RIG"
if ! mkdir "$LOCK" 2>/dev/null; then
  echo "a battery is already running on $RIG (lock: $LOCK) -- remove it if stale" >&2; exit 2
fi
trap 'rmdir "$LOCK" 2>/dev/null' EXIT

case "$RIG" in
  35b) export LEATHER_LLM_ENDPOINT=http://127.0.0.1:8000
       export LEATHER_MODEL=qwen36-35b-a3b-nvfp4
       export CONCURRENCY=8; export LP_PORT=8011
       CELLS="F2 S1" ;;
  4b)  export LEATHER_LLM_ENDPOINT=http://10.0.0.64:8000
       export LEATHER_MODEL=/home/tyler/llm/models/Qwen3-4B-Instruct-2507-AWQ
       export CONCURRENCY=4; export LP_PORT=8021
       CELLS="A0 D Dn H T2 T3 G P1 P2 F" ;;
  *)   echo "unknown rig $RIG" >&2; exit 2 ;;
esac
export LEATHER=../../leather SHELLMCP=../../shell-mcp
export STATE_SUFFIX="-$RIG" LOGPROB=1

CACHE="eval/results/runs/${RIG}-A/analyze-notes.jsonl"
[ "$(grep -c . "$CACHE" 2>/dev/null || echo 0)" = 250 ] || {
  echo "no complete analyze cache for $RIG" >&2; exit 2; }

AGENTS="$TMP/agents-fin-$RIG"; rm -rf "$AGENTS"; command cp -rf agents "$AGENTS"
export LEATHER_AGENT_DIR="$AGENTS"

# P1/P2 replay a cache with the catalog spliced into the user turn, built from THIS rig's own
# analyze notes so the arm compares one rig's match prompt, not two rigs' analyze stages.
build_pcache() {
  python3 - "$1" "$CACHE" "$RIG" <<'PY'
import json, sys
tag, base, rig = sys.argv[1], sys.argv[2], sys.argv[3]
cat = open('sigs.reference.yaml').read().rstrip()
block = ("The SIG feature catalog follows. Match the issue's signals against it.\n\n"
         "```yaml\n" + cat + "\n```")
out = f"eval/.caches/analyze-cache-{tag}-{rig}.jsonl"
with open(out, "w") as f:
    for l in open(base):
        r = json.loads(l)
        r["note"] = (block + "\n\n" + r["note"]) if tag == "P1" else (r["note"] + "\n\n" + block)
        f.write(json.dumps(r) + "\n")
print(out)
PY
}

run_cell() {
  local v="$1" tag="${RIG}-$1"
  # Row count is NOT completion: run-eval writes a row per corpus issue whether or not
  # the model answered, so a run that failed on every issue still archives 250 rows. A
  # stale-proxy failure produced exactly that, and the skip let the wreckage stand as a
  # finished cell. Require that most rows actually carry an answer.
  if [ -f "eval/results/runs/$tag/predictions.jsonl" ]; then
    live=$(python3 -c "
import json,sys
rows=[json.loads(l) for l in open('eval/results/runs/$tag/predictions.jsonl') if l.strip()]
ok=sum(1 for r in rows if not (r.get('predicted')=='unknown' and r.get('confidence')=='no-output'))
print(1 if len(rows)==250 and ok >= 225 else 0)" 2>/dev/null)
    if [ "$live" = 1 ]; then echo "  $tag already complete — skipping"; return 0; fi
    echo "  $tag archive exists but is incomplete — re-running"
  fi
  local file force idx cache curings
  force=0; idx=""; cache="$CACHE"; curings=""
  case "$v" in
    A0)     file=eval/ablation/match.A0.agent.md ;;
    D)      file=eval/ablation/match.D.agent.md;  force=1 ;;
    Dn)     file=eval/ablation/match.Dn.agent.md; force=1 ;;
    H)      file=agents/match.agent.md;           force=1 ;;
    T2)     file=eval/ablation/match.T2.agent.md ;;
    T3)     file=eval/ablation/match.T3.agent.md ;;
    G)      file=eval/ablation/match.G.agent.md;  force=1; idx=sigs.index.seeded.tsv ;;
    G2)     file=eval/ablation/match.G2.agent.md; force=1; idx=sigs.index.seeded.tsv ;;
    P1|P2)  file=eval/ablation/match.B.agent.md;  cache="$(build_pcache "$v" | tail -1)"
            curings=curings-nopage ;;
    # F is the ONLY cell that removes a stage: no analyze, raw issue straight into a
    # single agent. So it runs the FULL pipeline (cache="") against a one-curing set.
    F)      file=eval/ablation/match.F.agent.md;  cache=""; curings=curings-oneshot ;;
    # F2 is the FAIR one-stage arm: F pastes the catalog into the system prompt, which is
    # the worst delivery measured (-3.6 vs a user turn, -7.6 vs a fetch), so F vs A
    # handicapped the one-stage side. F2 uses the enforced v3 lookup instead, so F2 vs G2
    # isolates the stage split with catalog handling held constant AND correct.
    F2)     file=eval/ablation/match.F2.agent.md; cache=""; curings=curings-oneshot
            force=1; idx=sigs.index.seeded.tsv ;;
    # S1: bounded context via a STAGE boundary rather than a turn boundary. The shortlist
    # curing narrows with no catalog; a FRESH SESSION then decides from the shortlist alone,
    # fetching only its candidates' entries. Contrast with T3, where context grew 1206 ->
    # 2828 tok across turns because Session.Reset is unwired.
    S1)     file=eval/ablation/match.S1.agent.md; curings=curings-s1 ;;
    *) echo "unknown cell $v" >&2; return 1 ;;
  esac
  command cp -f "$file" "$AGENTS/match.agent.md"
  [ "$(sha256sum "$file" | cut -d' ' -f1)" = "$(sha256sum "$AGENTS/match.agent.md" | cut -d' ' -f1)" ] \
    || { echo "ABORT $tag: agent copy did not take" >&2; return 1; }
  if [ -n "$idx" ]; then export SIG_INDEX="$idx"; else unset SIG_INDEX; fi
  if [ -n "$curings" ]; then export CURING_DIR="$curings"; else unset CURING_DIR; fi
  if [ -n "$cache" ]; then export ANALYZE_CACHE="$cache"; else unset ANALYZE_CACHE; fi
  RUN_TAG="$tag" FORCE_TOOL="$force" \
    bash eval/run-eval.sh > "$TMP/fin-$tag.log" 2>&1
  printf '%-10s acc=%-7s %s\n' "$tag" \
    "$(grep -oE 'overall accuracy[^:]*: [0-9.]+%' "$TMP/fin-$tag.log" | grep -oE '[0-9.]+%')" \
    "$(grep -oE 'offered on [0-9]+/[0-9]+, actually called [0-9]+; mean match rounds/issue [0-9.]+' "$TMP/fin-$tag.log" | tail -1)"
  bash eval/scripts/verify-run.sh --archive "eval/results/runs/$tag" 2>&1 |
    grep -E '^\s+\[(FAIL|SKIP)\]' | sed 's/^/           /'
}

for v in $CELLS; do echo "=== $RIG / $v ==="; run_cell "$v"; done
echo "DONE-FINAL $RIG"

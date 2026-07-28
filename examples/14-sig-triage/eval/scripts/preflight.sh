#!/usr/bin/env bash
# preflight.sh — exercise every path the eval harness uses, on 5 issues, before
# committing hours of GPU time to it.
#
# This exists because the harness accumulated a run of bugs that were each
# invisible in the output and only surfaced after a long run had already been
# spent: a proxy that misfiled every match call under the wrong stage, two rigs
# sharing one queue store, a tool schema emitting `required: null`, archiving
# that never ran when a cell was killed, a manifest that could not identify its
# own input. Every one was "the script sets X, therefore X happens" -- the same
# assumption the eval itself is not allowed to make about a model.
#
# So the rule is symmetrical: the harness proves it works before it is trusted.
#
#   PRIMARY_ENDPOINT=http://127.0.0.1:8000 PRIMARY_MODEL=my-model \
#     bash eval/scripts/preflight.sh
#
#   # add a second rig to also verify parallel isolation:
#   SECONDARY_ENDPOINT=http://10.0.0.64:8000 SECONDARY_MODEL=other-model \
#     bash eval/scripts/preflight.sh
#
# Exit 0 only when every check passes.
set -uo pipefail

EVAL_DIR="$(cd "$(dirname "$0")/.." && pwd)"
EX_DIR="$(cd "${EVAL_DIR}/.." && pwd)"
cd "$EX_DIR"

PRIMARY_ENDPOINT="${PRIMARY_ENDPOINT:-${LEATHER_LLM_ENDPOINT:-http://127.0.0.1:8000}}"
PRIMARY_MODEL="${PRIMARY_MODEL:-${LEATHER_MODEL:-}}"
[ -n "$PRIMARY_MODEL" ] || { echo "set PRIMARY_MODEL (or LEATHER_MODEL)" >&2; exit 2; }

WORK="${EVAL_DIR}/.preflight"
rm -rf "$WORK"; mkdir -p "$WORK"
head -5 eval/corpus.jsonl > "$WORK/corpus5.jsonl"
rm -rf "$WORK/agents"; cp -r agents "$WORK/agents"
trap 'rm -rf "$WORK" "${EVAL_DIR}"/.state-eval-pf* "${EVAL_DIR}"/results/runs/pf-*' EXIT

export LEATHER="${LEATHER:-${EX_DIR}/../../leather}"
export LEATHER_LLM_ENDPOINT="$PRIMARY_ENDPOINT" LEATHER_MODEL="$PRIMARY_MODEL"
export LEATHER_AGENT_DIR="$WORK/agents" CORPUS="$WORK/corpus5.jsonl"

fails=0
ck() { if [ "$2" = 1 ]; then printf '  [ok]   %s\n' "$1"
       else printf '  [FAIL] %s\n' "$1"; fails=$((fails+1)); fi; }
mf() { python3 -c "import json,sys;print(json.load(open(sys.argv[1])).get(sys.argv[2],''))" "$1" "$2" 2>/dev/null; }

cell() { # tag agentfile force cache index suffix port
  cp "$2" "$WORK/agents/match.agent.md"
  if [ -n "$5" ]; then export SIG_INDEX="$5"; else unset SIG_INDEX; fi
  if [ -n "$4" ]; then export ANALYZE_CACHE="$4"; else unset ANALYZE_CACHE; fi
  RUN_TAG="$1" FORCE_TOOL="$3" STATE_SUFFIX="$6" LP_PORT="$7" CONCURRENCY=4 LOGPROB=1 \
    bash eval/run-eval.sh > "$WORK/$1.log" 2>&1
}

echo "=== 1. full pipeline: provenance and archiving ==="
cell pf-A agents/match.agent.md 0 "" "" -pf 8061
A="${EVAL_DIR}/results/runs/pf-A"
[ -f "$A/run-manifest.json" ] && ck "manifest written" 1 || ck "manifest written" 0
[ "$(mf "$A/run-manifest.json" model)" = "$PRIMARY_MODEL" ] && ck "manifest records the model actually used" 1 || ck "manifest records the model" 0
[ "$(mf "$A/run-manifest.json" agent_sha)" != none ] && ck "manifest records the agent prompt sha" 1 || ck "manifest records agent sha" 0
[ "$(mf "$A/run-manifest.json" corpus_sha)" != none ] && ck "manifest records the corpus sha" 1 || ck "manifest records corpus sha" 0
n=$(grep -c . "$A/analyze-notes.jsonl" 2>/dev/null); [ "${n:-0}" = 5 ] && ck "analyze notes archived" 1 || ck "analyze notes archived (got ${n:-0})" 0
python3 -c "
import json,sys
r=[json.loads(l) for l in open('$A/analyze-notes.jsonl')]
sys.exit(0 if r and all(len(x.get('note',''))>50 for x in r) else 1)" \
  && ck "notes carry full text, so they are replayable" 1 || ck "notes carry full text" 0
[ -f "$A/logprobs.jsonl.gz" ] && ck "logprobs archived" 1 || ck "logprobs archived" 0
[ -f "$A/run-evidence.log.gz" ] && ck "evidence log archived" 1 || ck "evidence log archived" 0
bash eval/scripts/verify-run.sh -pf > "$WORK/verify-A.txt" 2>&1
grep -q "VERIFY PASSED" "$WORK/verify-A.txt" && ck "verify-run passes" 1 || { ck "verify-run passes" 0; sed 's/^/         /' "$WORK/verify-A.txt"; }
grep -q "temperature 0 on every recorded request" "$WORK/verify-A.txt" \
  && ck "temperature 0 confirmed from request bodies, not from config" 1 || ck "temperature 0 confirmed" 0

echo "=== 2. match-only replay ==="
cell pf-B eval/ablation/match.B.agent.md 0 "$A/analyze-notes.jsonl" "" -pf 8061
B="${EVAL_DIR}/results/runs/pf-B"
[ "$(mf "$B/run-manifest.json" analyze_cache)" != none ] && ck "manifest records which analyze cache fed it" 1 || ck "manifest records the analyze cache" 0
[ "$(mf "$B/run-manifest.json" analyze_cache_sha)" != none ] && ck "manifest records the cache sha" 1 || ck "manifest records cache sha" 0
a=$(ls "${EVAL_DIR}/.state-eval-pf/artifacts/analyze" 2>/dev/null | wc -l)
m=$(ls "${EVAL_DIR}/.state-eval-pf/artifacts/match" 2>/dev/null | wc -l)
[ "$a" = 0 ] && ck "analyze stage genuinely skipped" 1 || ck "analyze skipped (got $a artifacts)" 0
[ "$m" = 5 ] && ck "match ran on every row" 1 || ck "match ran on every row (got $m)" 0
grep -q "attributed 5/5" "$WORK/pf-B.log" && ck "every row attributed" 1 || ck "every row attributed" 0

echo "=== 3. forced tool loop ==="
cell pf-E eval/ablation/match.E.agent.md 1 "$A/analyze-notes.jsonl" sigs.index.seeded.tsv -pf 8061
t=$(grep -c 'executing tool' "${EVAL_DIR}/.state-eval-pf/run.log" 2>/dev/null); t=${t:-0}
[ "$t" -ge 5 ] && ck "the tool actually executed ($t times)" 1 || ck "the tool executed (got $t)" 0
grep -qE "mean match rounds/issue [1-9]\.[0-9]+" "$WORK/pf-E.log" && ck "two-round loop observed in rounds/issue" 1 || ck "two-round loop observed" 0
EXPECT_TOOL=lookup_sig bash eval/scripts/verify-run.sh -pf > "$WORK/verify-E.txt" 2>&1
grep -q "VERIFY PASSED" "$WORK/verify-E.txt" && ck "verify-run passes with EXPECT_TOOL" 1 || { ck "verify-run passes with EXPECT_TOOL" 0; sed 's/^/         /' "$WORK/verify-E.txt"; }
[ "$(mf "${EVAL_DIR}/results/runs/pf-E/run-manifest.json" index)" = "sigs.index.seeded.tsv" ] \
  && ck "manifest records which index was read" 1 || ck "manifest records the index" 0

echo "=== 4. unforced: offered-vs-called is distinguishable ==="
cell pf-Eauto eval/ablation/match.E.agent.md 0 "$A/analyze-notes.jsonl" sigs.index.tsv -pf 8061
grep -q "offered on 5/5" "$WORK/pf-Eauto.log" \
  && ck "tool offered on every row (so 'called 0' would mean declined, not absent)" 1 \
  || ck "tool offered on every row" 0
grep -qE "mean match rounds/issue [0-9]+\.[0-9]+" "$WORK/pf-Eauto.log" && ck "rounds/issue recorded" 1 || ck "rounds/issue recorded" 0

echo "=== 5. a killed cell still archives its evidence ==="
( RUN_TAG=pf-kill FORCE_TOOL=0 STATE_SUFFIX=-pfk LP_PORT=8062 CONCURRENCY=4 LOGPROB=1 \
  ANALYZE_CACHE="$A/analyze-notes.jsonl" bash eval/run-eval.sh > "$WORK/pf-kill.log" 2>&1 ) &
kp=$!
sleep 25; kill $kp 2>/dev/null
for p in $(pgrep -f "STATE_SUFFIX=-pfk"); do kill "$p" 2>/dev/null; done
sleep 3
[ -d "${EVAL_DIR}/results/runs/pf-kill" ] && ck "killed cell archived anyway (EXIT trap)" 1 || ck "killed cell archived anyway" 0
[ -f "${EVAL_DIR}/results/runs/pf-kill/run-manifest.json" ] && ck "killed cell kept its manifest" 1 || ck "killed cell kept its manifest" 0

echo "=== 6. parallel rigs do not share state ==="
if [ -n "${SECONDARY_ENDPOINT:-}" ] && [ -n "${SECONDARY_MODEL:-}" ]; then
  rm -rf "$WORK/agents2"; cp -r agents "$WORK/agents2"
  ( LEATHER_AGENT_DIR="$WORK/agents" RUN_TAG=pf-par1 STATE_SUFFIX=-pfp1 LP_PORT=8071 \
    CONCURRENCY=4 LOGPROB=1 bash eval/run-eval.sh > "$WORK/par1.log" 2>&1 ) &
  p1=$!
  ( LEATHER_LLM_ENDPOINT="$SECONDARY_ENDPOINT" LEATHER_MODEL="$SECONDARY_MODEL" \
    LEATHER_AGENT_DIR="$WORK/agents2" RUN_TAG=pf-par2 STATE_SUFFIX=-pfp2 LP_PORT=8072 \
    CONCURRENCY=4 LOGPROB=1 bash eval/run-eval.sh > "$WORK/par2.log" 2>&1 ) &
  p2=$!
  wait $p1; wait $p2
  for s in pfp1 pfp2; do
    h=$(grep -c 'hide missing' "${EVAL_DIR}/.state-eval-${s}/run.log" 2>/dev/null); h=${h:-0}
    [ "$h" = 0 ] && ck "rig $s: no queue cross-talk" 1 || ck "rig $s: $h 'hide missing' (shared queue store!)" 0
  done
  m1=$(mf "${EVAL_DIR}/results/runs/pf-par1/run-manifest.json" model)
  m2=$(mf "${EVAL_DIR}/results/runs/pf-par2/run-manifest.json" model)
  [ "$m1" != "$m2" ] && ck "each rig archived its own model ($m1 vs $m2)" 1 || ck "each rig archived its own model" 0
  rm -rf "${EVAL_DIR}"/.state-eval-pfp* "${EVAL_DIR}"/results/runs/pf-par*
else
  echo "  [skip] set SECONDARY_ENDPOINT and SECONDARY_MODEL to check parallel isolation"
fi

echo "=== 7. multi-turn arm (per-turn tool scope) ==="
cell pf-T2 eval/ablation/match.T2.agent.md 0 "$A/analyze-notes.jsonl" "" -pf 8061
b400=$(grep -c 'status 400' "${EVAL_DIR}/.state-eval-pf/run.log" 2>/dev/null); b400=${b400:-0}
[ "$b400" = 0 ] && ck "no 400s (turn scope uses tools:, not skills:)" 1 \
                || ck "no 400s (got $b400 — a turn-level skills: injects a mid-conversation system message)" 0
t=$(grep -c 'executing tool' "${EVAL_DIR}/.state-eval-pf/run.log" 2>/dev/null); t=${t:-0}
[ "$t" -ge 3 ] && ck "the fetch turn actually called the tool ($t)" 1 || ck "fetch turn called the tool (got $t)" 0
grep -qE "mean match rounds/issue [2-9]\." "$WORK/pf-T2.log" \
  && ck "rounds/issue > 2 — multi-turn is visible in the summary" 1 \
  || ck "rounds/issue reflects multi-turn (counter bug?)" 0
pg=$(grep -cE 'tool=hide_(next|jump)' "${EVAL_DIR}/.state-eval-pf/run.log" 2>/dev/null); pg=${pg:-0}
[ "$pg" = 0 ] && ck "no pagination" 1 || ck "no pagination (got $pg hide-nav calls)" 0

echo "=== 8. arm G (lookup returning full catalog entries) ==="
if grep -q lookup_sig_v2 shell-tools.json 2>/dev/null && [ -f eval/ablation/match.G.agent.md ]; then
  cell pf-G eval/ablation/match.G.agent.md 1 "$A/analyze-notes.jsonl" sigs.index.seeded.tsv -pf 8061
  g=$(grep -c 'tool=lookup_sig_v2' "${EVAL_DIR}/.state-eval-pf/run.log" 2>/dev/null); g=${g:-0}
  [ "$g" -ge 3 ] && ck "lookup_sig_v2 executed ($g)" 1 || ck "lookup_sig_v2 executed (got $g)" 0
  grep -q "attributed 5/5" "$WORK/pf-G.log" && ck "arm G attributed every row" 1 || ck "arm G attributed every row" 0
else
  echo "  [skip] lookup_sig_v2 / match.G.agent.md not present"
fi

echo
if [ "$fails" -eq 0 ]; then echo "PREFLIGHT GREEN"; else echo "PREFLIGHT RED ($fails failures)"; fi
exit $(( fails > 0 ))

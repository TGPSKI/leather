#!/usr/bin/env bash
# verify-run.sh — assert that a completed run did what its configuration claimed.
#
# Every load-bearing conclusion in results/ rests on an assumption about runtime
# behaviour: that the tool was offered, that it was (or was not) called, that the
# intended index was read, that decoding was greedy. Twice in this project such
# an assumption was silently false -- a proxy misfiled every match call under the
# wrong stage, and two rigs shared one queue store -- and in both cases the
# accuracy numbers looked entirely normal.
#
# So: no claim from the assumed configuration. Each check below reads a RUNTIME
# ARTIFACT, and the ones that matter most are cross-checked against two
# independent sources that must agree.
#
#   bash eval/scripts/verify-run.sh                 # the default single-run state
#   bash eval/scripts/verify-run.sh -35b            # a suffixed (parallel) run
#   EXPECT_TOOL=lookup_sig EXPECT_CALLS=250 bash eval/scripts/verify-run.sh -4b
#
# Exit 0 when every check passes, 1 otherwise. Unknowable checks report SKIP and
# do not fail the run -- an absent artifact is not evidence of correctness, and
# saying so is the point.
set -uo pipefail

EVAL_DIR="$(cd "$(dirname "$0")/.." && pwd)"
EX_DIR="$(cd "${EVAL_DIR}/.." && pwd)"
cd "$EX_DIR"
# --archive <dir> verifies a COMPLETED cell from results/runs/<tag>/ instead of live
# state. Live state is destroyed by the next cell, so without this a cell can never
# be re-verified — which is how one arm's tool evidence was lost.
ARCHIVE=""
if [ "${1:-}" = "--archive" ]; then ARCHIVE="${2:?--archive needs a directory}"; shift 2; fi
SUFFIX="${1:-}"
STATE="${EVAL_DIR}/.state-eval${SUFFIX}"
PRED="eval/predictions${SUFFIX}.jsonl"
CORPUS="${CORPUS:-eval/corpus.jsonl}"
RUNLOG="${STATE}/run.log"
LP="${STATE}/logprobs.jsonl"
MANIFEST="${STATE}/run-manifest.json"

fails=0
pass() { printf '  [PASS] %s\n' "$1"; }
fail() { printf '  [FAIL] %s\n' "$1"; fails=$((fails+1)); }
skip() { printf '  [SKIP] %s\n' "$1"; }

if [ -n "$ARCHIVE" ]; then
  echo "verifying ARCHIVE: ${ARCHIVE}"
  [ -d "$ARCHIVE" ] || { echo "  no such archive" >&2; exit 2; }
  MANIFEST="${ARCHIVE}/run-manifest.json"
  PRED="${ARCHIVE}/predictions.jsonl"
  LP="${ARCHIVE}/.logprobs.jsonl"      # decompressed below
  RUNLOG="${ARCHIVE}/.evidence.log"
  [ -f "${ARCHIVE}/logprobs.jsonl.gz" ] && gzip -dc "${ARCHIVE}/logprobs.jsonl.gz" > "$LP"
  [ -f "${ARCHIVE}/run-evidence.log.gz" ] && gzip -dc "${ARCHIVE}/run-evidence.log.gz" > "$RUNLOG"
  trap 'rm -f "$LP" "$RUNLOG"' EXIT
else
  echo "verifying run state: ${STATE}"
  [ -d "$STATE" ] || { echo "  no such state dir" >&2; exit 2; }
fi

# 1. Provenance. Without a manifest, every later check describes a run you cannot
#    identify afterwards -- which model, which index, which prompt.
if [ -f "$MANIFEST" ]; then
  python3 - "$MANIFEST" <<'PY'
import json, sys
m = json.load(open(sys.argv[1]))
for k in ("model", "endpoint", "agent_sha", "index", "index_sha", "corpus_sha", "git_commit"):
    print(f"  [INFO] {k}: {m.get(k, '(absent)')}")
PY
  pass "run manifest present"
else
  skip "no run-manifest.json — this run cannot be identified after the fact"
fi

# 2. Attribution completeness. A run missing rows is measuring the harness.
if [ -f "$PRED" ]; then
  want=$(grep -c . "$CORPUS"); got=$(grep -c . "$PRED")
  empty=$(python3 -c "
import json,sys
n=0
for l in open('$PRED'):
    if not l.strip(): continue
    p=json.loads(l)
    if p.get('predicted')=='unknown' and p.get('confidence')=='no-output': n+=1
print(n)")
  [ "$want" = "$got" ] && pass "attribution: $got/$want rows" || fail "attribution: $got/$want rows"
  [ "$empty" = "0" ] && pass "no rows lost by the harness" \
                     || fail "$empty rows had no usable match artifact"
else
  fail "no predictions file at $PRED"
fi

# 3. Harness errors. Nonzero means the run measured plumbing, not the model.
hm=$(grep -c 'hide missing' "$RUNLOG" 2>/dev/null); hm=${hm:-0}
pf=$(grep -c 'process failed' "$RUNLOG" 2>/dev/null); pf=${pf:-0}
[ "$hm" = "0" ] && pass "no 'hide missing' errors" || fail "$hm 'hide missing' errors (queue isolation?)"
[ "$pf" = "0" ] && pass "no stage failures" || fail "$pf stage failures"

# 4. Tool behaviour, cross-checked. leather's log proves a POSITIVE (a call ran);
#    the proxy's round count proves a NEGATIVE (a call forces a second round, so
#    1.00 rounds/issue means none happened). Neither alone is sufficient: the log
#    cannot distinguish "declined" from "never offered", and the proxy's stage
#    attribution has been wrong before. They must agree.
logcalls=$(grep -c 'executing tool' "$RUNLOG" 2>/dev/null); logcalls=${logcalls:-0}
if [ -s "$LP" ]; then
  read -r rounds issues offered <<<"$(python3 - "$LP" <<'PY'
import json, sys
recs = []
for l in open(sys.argv[1]):
    try: recs.append(json.loads(l))
    except Exception: pass
m = [r for r in recs if r.get("stage") == "match"]
iss = {r.get("issue") for r in m if r.get("issue") is not None}
offered = sum(1 for r in m if r.get("tools_offered"))
print(f"{len(m)} {len(iss)} {offered}")
PY
)"
  if [ "${issues:-0}" -gt 0 ]; then
    rpi=$(python3 -c "print(f'{$rounds/$issues:.2f}')")
    echo "  [INFO] match rounds/issue: $rpi over $issues issues; tools offered on $offered; leather logged $logcalls tool executions"
    twoway_ok=$(python3 -c "
rpi=$rounds/$issues; log=$logcalls
# >1.00 rounds/issue and a nonzero log agree that calls happened;
# ==1.00 and a zero log agree that none did. Anything else is a contradiction.
print('ok' if ((rpi>1.05) == (log>0)) else 'bad')")
    [ "$twoway_ok" = ok ] && pass "tool evidence agrees across proxy and leather log" \
                          || fail "CONTRADICTION: rounds/issue=$rpi but leather logged $logcalls tool executions"
  else
    skip "proxy recorded no match-stage rounds (stage detection?) — tool claims unverifiable from the proxy"
  fi
else
  skip "no logprobs.jsonl — run without LOGPROB=1; tool claims rest on the leather log alone"
fi

# 5. Expected tool, when the caller declares one.
if [ -n "${EXPECT_TOOL:-}" ]; then
  n=$(grep -c "tool=${EXPECT_TOOL}" "$RUNLOG" 2>/dev/null); n=${n:-0}
  if [ -n "${EXPECT_CALLS:-}" ]; then
    python3 -c "import sys; sys.exit(0 if $n >= int(0.9*$EXPECT_CALLS) else 1)" \
      && pass "${EXPECT_TOOL} executed $n times (expected ~${EXPECT_CALLS})" \
      || fail "${EXPECT_TOOL} executed only $n times, expected ~${EXPECT_CALLS}"
  else
    [ "$n" -gt 0 ] && pass "${EXPECT_TOOL} executed $n times" || fail "${EXPECT_TOOL} never executed"
  fi
fi

# 5b. Pagination. An oversized hide puts leather into reflection mode (paging
#     preamble, tools stripped per turn, N+1 alternating turns), so an arm that
#     believes it is single-turn silently measures a different mechanism.
pages=$(grep -cE 'tool=hide_(next|jump)' "$RUNLOG" 2>/dev/null); pages=${pages:-0}
if [ "$pages" -gt 0 ]; then
  fail "PAGINATED: $pages hide-navigation calls — this ran in reflection mode"
else
  pass "no pagination (single-page hide delivery)"
fi

# 5c. Attribution sanity. Artifacts are attributed by an ISSUE: line the model
#     COPIES, so a mis-transcribed id silently drops a row — or, worse, lands on
#     another real issue and overwrites its answer with every counter reading clean.
if [ -f "${ARCHIVE:-$STATE}/analyze-notes.jsonl" ]; then
  python3 - "${ARCHIVE:-$STATE}/analyze-notes.jsonl" "$CORPUS" <<'PYA'
import json, sys
notes = [json.loads(l)["number"] for l in open(sys.argv[1]) if l.strip()]
corpus = {json.loads(l)["number"] for l in open(sys.argv[2]) if l.strip()}
orphans = sorted(set(notes) - corpus)
dupes = sorted({n for n in notes if notes.count(n) > 1})
if orphans: print(f"  [FAIL] artifacts attributed to issues not in the corpus "
                  f"(model mis-transcribed an id): {orphans[:5]}")
if dupes:   print(f"  [FAIL] duplicate ISSUE attribution — one issue's answer may have "
                  f"overwritten another's: {dupes[:5]}")
if not orphans and not dupes: print("  [PASS] every artifact maps to a distinct corpus issue")
PYA
fi

# 6. Greedy decoding. The temperature trap cost a committed number once: the
#    agent said 0, the runtime used 0.7, and nothing anywhere disagreed.
if [ -s "$LP" ]; then
  temps=$(python3 - "$LP" <<'PY'
import json, sys
ts = set()
for l in open(sys.argv[1]):
    try:
        t = json.loads(l).get("temperature")
    except Exception:
        continue
    if t is not None: ts.add(t)
print(",".join(str(t) for t in sorted(ts)) if ts else "")
PY
)
  if [ -z "$temps" ]; then
    skip "temperature not recorded by the proxy (older run) — greedy decoding unverified"
  elif [ "$temps" = "0" ] || [ "$temps" = "0.0" ]; then
    pass "temperature 0 on every recorded request"
  else
    fail "temperature was $temps, not 0 — this run is not reproducible"
  fi
fi

echo
if [ "$fails" -eq 0 ]; then echo "VERIFY PASSED"; else echo "VERIFY FAILED ($fails)"; fi
exit $(( fails > 0 ))

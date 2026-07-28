#!/usr/bin/env bash
# SIG-catalog gate: run analyze->match over the labeled corpus, score, pass/fail.
# Isolated in eval/.state-eval/ (own tannery + curings). No `label` stage, no side effects.
#
#   LEATHER_MODEL=qwen3.6-4b-instruct-2507-awq LEATHER_LLM_ENDPOINT=http://127.0.0.1:8000/v1 \
#   MIN_ACCURACY=0.85 MAX_ABSTAIN=0.25 MIN_MACRO_RECALL=0.85 bash eval/run-eval.sh
#
# Scoring reads three committed artifacts: gold.jsonl (pristine fetch output),
# gold.overrides.jsonl (rule-generated relabels, applied on load) and
# splits.jsonl (which rows tuning was allowed to see).
set -euo pipefail

EX_DIR="$(cd "$(dirname "$0")/.." && pwd)"
ROOT_DIR="$(cd "${EX_DIR}/../.." && pwd)"
EVAL_DIR="${EX_DIR}/eval"
LEATHER="${LEATHER:-${ROOT_DIR}/leather}"
cd "$EX_DIR"

# leather resolves a tannery's hide_dir/artifact_dir/curing_dir relative to the
# tannery file's own directory -- NOT the cwd. To keep the writer (workflow run)
# and the reader (this script) pointed at the same place on any machine and from
# any cwd, rewrite those three keys to absolute paths at runtime and hand the
# resolved copy to workflow run. Nothing machine-specific is committed.
STATE_DIR="${EVAL_DIR}/.state-eval"
ARTIFACT_DIR="${STATE_DIR}/artifacts"
RESOLVED_TANNERY="${EVAL_DIR}/.tannery.resolved.yaml"
trap 'rm -f "$RESOLVED_TANNERY"' EXIT

awk -v hide="${STATE_DIR}/hides" -v art="${ARTIFACT_DIR}" -v cur="${EVAL_DIR}/curings" '
  /^hide_dir:/     { print "hide_dir: " hide; next }
  /^artifact_dir:/ { print "artifact_dir: " art; next }
  /^curing_dir:/   { print "curing_dir: " cur; next }
  { print }
' "${EVAL_DIR}/tannery.yaml" > "$RESOLVED_TANNERY"

export GITHUB_WEBHOOK_SECRET="${GITHUB_WEBHOOK_SECRET:-eval-secret}"
export LEATHER_DEMO_MODE="dry"
CORPUS="${CORPUS:-eval/corpus.jsonl}"       # blind input shown to the model (gold stripped)
GOLD="${GOLD:-eval/gold.jsonl}"             # answer key sigeval scores against
PRED="eval/predictions.jsonl"; : > "$PRED"
rm -rf "$STATE_DIR"

RUNLOG="${STATE_DIR}/run.log"; mkdir -p "$STATE_DIR"; : > "$RUNLOG"

# LOGPROB=1 routes the run through eval/scripts/logprob-proxy.py, which injects
# `logprobs: true` (leather has no knob for it) and records the top-token margin
# at the SIG decision, plus whether the catalog tool was actually OFFERED on each
# request. Verbalized confidence is a prompted self-report; the token margin is
# measured from the same forward pass, so the two can be compared head to head on
# one run rather than argued about.
LOGPROB_OUT="${STATE_DIR}/logprobs.jsonl"
if [ "${LOGPROB:-0}" = "1" ]; then
  LP_PORT="${LP_PORT:-8011}"
  # Fail closed if the port is already taken. A stale proxy from an earlier run
  # would answer the readiness probe and happily serve the whole eval while
  # recording to ITS output file, leaving this run's logprobs.jsonl empty and the
  # margins silently missing. An occupied port is an error, not something to
  # discover from a blank column later.
  if curl -fsS -m 1 "http://127.0.0.1:${LP_PORT}/v1/models" >/dev/null 2>&1; then
    echo "port ${LP_PORT} is already serving; refusing to start a second logprob proxy." >&2
    echo "  kill the stale one (pkill -f logprob-proxy.py) or set LP_PORT." >&2
    exit 2
  fi
  : > "$LOGPROB_OUT"
  UPSTREAM="${LEATHER_LLM_ENDPOINT:-http://127.0.0.1:8000}" PORT="$LP_PORT" \
    LOGPROB_OUT="$LOGPROB_OUT" python3 "${EVAL_DIR}/scripts/logprob-proxy.py" \
      >>"$RUNLOG" 2>&1 &
  LP_PID=$!
  trap 'kill "$LP_PID" 2>/dev/null; rm -f "$RESOLVED_TANNERY"' EXIT
  ready=0
  for _ in $(seq 1 20); do
    if curl -fsS -m 1 "http://127.0.0.1:${LP_PORT}/v1/models" >/dev/null 2>&1; then
      ready=1; break
    fi
    kill -0 "$LP_PID" 2>/dev/null || break   # it died; stop waiting
    sleep 0.25
  done
  if [ "$ready" != 1 ] || ! kill -0 "$LP_PID" 2>/dev/null; then
    echo "logprob proxy failed to start; see $RUNLOG" >&2; exit 2
  fi
  export LEATHER_LLM_ENDPOINT="http://127.0.0.1:${LP_PORT}"
  echo "logprob proxy on :${LP_PORT} -> recording ${LOGPROB_OUT}"
fi
total=$(grep -c . "$CORPUS"); i=0; mismatch=0; empty=0
echo "running analyze->match over ${total} issues..."
while IFS= read -r row; do
  [ -z "$row" ] && continue
  num=$(jq -r '.number' <<<"$row")
  hide=$(jq -c '{number,repo,title,body}' <<<"$row")     # gold sig/accept stripped from what the model sees
  rm -rf "${ARTIFACT_DIR}/match"
  # Keep the per-issue log. Discarding stderr hides the two things that silently
  # corrupt a run -- LLM retries and failed `get_sig_reference` calls (the skill
  # tells the agent to answer from memory when the catalog tool fails, which
  # quietly turns a read-the-catalog eval into a closed-book one).
  printf '%s' "$hide" | "$LEATHER" workflow run \
      --config eval/config.eval.yaml --tannery "$RESOLVED_TANNERY" \
      --curing analyze --queue analyze-in \
      --kind github.issues --source cli --settle 800ms >/dev/null 2>>"$RUNLOG" || true
  # Wait for THIS issue's match artifact, verifying provenance.
  #
  # `--settle` is a quiet-period heuristic, not a completion signal: under model
  # contention it can return before the match stage has written, leaving either
  # no artifact (-> a spurious `unknown`) or, worse, the PREVIOUS issue's artifact
  # still on disk, which would be silently scored as this issue's answer. The
  # agent copies `ISSUE:` verbatim, so the artifact carries the provenance needed
  # to tell those apart -- poll until an artifact whose ISSUE matches the row we
  # submitted appears, and count anything else rather than trusting it.
  out=""; stale=""
  for _ in $(seq 1 24); do
    cand=$(cat "${ARTIFACT_DIR}"/match/*.json 2>/dev/null | jq -r '.content // empty' || true)
    if [ -n "$cand" ]; then
      art=$(grep -m1 '^ISSUE:' <<<"$cand" | awk '{print $2}' || true)
      if [ "$art" = "$num" ]; then out="$cand"; break; fi
      stale="$art"          # an artifact, but not for this issue -- keep waiting
    fi
    sleep 0.25
  done
  if [ -z "$out" ] && [ -n "$stale" ]; then
    mismatch=$((mismatch+1))
    echo "  WARN #${num}: only saw artifact for ISSUE ${stale}; recording unknown" >&2
  fi
  sig=$(grep -m1 '^SIG:' <<<"$out" | awk '{print $2}' || true); [ -z "$sig" ] && sig="unknown"
  # A missing CONFIDENCE means we got no usable completion (timeout / empty), not
  # that the model expressed low confidence. Tag it distinctly so it lands in the
  # report's `(other)` bucket instead of poisoning the low-confidence stats that
  # confidence routing is calibrated against.
  conf=$(grep -m1 '^CONFIDENCE:' <<<"$out" | awk '{print tolower($2)}' || true); [ -z "$conf" ] && conf="no-output"
  # RUNNER_UP is the model's second choice; the scorer uses it to report how much
  # headroom a top-2 adjudicator stage would have. Absent (older prompt) -> none.
  ru=$(grep -m1 '^RUNNER_UP:' <<<"$out" | awk '{print tolower($2)}' || true); [ -z "$ru" ] && ru="none"
  jq -nc --argjson n "$num" --arg p "$sig" --arg r "$ru" --arg c "$conf" \
     '{number:$n,predicted:$p,runner_up:$r,confidence:$c}' >> "$PRED"
  [ -z "$out" ] && empty=$((empty+1))
  i=$((i+1)); printf '\r  %d/%d' "$i" "$total"
done < "$CORPUS"
echo
# Run-integrity line. These two counters are the difference between "the model
# was wrong" and "the harness lost the answer" -- a run with a nonzero count is
# measuring the harness, so surface it next to the score instead of burying it.
# Fold the proxy's per-request observations into predictions.jsonl so the scorer
# sees both uncertainty signals side by side. Keyed by issue; match stage only.
if [ "${LOGPROB:-0}" = "1" ] && [ -s "$LOGPROB_OUT" ]; then
  python3 - "$PRED" "$LOGPROB_OUT" <<'PY'
import json, sys
pred_p, lp_p = sys.argv[1], sys.argv[2]
lp = {}
for line in open(lp_p):
    try: r = json.loads(line)
    except Exception: continue
    if r.get("stage") != "match" or r.get("issue") is None:
        continue
    lp[r["issue"]] = r          # last match call for the issue wins
rows = []
for line in open(pred_p):
    if not line.strip(): continue
    p = json.loads(line)
    r = lp.get(p["number"])
    if r:
        p["sig_margin"] = r.get("sig_margin")
        p["commit_margin"] = r.get("commit_margin")
        p["tools_offered"] = bool(r.get("tools_offered"))
        p["tool_called"] = bool(r.get("tool_calls_made"))
    rows.append(p)
with open(pred_p, "w") as f:
    for p in rows:
        f.write(json.dumps(p) + "\n")
offered = sum(1 for p in rows if p.get("tools_offered"))
called = sum(1 for p in rows if p.get("tool_called"))
have = sum(1 for p in rows if p.get("sig_margin") is not None)
print(f"logprobs: {have}/{len(rows)} rows with a SIG margin; "
      f"catalog tool offered on {offered}/{len(rows)}, actually called {called}")
PY
fi

retries=$(grep -c "process failed" "$RUNLOG" 2>/dev/null || true)
toolfail=$(grep -ciE "tool.*(error|failed)|get_sig_reference.*(error|fail)" "$RUNLOG" 2>/dev/null || true)
echo "run integrity: ${empty}/${total} rows with no usable match artifact, ${mismatch} of those saw only another issue's artifact"
echo "               ${retries:-0} stage retries, ${toolfail:-0} tool errors  (full log: ${RUNLOG})"

( cd "$ROOT_DIR" && go run ./examples/14-sig-triage/eval/sigeval.go \
    -gold "examples/14-sig-triage/${GOLD}" -pred "examples/14-sig-triage/${PRED}" \
    -overrides "examples/14-sig-triage/${OVERRIDES:-eval/gold.overrides.jsonl}" \
    -split "examples/14-sig-triage/${SPLIT:-eval/splits.jsonl}" \
    -min-accuracy "${MIN_ACCURACY:-0.80}" -max-abstain "${MAX_ABSTAIN:-0.30}" \
    -min-macro-recall "${MIN_MACRO_RECALL:-0.85}" \
    -min-core-recall "${MIN_CORE_RECALL:-0.90}" \
    -min-class-support "${MIN_CLASS_SUPPORT:-20}" )

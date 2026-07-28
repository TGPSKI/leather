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
# STATE_SUFFIX namespaces every mutable path so two rigs can run at once (one
# eval per model server, in parallel). Empty by default -- the single-run paths
# are unchanged, so nothing that referenced .state-eval/ has to move.
STATE_DIR="${EVAL_DIR}/.state-eval${STATE_SUFFIX:-}"
ARTIFACT_DIR="${STATE_DIR}/artifacts"
RESOLVED_TANNERY="${EVAL_DIR}/.tannery.resolved${STATE_SUFFIX:-}.yaml"
trap 'rm -f "$RESOLVED_TANNERY"' EXIT

# CURING_DIR selects an alternate curing set. curings-nopage/ raises page_size_bytes so
# an oversized hide arrives in ONE page: the P1/P2 positional controls splice the catalog
# into the user turn (7215 B against the 6000 default), which silently switched them into
# reflection mode and made them measure a different delivery mechanism than their name says.
awk -v hide="${STATE_DIR}/hides" -v art="${ARTIFACT_DIR}" -v cur="${EVAL_DIR}/${CURING_DIR:-curings}" '
  /^hide_dir:/     { print "hide_dir: " hide; next }
  /^artifact_dir:/ { print "artifact_dir: " art; next }
  /^curing_dir:/   { print "curing_dir: " cur; next }
  { print }
' "${EVAL_DIR}/tannery.yaml" > "$RESOLVED_TANNERY"

export GITHUB_WEBHOOK_SECRET="${GITHUB_WEBHOOK_SECRET:-eval-secret}"
export LEATHER_DEMO_MODE="dry"
CORPUS="${CORPUS:-eval/corpus.jsonl}"       # blind input shown to the model (gold stripped)
GOLD="${GOLD:-eval/gold.jsonl}"             # answer key sigeval scores against
PRED="eval/predictions${STATE_SUFFIX:-}.jsonl"; : > "$PRED"
rm -rf "$STATE_DIR"

RUNLOG="${STATE_DIR}/run.log"; mkdir -p "$STATE_DIR"; : > "$RUNLOG"

# Provenance, written BEFORE the run. Without it, "which prompt produced 77.2%?"
# has no answer once the next cell overwrites this directory.
sha() { [ -f "$1" ] && sha256sum "$1" 2>/dev/null | cut -c1-12 || echo none; }
cat > "${STATE_DIR}/run-manifest.json" <<EOF
{
  "run_tag":    "${RUN_TAG:-unnamed}",
  "started":    "$(date -Iseconds)",
  "model":      "${LEATHER_MODEL:-unset}",
  "endpoint":   "${LEATHER_LLM_ENDPOINT:-unset}",
  "agent_dir":  "${LEATHER_AGENT_DIR:-agents}",
  "agent_sha":  "$(sha "${LEATHER_AGENT_DIR:-agents}/match.agent.md")",
  "index":      "${SIG_INDEX:-sigs.index.tsv}",
  "index_sha":  "$(sha "${SIG_INDEX:-sigs.index.tsv}")",
  "analyze_cache":     "${ANALYZE_CACHE:-none}",
  "analyze_cache_sha": "$(sha "${ANALYZE_CACHE:-/nonexistent}")",
  "corpus":     "${CORPUS}",
  "corpus_sha": "$(sha "${CORPUS}")",
  "git_commit": "$(git -C "$ROOT_DIR" rev-parse --short HEAD 2>/dev/null || echo none)",
  "force_tool": "${FORCE_TOOL:-0}",
  "concurrency": "${CONCURRENCY:-8}",
  "logprob":    "${LOGPROB:-0}"
}
EOF


# QUEUES live in leather's state_dir, not in the tannery's hide/artifact dirs, so
# STATE_SUFFIX alone does not isolate them. Two rigs sharing config.eval.yaml's
# `state_dir: .state` drain ONE queue store: each supervisor dequeues items whose
# hides live in the OTHER rig's hide_dir and DLQs every one of them
# ("curing/process: hide missing"). Pinning it under STATE_DIR also means the
# rm -rf above clears stale queue items, so a previous run's DLQ leftovers can no
# longer be re-drained by the next run.
export LEATHER_STATE_DIR="${STATE_DIR}/leather-state"

# Archive the evidence before the next run wipes this directory. Derived facts,
# not bulk: the analyze notes are what exposure analysis needs, and they are ~5%
# of the raw artifacts' size. Set RUN_TAG to name the cell.
archive_evidence() {
  [ -n "${RUN_TAG:-}" ] || return 0
  ARCH="${EVAL_DIR}/results/runs/${RUN_TAG}"
  mkdir -p "$ARCH"
  cp "${STATE_DIR}/run-manifest.json" "$ARCH/" 2>/dev/null
  cp "$PRED" "$ARCH/predictions.jsonl" 2>/dev/null
  [ -s "$LOGPROB_OUT" ] && gzip -c "$LOGPROB_OUT" > "$ARCH/logprobs.jsonl.gz"
  # The log lines that carry evidence: tool executions, failures, and the agent
  # responses that show whether a boundary was cited.
  grep -E 'executing tool|process failed|hide missing|agent response content' \
    "$RUNLOG" 2>/dev/null | gzip -c > "$ARCH/run-evidence.log.gz"
  python3 - "$ARTIFACT_DIR/analyze" "$ARCH/analyze-notes.jsonl" <<'PYARCH'
import glob, json, os, re, sys
src, out = sys.argv[1], sys.argv[2]
rows = []
for f in glob.glob(os.path.join(src, "*.json")):
    try: c = json.load(open(f)).get("content") or ""
    except Exception: continue
    m = re.search(r"^ISSUE:\s*(\d+)", c, re.M)
    if not m: continue
    def fld(n):
        h = re.search(rf"^{n}:\s*(.+)$", c, re.M)
        return h.group(1).strip() if h else ""
    rows.append({"number": int(m.group(1)), "note": c.strip(),
                 "components": fld("COMPONENTS"), "keywords": fld("KEYWORDS")})
rows.sort(key=lambda r: r["number"])
with open(out, "w") as fh:
    for r in rows: fh.write(json.dumps(r) + "\n")
print(f"archived {len(rows)} analyze notes", file=sys.stderr)
PYARCH
  echo "evidence archived to ${ARCH}"
}


trap 'archive_evidence; rm -f "$RESOLVED_TANNERY"' EXIT


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
    LOGPROB_OUT="$LOGPROB_OUT" FORCE_TOOL="${FORCE_TOOL:-0}" \
    python3 "${EVAL_DIR}/scripts/logprob-proxy.py" \
      >>"$RUNLOG" 2>&1 &
  LP_PID=$!
  trap 'kill "$LP_PID" 2>/dev/null; archive_evidence; rm -f "$RESOLVED_TANNERY"' EXIT
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
total=$(grep -c . "$CORPUS"); mismatch=0; empty=0

# Pipe the whole corpus into the queue, then drain ONCE with the queue's own
# concurrency. The previous design ran one `workflow run` per issue and waited,
# so the tannery's `concurrency:` was dead -- the queue never held more than one
# item and the run was strictly serial regardless of how much parallelism the
# model server had. Batch-ingest + a single drain is leather's actual concurrency
# model and lets vLLM batch the requests.
#
# This also retires the per-issue artifact race outright: instead of clearing the
# artifact dir and hoping the right file appears, every match artifact is kept and
# attributed by the `ISSUE:` line the agent copies verbatim. Provenance is read
# from the artifact rather than inferred from timing.
CONCURRENCY="${CONCURRENCY:-8}"
sed -i -E "s/^( +concurrency:) [0-9]+/\1 ${CONCURRENCY}/" "$RESOLVED_TANNERY"
# ANALYZE_CACHE replays frozen analyze notes directly into match-in, skipping the
# analyze stage. The ablation varies only the match agent, so re-running analyze
# per cell buys nothing and actively harms the comparison: it injects
# analyze-stage variance into a measurement meant to isolate the match prompt.
# With the cache, every arm sees byte-identical input.
if [ -n "${ANALYZE_CACHE:-}" ]; then
  [ -s "$ANALYZE_CACHE" ] || { echo "ANALYZE_CACHE $ANALYZE_CACHE is missing or empty" >&2; exit 2; }
  cached=$(grep -c . "$ANALYZE_CACHE")
  [ "$cached" = "$total" ] || { echo "ANALYZE_CACHE has $cached notes but the corpus has $total -- refusing to score a partial run" >&2; exit 2; }
  echo "replaying ${cached} cached analyze notes into match-in (analyze stage skipped)..."
  mapfile -t NOTES < <(python3 -c "
import json,sys
for l in open('$ANALYZE_CACHE'):
    if l.strip(): print(json.dumps(json.loads(l)['note']))
")
  last_idx=$(( ${#NOTES[@]} - 1 ))
  for idx in "${!NOTES[@]}"; do
    [ "$idx" -eq "$last_idx" ] && continue
    python3 -c "import json,sys;sys.stdout.write(json.loads(sys.argv[1]))" "${NOTES[$idx]}" \
      | "$LEATHER" ingest --config eval/config.eval.yaml --tannery "$RESOLVED_TANNERY" \
          --curing match --queue match-in --kind analyze --source cli \
          >/dev/null 2>>"$RUNLOG" || true
    printf '\r  queued %d/%d' "$((idx+1))" "$cached"
  done
  echo
  echo "draining match-in at concurrency ${CONCURRENCY} (${cached} notes)..."
  python3 -c "import json,sys;sys.stdout.write(json.loads(sys.argv[1]))" "${NOTES[$last_idx]}" \
    | "$LEATHER" workflow run --config eval/config.eval.yaml --tannery "$RESOLVED_TANNERY" \
        --curing match --queue match-in --kind analyze --source cli --settle 5s \
        >/dev/null 2>>"$RUNLOG" || true
else
echo "ingesting ${total} issues into analyze-in..."
mapfile -t ROWS < <(grep . "$CORPUS")
last_idx=$(( ${#ROWS[@]} - 1 ))
for idx in "${!ROWS[@]}"; do
  [ "$idx" -eq "$last_idx" ] && continue      # the last one goes in via `workflow run`
  jq -c '{number,repo,title,body}' <<<"${ROWS[$idx]}" | "$LEATHER" ingest \
      --config eval/config.eval.yaml --tannery "$RESOLVED_TANNERY" \
      --curing analyze --queue analyze-in \
      --kind github.issues --source cli >/dev/null 2>>"$RUNLOG" || true
  printf '\r  ingested %d/%d' "$((idx+1))" "$total"
done
echo
echo "draining queues at concurrency ${CONCURRENCY} (analyze->match over ${total} issues)..."
# The final hide is submitted by `workflow run`, which then runs the supervisor
# until every queue is quiescent -- draining the ones ingested above with it.
jq -c '{number,repo,title,body}' <<<"${ROWS[$last_idx]}" | "$LEATHER" workflow run \
    --config eval/config.eval.yaml --tannery "$RESOLVED_TANNERY" \
    --curing analyze --queue analyze-in \
    --kind github.issues --source cli --settle 5s >/dev/null 2>>"$RUNLOG" || true
fi

# Attribute every match artifact by its own ISSUE line.
python3 - "$ARTIFACT_DIR/match" "$CORPUS" "$PRED" <<'PY'
import glob, json, os, re, sys
art_dir, corpus_p, pred_p = sys.argv[1], sys.argv[2], sys.argv[3]
want = [json.loads(l)["number"] for l in open(corpus_p) if l.strip()]
got = {}
for f in glob.glob(os.path.join(art_dir, "*.json")):
    try:
        c = json.load(open(f)).get("content") or ""
    except Exception:
        continue
    m = re.search(r"^ISSUE:\s*(\d+)", c, re.M)
    if not m:
        continue
    got[int(m.group(1))] = c
def field(c, name, default):
    m = re.search(rf"^{name}:\s*(\S+)", c or "", re.M)
    return m.group(1).lower() if m else default
with open(pred_p, "w") as f:
    for n in want:
        c = got.get(n)
        f.write(json.dumps({
            "number": n,
            "predicted": field(c, "SIG", "unknown"),
            "runner_up": field(c, "RUNNER_UP", "none"),
            # Absent CONFIDENCE means no usable completion, not a hedge.
            "confidence": field(c, "CONFIDENCE", "no-output"),
        }) + "\n")
missing = [n for n in want if n not in got]
print(f"attributed {len(got)}/{len(want)} match artifacts by ISSUE line", file=sys.stderr)
if missing:
    print(f"MISSING {len(missing)}: {missing[:10]}", file=sys.stderr)
sys.exit(0)
PY
empty=$(jq -r 'select(.predicted=="unknown" and .confidence=="no-output")|.number' "$PRED" | grep -c . || true)
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
    # The LAST match round carries the answer (margins, predicted SIG), but a tool
    # call happens on an EARLIER round -- calling a tool forces a second round, and
    # that round's record has no tool_calls. Overwriting by issue therefore erases
    # every call that was actually made and reports "called 0" for a run where the
    # tool fired on every row. Keep the last record for the answer, but OR the tool
    # evidence across all of the issue's rounds.
    prev = lp.get(r["issue"])
    if prev:
        r = dict(r)
        r["tool_calls_made"] = (prev.get("tool_calls_made") or []) + (r.get("tool_calls_made") or [])
        r["rounds"] = (prev.get("rounds") or 1) + 1
    lp[r["issue"]] = r
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
# Rounds per issue is the independent check on the tool counter: a tool call
# forces a second round, so 1.00 rounds/issue IS the evidence that none happened.
# Count match-stage records per issue straight from the proxy log. Threading a
# tally through the predictions merge under-reported multi-turn arms as 1.00 --
# which is precisely the metric that distinguishes them from single-turn arms.
_seen = {}
for _l in open(lp_p):
    try: _r = json.loads(_l)
    except Exception: continue
    if _r.get("stage") == "match" and _r.get("issue") is not None:
        _seen[_r["issue"]] = _seen.get(_r["issue"], 0) + 1
rounds = (sum(_seen.values()) / len(_seen)) if _seen else 1.0
print(f"logprobs: {have}/{len(rows)} rows with a SIG margin; "
      f"catalog tool offered on {offered}/{len(rows)}, actually called {called}; "
      f"mean match rounds/issue {rounds:.2f} (>1.00 means a tool call happened)")
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
    -catalog "examples/14-sig-triage/sigs.reference.yaml" \
    -min-accuracy "${MIN_ACCURACY:-0.80}" -max-abstain "${MAX_ABSTAIN:-0.30}" \
    -min-macro-recall "${MIN_MACRO_RECALL:-0.85}" \
    -min-core-recall "${MIN_CORE_RECALL:-0.90}" \
    -min-class-support "${MIN_CLASS_SUPPORT:-20}" )

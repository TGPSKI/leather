#!/usr/bin/env bash
# EXPERIMENTAL, EVAL-HARNESS ONLY. Second-pass top-2 adjudication.
#
# This is NOT a leather feature and it is not a pattern to copy into a
# production tannery. leather has no content-conditional output routing: a
# curing's `output.queue` is a static name that fires on every success, and the
# tannery router matches on source/event_type/hide_kind (envelope metadata),
# never on what the artifact says. So the "route the uncertain ones to a
# tie-breaker" decision is made HERE, in the harness, by reading pass 1's
# predictions off disk and ingesting the selected minority into `adjudicate-in`
# by hand. See docs/LEP-0008 for the design that would let leather do this
# itself, and eval/README.md "Two-pass adjudication (experimental)".
#
# Routing is on the token-level logprob MARGIN, not on verbalized CONFIDENCE.
# That is the whole point: the margin separates right from wrong at AUROC
# ~0.71-0.73 on this corpus, the self-report at 0.483 (coin flip). A conditional
# routing feature inside leather could only see the artifact text, which means
# it could only route on the signal that does not work -- which is exactly why
# LEP-0008 has to carry the connector as well as the router.
#
#   bash eval/run-eval.sh                     # pass 1, LOGPROB=1 required
#   COVERAGE=20 bash eval/scripts/adjudicate-pass.sh
#
# Requires a completed LOGPROB=1 run: it reuses that run's .state-eval (analyze
# artifacts and margins). It never re-runs analyze or match.
set -euo pipefail

EVAL_DIR="$(cd "$(dirname "$0")/.." && pwd)"
EX_DIR="$(cd "${EVAL_DIR}/.." && pwd)"
ROOT_DIR="$(cd "${EX_DIR}/../.." && pwd)"
LEATHER="${LEATHER:-${ROOT_DIR}/leather}"
cd "$EX_DIR"

STATE_DIR="${EVAL_DIR}/.state-eval"
ARTIFACT_DIR="${STATE_DIR}/artifacts"
PRED="eval/predictions.jsonl"                     # pass 1, read-only here
MERGED="eval/predictions.adjudicated.jsonl"       # pass 1 + tie-breaks
WORK="${STATE_DIR}/adjudicate"
RUNLOG="${STATE_DIR}/adjudicate.log"

# Fraction of the corpus escalated to the tie-breaker, lowest margin first.
# This is the compute knob: coverage 20 means 1.2x total model calls on the
# match path. Do not tune it on the holdout split.
COVERAGE="${COVERAGE:-20}"
CONCURRENCY="${CONCURRENCY:-8}"

[ -s "$PRED" ] || { echo "no pass-1 predictions at $PRED -- run eval/run-eval.sh first" >&2; exit 2; }
[ -d "${ARTIFACT_DIR}/analyze" ] || { echo "no analyze artifacts in ${ARTIFACT_DIR}/analyze -- pass 1 state was cleared" >&2; exit 2; }

rm -rf "$WORK"; mkdir -p "$WORK"; : > "$RUNLOG"

RESOLVED_TANNERY="${EVAL_DIR}/.tannery.resolved.yaml"
trap 'rm -f "$RESOLVED_TANNERY"' EXIT
awk -v hide="${STATE_DIR}/hides" -v art="${ARTIFACT_DIR}" -v cur="${EVAL_DIR}/curings" '
  /^hide_dir:/     { print "hide_dir: " hide; next }
  /^artifact_dir:/ { print "artifact_dir: " art; next }
  /^curing_dir:/   { print "curing_dir: " cur; next }
  { print }
' "${EVAL_DIR}/tannery.yaml" > "$RESOLVED_TANNERY"
sed -i -E "s/^( +concurrency:) [0-9]+/\1 ${CONCURRENCY}/" "$RESOLVED_TANNERY"

export GITHUB_WEBHOOK_SECRET="${GITHUB_WEBHOOK_SECRET:-eval-secret}"
export LEATHER_DEMO_MODE="dry"

# Optional second logprob capture. Records the adjudicator's own margins and --
# a free extra data point on the shadow-catalog finding -- whether a THINKING
# agent actually calls get_sig_reference, which the match agent never does.
LOGPROB_OUT="${STATE_DIR}/logprobs-adjudicate.jsonl"
if [ "${LOGPROB:-0}" = "1" ]; then
  LP_PORT="${LP_PORT:-8011}"
  if curl -fsS -m 1 "http://127.0.0.1:${LP_PORT}/v1/models" >/dev/null 2>&1; then
    echo "port ${LP_PORT} is already serving; refusing to start a second logprob proxy." >&2
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
    curl -fsS -m 1 "http://127.0.0.1:${LP_PORT}/v1/models" >/dev/null 2>&1 && { ready=1; break; }
    kill -0 "$LP_PID" 2>/dev/null || break
    sleep 0.25
  done
  [ "$ready" = 1 ] || { echo "logprob proxy failed to start; see $RUNLOG" >&2; exit 2; }
  export LEATHER_LLM_ENDPOINT="http://127.0.0.1:${LP_PORT}"
fi

# ---- selection: lowest-margin COVERAGE% of the corpus -----------------------
# Candidate order is decided by issue-number parity, not by which one pass 1
# preferred. Presenting the top-1 first every time would let the adjudicator
# score well by simply agreeing with position 1, and we would never know. Parity
# keeps it deterministic (same split on a re-run) while balancing the positions.
python3 - "$PRED" "${ARTIFACT_DIR}/analyze" "$WORK" "$COVERAGE" <<'PY'
import glob, json, os, re, sys
pred_p, analyze_dir, work, coverage = sys.argv[1], sys.argv[2], sys.argv[3], float(sys.argv[4])

rows = [json.loads(l) for l in open(pred_p) if l.strip()]
have = [r for r in rows if r.get("sig_margin") is not None]
if len(have) < 0.5 * len(rows):
    sys.exit(f"only {len(have)}/{len(rows)} rows carry a sig_margin; "
             "adjudication routes on the margin, so pass 1 must run with LOGPROB=1")

# Eligible: a real prediction with a real, different runner-up. Nothing to
# tie-break when the first pass abstained or named no second candidate.
elig = [r for r in have
        if r.get("predicted") not in (None, "", "unknown")
        and r.get("runner_up") not in (None, "", "none", "unknown")
        and r["runner_up"] != r["predicted"]]
elig.sort(key=lambda r: (r["sig_margin"], r["number"]))
n = int(round(len(rows) * coverage / 100.0))
picked = elig[:n]

notes = {}
for f in glob.glob(os.path.join(analyze_dir, "*.json")):
    try:
        c = json.load(open(f)).get("content") or ""
    except Exception:
        continue
    m = re.search(r"^ISSUE:\s*(\d+)", c, re.M)
    if m:
        notes[int(m.group(1))] = c.strip()

sel, missing = [], []
for r in picked:
    note = notes.get(r["number"])
    if not note:
        missing.append(r["number"])
        continue
    top, runner = r["predicted"], r["runner_up"]
    c1, c2 = (top, runner) if r["number"] % 2 == 0 else (runner, top)
    body = note if re.search(r"^ISSUE:", note, re.M) else f"ISSUE: {r['number']}\n{note}"
    body += f"\n\nCANDIDATE_1: {c1}\nCANDIDATE_2: {c2}\n"
    open(os.path.join(work, f"{r['number']}.txt"), "w").write(body)
    sel.append({"number": r["number"], "cand1": c1, "cand2": c2,
                "pass1": top, "runner_up": runner, "margin": r["sig_margin"]})

with open(os.path.join(work, "selection.jsonl"), "w") as f:
    for s in sel:
        f.write(json.dumps(s) + "\n")
band = f"{picked[0]['sig_margin']:.3f}..{picked[-1]['sig_margin']:.3f}" if picked else "n/a"
print(f"escalating {len(sel)}/{len(rows)} rows ({100.0*len(sel)/len(rows):.1f}% coverage), "
      f"margin band {band}")
if len(elig) < n:
    print(f"  note: only {len(elig)} rows were eligible for a tie-break, "
          f"fewer than the {n} requested by COVERAGE={coverage:g}")
if missing:
    print(f"  note: {len(missing)} selected rows had no analyze artifact and were dropped: {missing[:10]}")
PY

count=$(grep -c . "${WORK}/selection.jsonl" || true)
[ "${count:-0}" -gt 0 ] || { echo "nothing to adjudicate" >&2; exit 0; }

# ---- pass 2: ingest the selected minority, drain once ------------------------
mapfile -t FILES < <(ls "${WORK}"/*.txt)
last_idx=$(( ${#FILES[@]} - 1 ))
for idx in "${!FILES[@]}"; do
  [ "$idx" -eq "$last_idx" ] && continue
  "$LEATHER" ingest --config eval/config.eval.yaml --tannery "$RESOLVED_TANNERY" \
      --curing adjudicate --queue adjudicate-in \
      --kind sig.adjudicate --source cli < "${FILES[$idx]}" >/dev/null 2>>"$RUNLOG" || true
  printf '\r  ingested %d/%d' "$((idx+1))" "${#FILES[@]}"
done
echo
echo "draining adjudicate-in at concurrency ${CONCURRENCY} (${count} tie-breaks)..."
"$LEATHER" workflow run --config eval/config.eval.yaml --tannery "$RESOLVED_TANNERY" \
    --curing adjudicate --queue adjudicate-in \
    --kind sig.adjudicate --source cli --settle 5s < "${FILES[$last_idx]}" \
    >/dev/null 2>>"$RUNLOG" || true

# ---- merge -------------------------------------------------------------------
# Fail-safe: any tie-break that is missing, inconsistent (VERDICT and SIG
# disagree), off-vocabulary (names a SIG that was not on the ballot) or
# `neither` leaves pass 1's answer standing. The adjudicator can improve the
# score or leave it alone; it cannot actively destroy it by refusing to answer.
# Each of those outcomes is counted, because a pass that "helps" only by having
# silently declined 40% of its cases is not a result.
python3 - "$PRED" "${ARTIFACT_DIR}/adjudicate" "${WORK}/selection.jsonl" "$MERGED" <<'PY'
import glob, json, os, re, sys
pred_p, art_dir, sel_p, out_p = sys.argv[1:5]
rows = [json.loads(l) for l in open(pred_p) if l.strip()]
sel = {}
for l in open(sel_p):
    if l.strip():
        s = json.loads(l)
        sel[s["number"]] = s

got = {}
for f in glob.glob(os.path.join(art_dir, "*.json")):
    try:
        c = json.load(open(f)).get("content") or ""
    except Exception:
        continue
    m = re.search(r"^ISSUE:\s*(\d+)", c, re.M)
    if m:
        got[int(m.group(1))] = c

def field(c, name):
    m = re.search(rf"^{name}:\s*(\S+)", c or "", re.M)
    return m.group(1).strip().lower() if m else None

applied = changed = 0
skips = {"no-artifact": 0, "inconsistent": 0, "off-ballot": 0, "neither": 0}
for r in rows:
    s = sel.get(r["number"])
    if not s:
        continue
    c = got.get(r["number"])
    if not c:
        skips["no-artifact"] += 1
        continue
    verdict, sig = field(c, "VERDICT"), field(c, "SIG")
    if verdict == "neither" or sig == "neither":
        skips["neither"] += 1
        continue
    ballot = {"1": s["cand1"], "2": s["cand2"]}
    if verdict not in ballot:
        skips["inconsistent"] += 1
        continue
    if sig != ballot[verdict]:          # VERDICT and SIG disagree -> discard both
        skips["inconsistent"] += 1
        continue
    if sig not in (s["cand1"], s["cand2"]):
        skips["off-ballot"] += 1
        continue
    r["adjudicated"] = True
    r["pass1_predicted"] = s["pass1"]
    applied += 1
    if sig != r["predicted"]:
        r["predicted"] = sig
        changed += 1

with open(out_p, "w") as f:
    for r in rows:
        f.write(json.dumps(r) + "\n")

n = len(sel)
line = f"adjudicated {applied}/{n} escalated rows; {changed} changed the prediction"
if n - applied:
    why = ", ".join(f"{k}={v}" for k, v in skips.items() if v)
    line += f" ({n - applied} left standing at pass 1: {why})"
print(line)
print(f"compute: {len(rows)} match calls + {n} adjudicate calls = "
      f"{1 + n/len(rows):.2f}x on the decision path")
PY

# ---- score: merged vs pass 1, on the same gates ------------------------------
( cd "$ROOT_DIR" && go run ./examples/14-sig-triage/eval/sigeval.go \
    -gold "examples/14-sig-triage/${GOLD:-eval/gold.jsonl}" \
    -pred "examples/14-sig-triage/${MERGED}" \
    -overrides "examples/14-sig-triage/${OVERRIDES:-eval/gold.overrides.jsonl}" \
    -split "examples/14-sig-triage/${SPLIT:-eval/splits.jsonl}" \
    -catalog "examples/14-sig-triage/sigs.reference.yaml" \
    -flip-vs "examples/14-sig-triage/${PRED}" \
    -min-accuracy "${MIN_ACCURACY:-0.80}" -max-abstain "${MAX_ABSTAIN:-0.30}" \
    -min-macro-recall "${MIN_MACRO_RECALL:-0.85}" \
    -min-core-recall "${MIN_CORE_RECALL:-0.90}" \
    -min-class-support "${MIN_CLASS_SUPPORT:-20}" )

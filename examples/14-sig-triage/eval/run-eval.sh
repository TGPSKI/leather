#!/usr/bin/env bash
# SIG-catalog gate: run analyze->match over the labeled corpus, score, pass/fail.
# Isolated in eval/.state-eval/ (own tannery + curings). No `label` stage, no side effects.
#
#   LEATHER_MODEL=qwen3.6-4b-instruct-2507-awq LEATHER_LLM_ENDPOINT=http://127.0.0.1:8000/v1 \
#   MIN_ACCURACY=0.85 MAX_ABSTAIN=0.25 MIN_CORE_RECALL=0.90 bash eval/run-eval.sh
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

total=$(grep -c . "$CORPUS"); i=0
echo "running analyze->match over ${total} issues..."
while IFS= read -r row; do
  [ -z "$row" ] && continue
  num=$(jq -r '.number' <<<"$row")
  hide=$(jq -c '{number,repo,title,body}' <<<"$row")     # gold sig/accept stripped from what the model sees
  rm -rf "${ARTIFACT_DIR}/match"
  printf '%s' "$hide" | "$LEATHER" workflow run \
      --config eval/config.eval.yaml --tannery "$RESOLVED_TANNERY" \
      --curing analyze --queue analyze-in \
      --kind github.issues --source cli --settle 800ms >/dev/null 2>&1 || true
  # Tolerate a timed-out / empty artifact (model contention): degrade to
  # unknown/low and keep going. Without the `|| true` guards, grep/cat returning
  # non-zero on empty content trips `set -e`+pipefail and aborts the whole run.
  out=$(cat "${ARTIFACT_DIR}"/match/*.json 2>/dev/null | jq -r '.content // empty' || true)
  sig=$(grep -m1 '^SIG:' <<<"$out" | awk '{print $2}' || true); [ -z "$sig" ] && sig="unknown"
  conf=$(grep -m1 '^CONFIDENCE:' <<<"$out" | awk '{print tolower($2)}' || true); [ -z "$conf" ] && conf="low"
  jq -nc --argjson n "$num" --arg p "$sig" --arg c "$conf" \
     '{number:$n,predicted:$p,confidence:$c}' >> "$PRED"
  i=$((i+1)); printf '\r  %d/%d' "$i" "$total"
done < "$CORPUS"
echo

( cd "$ROOT_DIR" && go run ./examples/14-sig-triage/eval/sigeval.go \
    -gold "examples/14-sig-triage/${GOLD}" -pred "examples/14-sig-triage/${PRED}" \
    -min-accuracy "${MIN_ACCURACY:-0.80}" -max-abstain "${MAX_ABSTAIN:-0.30}" \
    -min-core-recall "${MIN_CORE_RECALL:-0.90}" )

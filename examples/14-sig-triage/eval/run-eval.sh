#!/usr/bin/env bash
# SIG-catalog gate: run analyze->match over the labeled corpus, score, pass/fail.
# Isolated in .state-eval/ (own tannery + curings). No `label` stage, no side effects.
#
#   LEATHER_MODEL=qwen3.6-4b-instruct-2507-awq LEATHER_LLM_ENDPOINT=http://127.0.0.1:8000/v1 \
#   MIN_ACCURACY=0.85 MAX_ABSTAIN=0.25 MIN_CORE_RECALL=0.90 bash eval/run-eval.sh
set -euo pipefail

EX_DIR="$(cd "$(dirname "$0")/.." && pwd)"
ROOT_DIR="$(cd "${EX_DIR}/../.." && pwd)"
LEATHER="${LEATHER:-${ROOT_DIR}/leather}"
cd "$EX_DIR"

export GITHUB_WEBHOOK_SECRET="${GITHUB_WEBHOOK_SECRET:-eval-secret}"
export LEATHER_DEMO_MODE="dry"
CORPUS="${CORPUS:-eval/corpus.jsonl}"
PRED="eval/predictions.jsonl"; : > "$PRED"
rm -rf .state-eval

total=$(grep -c . "$CORPUS"); i=0
echo "running analyze->match over ${total} issues..."
while IFS= read -r row; do
  [ -z "$row" ] && continue
  num=$(jq -r '.number' <<<"$row")
  hide=$(jq -c '{number,repo,title,body}' <<<"$row")     # gold sig/accept stripped from what the model sees
  rm -rf .state-eval/artifacts/match
  printf '%s' "$hide" | "$LEATHER" workflow run \
      --config config.yaml --tannery eval/tannery.yaml \
      --curing analyze --queue analyze-in \
      --kind github.issues --source cli --settle 800ms >/dev/null 2>&1 || true
  out=$(cat .state-eval/artifacts/match/*.json 2>/dev/null | jq -r '.content // empty')
  sig=$(grep -m1 '^SIG:' <<<"$out" | awk '{print $2}'); [ -z "$sig" ] && sig="unknown"
  conf=$(grep -m1 '^CONFIDENCE:' <<<"$out" | awk '{print tolower($2)}'); [ -z "$conf" ] && conf="low"
  jq -nc --argjson n "$num" --arg p "$sig" --arg c "$conf" \
     '{number:$n,predicted:$p,confidence:$c}' >> "$PRED"
  i=$((i+1)); printf '\r  %d/%d' "$i" "$total"
done < "$CORPUS"
echo

( cd "$ROOT_DIR" && go run ./examples/14-sig-triage/eval/sigeval.go \
    -corpus "examples/14-sig-triage/${CORPUS}" -pred "examples/14-sig-triage/${PRED}" \
    -min-accuracy "${MIN_ACCURACY:-0.80}" -max-abstain "${MAX_ABSTAIN:-0.30}" \
    -min-core-recall "${MIN_CORE_RECALL:-0.90}" )

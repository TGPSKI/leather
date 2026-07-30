#!/usr/bin/env bash
# 4B re-run + ergonomics probe — RUN POST-BATTERY against the rig.
# 14 cells: the 10 PILOT-1 cells re-run at the corrected max_tokens (24576),
# plus the signed ergonomics probe (Be, V2e: edit_file search/replace
# primitive instead of full-file write_file) x both families.
# Usage: LEATHER_LLM_ENDPOINT=http://10.0.0.64:8000 bash .pilot/grid-4b-rerun.sh
set -uo pipefail
export LEATHER_MODEL="${LEATHER_MODEL:-/home/tyler/llm/models/Qwen3-4B-Instruct-2507-AWQ}"
export LEATHER_LLM_ENDPOINT="${LEATHER_LLM_ENDPOINT:?set to the 4B rig endpoint}"
export SKEPTIC_ROOT="${SKEPTIC_ROOT:-$HOME/git/TGPSKI/skeptic}"
cd "$(dirname "$0")/.."
for inst in gh-actions/mutable-action-pin/v1 gh-actions/unsafe-prt-checkout/v1; do
  short=$(basename "$(dirname "$inst")" | sed 's/mutable-action-pin/pin/;s/unsafe-prt-checkout/prt/')
  for arm in B R E V V2 Be V2e; do
    tag="pilot2-4b-${arm}-${short}-v1"
    [ -f "eval/results/runs/$tag/verdict.json" ] && { echo "skip $tag (done)"; continue; }
    echo "=== $tag ==="
    bash eval/run-instance.sh "$arm" "$inst" "$tag" || true
  done
done
echo "PILOT-GRID-DONE"

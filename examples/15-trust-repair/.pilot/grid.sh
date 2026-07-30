#!/usr/bin/env bash
set -uo pipefail
export LEATHER_MODEL=qwen3-4b-instruct-2507-awq
export LEATHER_LLM_ENDPOINT=http://127.0.0.1:8000
export SKEPTIC_ROOT="$HOME/git/TGPSKI/skeptic"
cd "$(dirname "$0")/.."
for inst in gh-actions/mutable-action-pin/v1 gh-actions/unsafe-prt-checkout/v1; do
  short=$(basename "$(dirname "$inst")" | sed 's/mutable-action-pin/pin/;s/unsafe-prt-checkout/prt/')
  for arm in B R E V V2; do
    tag="pilot-${arm}-${short}-v1"
    [ -f "eval/results/runs/$tag/verdict.json" ] && { echo "skip $tag (done)"; continue; }
    echo "=== $tag ==="
    bash eval/run-instance.sh "$arm" "$inst" "$tag" || true
  done
done
echo "PILOT-GRID-DONE"

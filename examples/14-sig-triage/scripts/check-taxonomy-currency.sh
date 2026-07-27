#!/usr/bin/env bash
# check-taxonomy-currency.sh — the cheap cron trigger for the self-updating loop.
#
# Hashes the current upstream SIG list (kubernetes/community sigs.yaml) and
# compares to the last hash we regenerated against. On drift it regenerates the
# name list, diffs it against sigs.reference.yaml, and EMITS A SIGNAL (exit 10 +
# a marker file) that a wrapping leather cron/agent acts on: open a catalog PR,
# then gate the change with eval/run-eval.sh before merge.
#
# Deterministic and quiet on the common path (no drift -> exit 0, no output).
#
#   */60 * * * *  cd .../14-sig-triage && scripts/check-taxonomy-currency.sh
#
# Exit: 0 = current, 10 = drift detected (signal), 2 = fetch error.
set -euo pipefail

EX_DIR="$(cd "$(dirname "$0")/.." && pwd)"
cd "$EX_DIR"
STORED="eval/.taxonomy.sha"
MARKER="eval/.taxonomy-drift"

# authoritative current names, hashed
current_names="$(bash scripts/gen-taxonomy.sh 2>/dev/null)" || { echo "fetch failed" >&2; exit 2; }
[ -n "$current_names" ] || { echo "empty upstream fetch" >&2; exit 2; }
current_sha="$(printf '%s\n' "$current_names" | sha256sum | cut -d' ' -f1)"

prev_sha="$(cat "$STORED" 2>/dev/null || echo none)"
if [ "$current_sha" = "$prev_sha" ]; then
  exit 0            # current: nothing to do
fi

# drift: show what changed vs what the catalog currently encodes
catalog_names="$(sed -nE 's/^\s*-\s*name:\s*(sig-[a-z-]+).*/\1/p' sigs.reference.yaml | sort -u)"
added="$(comm -23 <(printf '%s\n' "$current_names" | sort -u) <(printf '%s\n' "$catalog_names"))"
removed="$(comm -13 <(printf '%s\n' "$current_names" | sort -u) <(printf '%s\n' "$catalog_names"))"

{
  echo "SIG taxonomy drift detected."
  echo "upstream_sha=$current_sha prev_sha=$prev_sha"
  [ -n "$added" ]   && { echo "added (need features authored):"; printf '  + %s\n' $added; }
  [ -n "$removed" ] && { echo "removed (retire from catalog):"; printf '  - %s\n' $removed; }
  echo
  echo "next: regenerate features for added SIGs, open catalog PR, then gate:"
  echo "  LEATHER_MODEL=... LEATHER_LLM_ENDPOINT=... bash eval/run-eval.sh"
  echo "record acceptance by writing $current_sha to $STORED after the PR merges."
} | tee "$MARKER" >&2

exit 10

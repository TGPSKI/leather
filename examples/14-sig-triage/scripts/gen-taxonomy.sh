#!/usr/bin/env bash
# Regenerate the SIG name list from kubernetes/community sigs.yaml.
# sigs.reference.yaml seeds a current list already; run this to check for drift,
# then reconcile the curated `features` lists by hand (they aren't in sigs.yaml).
set -euo pipefail
URL="https://raw.githubusercontent.com/kubernetes/community/master/sigs.yaml"
# Upstream writes these as list items -- "  - dir: sig-api-machinery" -- so the
# optional "- " is required. Without it this matched nothing and returned empty,
# which check-taxonomy-currency.sh reports as a fetch error (exit 2), silently
# disabling the whole currency loop rather than failing loudly.
out="$(curl -fsSL "$URL" | sed -nE 's/^[[:space:]]*-?[[:space:]]*dir:[[:space:]]*(sig-[a-z0-9-]+).*/\1/p' | sort -u)"
[ -n "$out" ] || { echo "gen-taxonomy: upstream parse produced no SIG names (format change?)" >&2; exit 2; }
printf '%s\n' "$out"

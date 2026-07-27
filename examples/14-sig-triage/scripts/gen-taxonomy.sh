#!/usr/bin/env bash
# Regenerate the SIG name list from kubernetes/community sigs.yaml.
# sigs.reference.yaml seeds a current list already; run this to check for drift,
# then reconcile the curated `features` lists by hand (they aren't in sigs.yaml).
set -euo pipefail
URL="https://raw.githubusercontent.com/kubernetes/community/master/sigs.yaml"
curl -fsSL "$URL" \
  | grep -E '^\s*dir:\s*sig-' \
  | sed -E 's/.*dir:\s*//' \
  | sort -u

#!/usr/bin/env bash
# Pull open Kubernetes issues that have no SIG yet and ingest each as a hide.
# The k8s triage bot applies `needs-sig` when no sig/* label is present, so that
# label is the clean "no SIG picked" signal. Override with LABEL=.
set -euo pipefail

REPO="${REPO:-kubernetes/kubernetes}"
LABEL="${LABEL:-needs-sig}"
LIMIT="${LIMIT:-50}"
CONFIG="${CONFIG:-config.yaml}"
BODY_CAP="${BODY_CAP:-6000}"   # chars of body kept, to stay within one hide cut

mkdir -p .state/tmp

gh issue list -R "$REPO" --state open --label "$LABEL" --limit "$LIMIT" \
    --json number,title,body \
  | jq -c '.[]' \
  | while read -r issue; do
      num=$(jq -r '.number' <<<"$issue")
      jq --arg repo "$REPO" --argjson cap "$BODY_CAP" \
        '{number, repo: $repo, title, body: ((.body // "")[0:$cap])}' <<<"$issue" \
        > ".state/tmp/issue-${num}.json"
      leather ingest --config "$CONFIG" \
        --kind github.issues --curing analyze --queue analyze-in \
        ".state/tmp/issue-${num}.json"
      echo "ingested #${num}"
    done

echo "---"
echo "Drain (dry, default):  leather serve --config ${CONFIG} --tannery tannery.yaml --run-duration 300s"
echo "Apply comments live:   LEATHER_DEMO_MODE=live SIG_ACTION=comment leather serve --config ${CONFIG} --tannery tannery.yaml --run-duration 300s"
echo "Apply labels live:     LEATHER_DEMO_MODE=live SIG_ACTION=label   leather serve --config ${CONFIG} --tannery tannery.yaml --run-duration 300s"

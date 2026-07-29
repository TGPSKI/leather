#!/usr/bin/env bash
# run-demo.sh — sig-triage linear pipeline (analyze → match → label).
#   ingest one issue hide on stdin → workflow drains all three curings to quiescence.
#
# Modes:
#   LEATHER_DEMO_MODE=dry  (default) — apply_sig prints what it would do
#   LEATHER_DEMO_MODE=live           — apply_sig calls gh (needs auth + repo rights)
#   SIG_ACTION=comment (default) | label | both — selects the last-step action
set -euo pipefail

EX_DIR="$(cd "$(dirname "$0")/.." && pwd)"
ROOT_DIR="$(cd "${EX_DIR}/../.." && pwd)"
LEATHER="${LEATHER:-${ROOT_DIR}/leather}"

source "${EX_DIR}/scripts/pretty.sh"
source "${EX_DIR}/../scripts/preflight.sh"

export GITHUB_WEBHOOK_SECRET="${GITHUB_WEBHOOK_SECRET:-sig-triage-demo-secret}"

if [ "$(lth_demo_mode)" = "live" ]; then
  lth_live_requirements "14-sig-triage" \
    "gh CLI installed and authenticated (gh auth login)" \
    "apply_sig posts a /sig comment and/or a sig/* label on a repo you control" \
    "SIG_ACTION selects comment (default) | label | both"
  lth_require_gh_auth || exit $?
fi

echo ""
lth_step "14" "sig-triage  analyze → match → label"
lth_mode_banner "14"
lth_cont "issue → [analyze] → match-in → [match+catalog] → label-in → [label+apply_sig]"
lth_cont "action: ${SIG_ACTION:-comment}"
echo ""

mkdir -p "${EX_DIR}/.state"
cd "${EX_DIR}"
lth_step "workflow" "draining pipeline to quiescence"
cat sample/issue.json | "${LEATHER}" workflow run \
  --config  config.yaml \
  --tannery tannery.yaml \
  --curing  analyze \
  --queue   analyze-in \
  --kind    github.issues \
  --source  cli \
  --settle  800ms

echo ""
lth_step "done" "stage artifacts:"
for s in analyze match label; do
  f=$(find ".state/artifacts/${s}" -type f 2>/dev/null | sort | tail -1 || true)
  [ -n "$f" ] || continue
  lth_cont ""
  lth_cont "${s}:"
  jq -r '.content // "(empty)"' "$f" | while IFS= read -r line; do lth_cont "  $line"; done
done

#!/usr/bin/env bash
# run-demo.sh — CI gate demo (multi-agent fan-out + single-use queue fan-in):
#   1. start leather serve (tannery webhook mode)
#   2. wait for the API to come up
#   3. POST a signed GitHub PR event (eval payload, PR #42)
#   4. webhook fan-out creates per-event queues: pr-meta-<id>, pr-diff-<id>, pr-ctx-<id>
#   5. three analysis agents run in parallel, each consuming their single-use queue
#   6. each outputs to analysis-<id> (single-use join queue, keyed to this event)
#   7. decision curing prefix-scans for analysis-* queues, collects 3 items, runs join
#   8. decision agent writes FULL_EVAL or SKIP artifact
#   9. pr-comments agent posts PR comment + label
#
# PARALLEL_WEBHOOK_EVENTS=true (default: false)
#   false — send eval PR (#42), wait for decision artifact, then send skip PR (#43)
#   true  — send eval PR (#42) in background, immediately send skip PR (#43);
#            both fan-outs run concurrently, producing interleaved single-use queues
set -euo pipefail

EX_DIR="$(cd "$(dirname "$0")/.." && pwd)"
ROOT_DIR="$(cd "${EX_DIR}/../.." && pwd)"
LEATHER="${ROOT_DIR}/leather"

source "${EX_DIR}/scripts/pretty.sh"
# shellcheck source=../../scripts/preflight.sh
source "${EX_DIR}/../scripts/preflight.sh"

export GITHUB_WEBHOOK_SECRET="${GITHUB_WEBHOOK_SECRET:-ci-gate-demo-secret}"
export API_ADDR="${API_ADDR:-127.0.0.1:7749}"

PARALLEL_WEBHOOK_EVENTS="${PARALLEL_WEBHOOK_EVENTS:-false}"

mkdir -p "${EX_DIR}/.state"

# Preflight: in live mode, ensure GH CLI is authenticated before serving.
if [ "$(lth_demo_mode)" = "live" ]; then
  lth_live_requirements "10-ci-gate" \
    "gh CLI installed and authenticated (gh auth login)" \
    "repo write access (post_pr_comment, add_pr_label make real changes)" \
    "the configured webhook payloads target a repo you control"
  lth_require_gh_auth || exit $?
fi

echo ""
lth_step "10" "ci-gate  fan-out/fan-in multi-agent PR pipeline"
lth_mode_banner "10"
lth_cont "webhook → [pr-metadata | pr-diff | pr-context] (parallel, single-use queues)"
lth_cont "  → analysis-<event-id> (single-use join queue, collect_size: 3)"
lth_cont "  → decision → pr-comments (post comment + label)"
if [ "${PARALLEL_WEBHOOK_EVENTS}" = "true" ]; then
  lth_cont "mode: PARALLEL — eval (#42) + skip (#43) sent concurrently"
else
  lth_cont "mode: serial — eval (#42) fires first; skip (#43) fires after decision written"
fi
echo ""

lth_step "serve" "starting  api=${API_ADDR}  run-duration=180s"
(cd "${EX_DIR}" && "${LEATHER}" serve \
  --config  config.yaml \
  --tannery tannery.yaml \
  --mcp-servers-file mcp-servers.yaml \
  --api-addr "${API_ADDR}" \
  --log-file "${EX_DIR}/.state/serve.log" \
  --pretty --stats --run-duration 180s --api) &
SERVE_PID=$!
trap 'kill $SERVE_PID 2>/dev/null || true; wait $SERVE_PID 2>/dev/null || true' EXIT

lth_step "serve" "waiting for API  http://${API_ADDR}/healthz"
for i in $(seq 1 30); do
  if curl -fsS "http://${API_ADDR}/healthz" >/dev/null 2>&1; then
    break
  fi
  sleep 0.5
done

# ── send webhooks ─────────────────────────────────────────────────────────────

send_eval() {
  echo ""
  lth_step "webhook" "PR #42 — eval payload  (code change, needs full evaluation)"
  lth_cont "routes: pr-meta-<id>  pr-diff-<id>  pr-ctx-<id>  (single-use queues)"
  echo ""
  PAYLOAD_FILE="${EX_DIR}/sample/pr-event-eval.json" \
    bash "${EX_DIR}/scripts/send-webhook.sh" || {
    lth_step "error" "eval webhook POST failed — check .state/serve.log"
    exit 1
  }
}

send_skip() {
  echo ""
  lth_step "webhook" "PR #43 — skip payload  (docs-only change, expect SKIP decision)"
  lth_cont "routes: pr-meta-<id>  pr-diff-<id>  pr-ctx-<id>  (single-use queues)"
  echo ""
  PAYLOAD_FILE="${EX_DIR}/sample/pr-event-skip.json" \
    bash "${EX_DIR}/scripts/send-webhook.sh" || {
    lth_step "error" "skip webhook POST failed — check .state/serve.log"
    exit 1
  }
}

if [ "${PARALLEL_WEBHOOK_EVENTS}" = "true" ]; then
  # Fire both events at the same time. Both fan-outs will be in-flight
  # concurrently; the tree will show interleaved single-use queues.
  (trap - EXIT; send_eval) &
  EVAL_PID=$!
  (trap - EXIT; send_skip) &
  SKIP_PID=$!
  wait $EVAL_PID || true
  wait $SKIP_PID || true
else
  # Serial: eval first, wait for its decision artifact, then send skip.
  send_eval

  echo ""
  lth_step "wait" "waiting for eval decision artifact (up to 90s)…"
  lth_cont "decision curing prefix-scans analysis-* and collects 3 results for PR #42"
  for i in $(seq 1 180); do
    if [ -n "$(find "${EX_DIR}/.state/artifacts/decision" -type f 2>/dev/null || true)" ]; then
      break
    fi
    sleep 0.5
  done

  echo ""
  lth_step "artifact" "PR #42 decision output:"
  find "${EX_DIR}/.state/artifacts/decision" -type f 2>/dev/null \
    | sort | tail -1 \
    | while read -r f; do
        lth_cont ""
        lth_cont "$(basename "$f"):"
        jq -r '.content // "(empty)"' "$f" 2>/dev/null \
          | sed 's/[[:space:]]*$//' \
          | fold -s -w 90 \
          | while IFS= read -r line; do lth_cont "  $line"; done \
          || lth_cont "  (unreadable)"
      done \
    || lth_cont "(artifact not yet written — check .state/serve.log)"
  echo ""

  send_skip
fi

# ── wait for both decision artifacts ─────────────────────────────────────────

echo ""
lth_step "wait" "waiting for all decision artifacts (up to 90s)…"
lth_cont "expecting: decision artifact for PR #42 (FULL_EVAL) and PR #43 (SKIP)"

for i in $(seq 1 180); do
  count=$(find "${EX_DIR}/.state/artifacts/decision" -type f 2>/dev/null | wc -l) || true
  count="${count//[[:space:]]/}"
  if [ "${count:-0}" -ge 2 ]; then
    break
  fi
  sleep 0.5
done

echo ""
lth_step "artifact" "decision outputs:"
find "${EX_DIR}/.state/artifacts/decision" -type f 2>/dev/null \
  | sort \
  | while read -r f; do
      lth_cont ""
      lth_cont "$(basename "$f"):"
      jq -r '.content // "(empty)"' "$f" 2>/dev/null \
        | sed 's/[[:space:]]*$//' \
        | fold -s -w 90 \
        | while IFS= read -r line; do lth_cont "  $line"; done \
        || lth_cont "  (unreadable)"
    done

echo ""

wait $SERVE_PID 2>/dev/null || true
trap - EXIT

echo ""
lth_step "done" "pipeline complete"
lth_cont "Artifacts:"
lth_cont "  .state/artifacts/pr-metadata/   — file list + concern paths"
lth_cont "  .state/artifacts/pr-diff/        — logic changes + risk signals"
lth_cont "  .state/artifacts/pr-context/     — PR intent + urgency"
lth_cont "  .state/artifacts/decision/       — FULL_EVAL or SKIP decision (one per PR event)"
lth_cont "  .state/artifacts/pr-comments/    — comment + label confirmation"
lth_cont ""
lth_cont "Re-run with parallel fan-out:"
lth_cont "  PARALLEL_WEBHOOK_EVENTS=true make 10"
lth_cont ""
lth_cont "Production webhook:"
lth_cont "  http://<your-server>/webhooks/github"
lth_cont "  Content-Type: application/json  Secret: \$GITHUB_WEBHOOK_SECRET"
lth_cont "  Events: Pull requests"

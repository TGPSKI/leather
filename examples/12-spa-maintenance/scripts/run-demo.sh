#!/usr/bin/env bash
# run-demo.sh — SPA maintenance demo:
#
#   Scheduled agents (fire every minute in demo mode):
#     site-health    — HTTP + TLS check for {{site_url}}
#     dep-audit      — npm outdated + security audit for sample/
#     content-drift  — README/CHANGELOG version sync check for sample/
#
#   Webhook-driven curing (triggered by this script):
#     deploy-check   — verify site is up after a GitHub push to main
#
# The demo runs for 90 seconds so all scheduled agents fire at least once.
# After the first scheduled round completes (~60s), a sample push event is sent.
#
# Env:
#   LEATHER_LLM_ENDPOINT   model server base URL (default http://localhost:11434)
#   LEATHER_MODEL          model name (default llama3)
#   GITHUB_WEBHOOK_SECRET  HMAC secret for push webhook (default spa-demo-secret)
#   API_ADDR               leather API address (default 127.0.0.1:7749)
set -euo pipefail

EX_DIR="$(cd "$(dirname "$0")/.." && pwd)"
ROOT_DIR="$(cd "${EX_DIR}/../.." && pwd)"
LEATHER="${ROOT_DIR}/leather"

source "${EX_DIR}/scripts/pretty.sh"
# shellcheck source=../../scripts/preflight.sh
source "${EX_DIR}/../scripts/preflight.sh"

lth_mode_banner "12"

export GITHUB_WEBHOOK_SECRET="${GITHUB_WEBHOOK_SECRET:-spa-demo-secret}"
export API_ADDR="${API_ADDR:-127.0.0.1:7749}"
export LEATHER_LLM_ENDPOINT="${LEATHER_LLM_ENDPOINT:-http://localhost:11434}"
export LEATHER_MODEL="${LEATHER_MODEL:-llama3}"

mkdir -p "${EX_DIR}/.state"

echo ""
lth_step "12" "spa-maintenance  scheduled health + dep-audit + content-drift + deploy-check"
lth_cont "scheduled:  site-health  dep-audit  content-drift  (every minute, demo cadence)"
lth_cont "webhook:    push → deploy-check curing"
lth_cont "endpoint:   ${LEATHER_LLM_ENDPOINT}  model: ${LEATHER_MODEL}"
echo ""

# ── start serve ───────────────────────────────────────────────────────────────

lth_step "serve" "starting  api=${API_ADDR}  run-duration=180s"
(cd "${EX_DIR}" && "${LEATHER}" serve \
  --config       config.yaml \
  --tannery      tannery.yaml \
  --mcp-servers-file mcp-servers.yaml \
  --api-addr     "${API_ADDR}" \
  --log-file     "${EX_DIR}/.state/serve.log" \
  --pretty --stats --run-duration 180s --api) &
SERVE_PID=$!
trap 'kill $SERVE_PID 2>/dev/null || true; wait $SERVE_PID 2>/dev/null || true' EXIT

# ── wait for API ──────────────────────────────────────────────────────────────

lth_step "serve" "waiting for API  http://${API_ADDR}/healthz"
for i in $(seq 1 30); do
  if curl -fsS "http://${API_ADDR}/healthz" >/dev/null 2>&1; then
    lth_step "serve" "$(lth_green "ready")"
    break
  fi
  sleep 0.5
done

# ── wait for first scheduler round ───────────────────────────────────────────

lth_step "scheduler" "waiting 65s for first scheduled round (all three agents)"
lth_cont "site-health, dep-audit, content-drift each fire at the next :00 minute mark"
sleep 65

# ── send push webhook to trigger deploy-check ─────────────────────────────────

echo ""
lth_step "webhook" "sending push event → deploy-check curing"
PAYLOAD_FILE="${EX_DIR}/sample/push-event.json" \
GITHUB_EVENT="push" \
  bash "${EX_DIR}/scripts/send-webhook.sh" || {
  lth_step "error" "webhook POST failed — check .state/serve.log"
  exit 1
}

# ── wait for deploy-check to complete ─────────────────────────────────────────

lth_step "curing" "waiting for deploy-check artifact..."
sleep 20

# ── print artifacts ───────────────────────────────────────────────────────────

echo ""
lth_step "artifacts" ".state/artifacts/"
for dir in site-health dep-audit content-drift deploy-check; do
  artifact_dir="${EX_DIR}/.state/artifacts/${dir}"
  if [ -d "$artifact_dir" ]; then
    latest=$(ls -t "$artifact_dir"/*.json 2>/dev/null | head -1)
    if [ -n "$latest" ]; then
      echo ""
      lth_step "${dir}" "$(lth_dim "${latest##*/}")"
      # Extract and print the content field
      content=$(grep -o '"content":"[^"]*"' "$latest" 2>/dev/null | head -1 | sed 's/"content":"//;s/"$//' | sed 's/\\n/\n/g' || true)
      if [ -n "$content" ]; then
        while IFS= read -r line; do
          lth_cont "$line"
        done <<< "$content"
      fi
    fi
  fi
done

echo ""
lth_step "done" "serve window closing (90s elapsed)"
wait $SERVE_PID 2>/dev/null || true

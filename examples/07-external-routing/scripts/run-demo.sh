#!/usr/bin/env bash
# Orchestrate the external-routing demo end-to-end.
set -euo pipefail

EX_DIR="$(cd "$(dirname "$0")/.." && pwd)"
ROOT_DIR="$(cd "${EX_DIR}/../.." && pwd)"
LEATHER="${ROOT_DIR}/leather"

# shellcheck source=scripts/pretty.sh
source "${EX_DIR}/scripts/pretty.sh"

export LEATHER_WEBHOOK_SECRET="${LEATHER_WEBHOOK_SECRET:-demo-secret-change-me}"
export API_ADDR="${API_ADDR:-127.0.0.1:7749}"

mkdir -p "${EX_DIR}/.state"

lth_step "script" "serve  starting  api=${API_ADDR}"
(cd "${EX_DIR}" && "${LEATHER}" serve \
  --config config.yaml \
  --tannery tannery.yaml \
  --api --api-addr "${API_ADDR}" \
  --log-file "${EX_DIR}/.state/serve.log" \
  --pretty --stats --run-duration 60s) &
SERVE_PID=$!
trap 'kill $SERVE_PID 2>/dev/null || true; wait $SERVE_PID 2>/dev/null || true' EXIT

lth_step "script" "serve  waiting for API  http://${API_ADDR}/healthz"
for i in $(seq 1 30); do
  if curl -fsS "http://${API_ADDR}/healthz" >/dev/null 2>&1; then
    break
  fi
  sleep 0.5
done

EVENT_TYPE="pull_request_review_comment" PAYLOAD_FILE="sample/review-comment.json" \
  bash "${EX_DIR}/scripts/send-webhook.sh"
EVENT_TYPE="issues" PAYLOAD_FILE="sample/issue-opened.json" \
  bash "${EX_DIR}/scripts/send-webhook.sh"

if [[ "${SEND_TELEGRAM_EVENT:-0}" == "1" ]]; then
  # Fail fast when Telegram path is requested but no secret can be resolved.
  if [[ -z "${LEATHER_TELEGRAM_BOT_TOKEN:-}" ]]; then
    if ! command -v pass >/dev/null 2>&1 || ! pass show telegram/YOUR_BOT >/dev/null 2>&1; then
      lth_step "error" "telegram enabled but token is unavailable"
      lth_cont "Set LEATHER_TELEGRAM_BOT_TOKEN or configure pass entry: telegram/YOUR_BOT"
      exit 1
    fi
  fi
  lth_step "script" "telegram path enabled (deployment_status)"
  EVENT_TYPE="deployment_status" PAYLOAD_FILE="sample/deploy-failed.json" \
    bash "${EX_DIR}/scripts/send-webhook.sh"
fi

lth_step "script" "demo  waiting for artifacts"
for i in $(seq 1 40); do
  if [ -n "$(find "${EX_DIR}/.state/artifacts" -mindepth 2 -type f 2>/dev/null || true)" ]; then
    break
  fi
  sleep 0.5
done

echo ""
wait $SERVE_PID 2>/dev/null || true
trap - EXIT

#!/usr/bin/env bash
# Orchestrate the webhook demo end-to-end:
#   1. start leather serve in the background
#   2. wait for the API to come up
#   3. POST the signed sample payload
#   4. wait briefly for the worker to produce an artifact
#   5. shut serve down and print the artifact
set -euo pipefail

EX_DIR="$(cd "$(dirname "$0")/.." && pwd)"
ROOT_DIR="$(cd "${EX_DIR}/../.." && pwd)"
LEATHER="${ROOT_DIR}/leather"

# shellcheck source=scripts/pretty.sh
source "${EX_DIR}/scripts/pretty.sh"

# Stable demo secret. In real deployments use a strong random value.
export LEATHER_WEBHOOK_SECRET="${LEATHER_WEBHOOK_SECRET:-demo-secret-change-me}"
export API_ADDR="${API_ADDR:-127.0.0.1:7749}"

mkdir -p "${EX_DIR}/.state"

lth_step "script" "serve  starting  api=${API_ADDR}"
(cd "${EX_DIR}" && "${LEATHER}" serve \
  --config  config.yaml \
  --tannery tannery.yaml \
  --api-addr "${API_ADDR}" \
  --log-file "${EX_DIR}/.state/serve.log" \
  --pretty --stats --run-duration 45s --api) &
SERVE_PID=$!
trap 'kill $SERVE_PID 2>/dev/null || true; wait $SERVE_PID 2>/dev/null || true' EXIT

lth_step "script" "serve  waiting for API  http://${API_ADDR}/healthz"
for i in $(seq 1 30); do
  if curl -fsS "http://${API_ADDR}/healthz" >/dev/null 2>&1; then
    break
  fi
  sleep 0.5
done

lth_step "script" "demo  sending signed webhook"
bash "${EX_DIR}/scripts/send-webhook.sh" || {
  lth_step "error" "webhook POST failed"
  cat "${EX_DIR}/.state/serve.log" 2>/dev/null || lth_cont "(no log file)"
  exit 1
}

lth_step "script" "demo  waiting for artifact  up to 20s"
for i in $(seq 1 40); do
  if [ -n "$(find "${EX_DIR}/.state/artifacts" -mindepth 2 -type f 2>/dev/null || true)" ]; then
    break
  fi
  sleep 0.5
done

echo ""

# Let serve exit on its --run-duration so output flushes cleanly.
wait $SERVE_PID 2>/dev/null || true
trap - EXIT

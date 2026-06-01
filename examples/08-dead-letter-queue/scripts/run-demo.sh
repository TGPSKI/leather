#!/usr/bin/env bash
# Orchestrate DLQ demo end-to-end:
# 1) ingest one item
# 2) run serve so worker retries and sends to fail-in-dlq
# 3) verify DLQ depth via /queues/fail-in-dlq
set -euo pipefail

EX_DIR="$(cd "$(dirname "$0")/.." && pwd)"
ROOT_DIR="$(cd "${EX_DIR}/../.." && pwd)"
LEATHER="${ROOT_DIR}/leather"

# shellcheck source=scripts/pretty.sh
source "${EX_DIR}/scripts/pretty.sh"

API_ADDR="${API_ADDR:-127.0.0.1:7749}"

mkdir -p "${EX_DIR}/.state"

lth_step "script" "ingest  demo.failure -> fail-demo@fail-in"
(cd "${EX_DIR}" && "${LEATHER}" ingest \
  --config config.yaml \
  --tannery tannery.yaml \
  --kind demo.failure --source cli \
  --curing fail-demo --queue fail-in \
  sample/input.json >/dev/null)

lth_step "script" "serve  starting  api=${API_ADDR}  run-duration=20s"
(cd "${EX_DIR}" && "${LEATHER}" serve \
  --config config.yaml \
  --tannery tannery.yaml \
  --api --api-addr "${API_ADDR}" \
  --log-file "${EX_DIR}/.state/serve.log" \
  --pretty --stats --run-duration 45s) &
SERVE_PID=$!
trap 'kill $SERVE_PID 2>/dev/null || true; wait $SERVE_PID 2>/dev/null || true' EXIT

lth_step "script" "serve  waiting for API  http://${API_ADDR}/healthz"
for i in $(seq 1 30); do
  if curl -fsS "http://${API_ADDR}/healthz" >/dev/null 2>&1; then
    break
  fi
  sleep 0.5
done

lth_step "script" "verify  waiting for fail-in-dlq len >= 1"
DLQ_LEN=0
for i in $(seq 1 40); do
  DLQ_JSON=$(curl -fsS "http://${API_ADDR}/queues/fail-in-dlq" || true)
  DLQ_LEN=$(printf '%s' "$DLQ_JSON" | grep -o '"len":[0-9]*' | head -1 | cut -d: -f2)
  DLQ_LEN=${DLQ_LEN:-0}
  if [[ "$DLQ_LEN" -ge 1 ]]; then
    break
  fi
  sleep 0.5
done

if [[ "$DLQ_LEN" -lt 1 ]]; then
  lth_step "error" "DLQ item not observed (len=${DLQ_LEN})"
  lth_cont "Inspect .state/serve.log for retry/dlq events"
  exit 1
fi

lth_step "result" "$(lth_dim "fail-in-dlq len=${DLQ_LEN}")"

echo ""
wait $SERVE_PID 2>/dev/null || true
trap - EXIT

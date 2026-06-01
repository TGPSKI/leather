#!/usr/bin/env bash
# Orchestrate DLQ requeue demo end-to-end:
# 1) ingest one item
# 2) wait for first DLQ promotion
# 3) POST /queues/fail-in/requeue
# 4) verify item cycles back to DLQ again
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

lth_step "script" "serve  starting  api=${API_ADDR}  run-duration=25s"
(cd "${EX_DIR}" && "${LEATHER}" serve \
  --config config.yaml \
  --tannery tannery.yaml \
  --api --api-addr "${API_ADDR}" \
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

wait_for_dlq() {
  local deadline="${1:-40}"
  local dlq_len="0"
  for i in $(seq 1 "$deadline"); do
    local dlq_json
    dlq_json=$(curl -fsS "http://${API_ADDR}/queues/fail-in-dlq" || true)
    dlq_len=$(printf '%s' "$dlq_json" | grep -o '"len":[0-9]*' | head -1 | cut -d: -f2)
    dlq_len=${dlq_len:-0}
    if [[ "$dlq_len" -ge 1 ]]; then
      printf '%s' "$dlq_len"
      return 0
    fi
    sleep 0.5
  done
  printf '%s' "$dlq_len"
  return 1
}

lth_step "script" "verify  waiting for initial fail-in-dlq len >= 1"
if ! DLQ_LEN=$(wait_for_dlq 40); then
  lth_step "error" "initial DLQ item not observed (len=${DLQ_LEN})"
  lth_cont "Inspect .state/serve.log for retry/dlq events"
  exit 1
fi
lth_step "result" "$(lth_dim "initial fail-in-dlq len=${DLQ_LEN}")"

lth_step "script" "action  POST /queues/fail-in/requeue"
REQUEUE_JSON=$(curl -fsS -X POST "http://${API_ADDR}/queues/fail-in/requeue")
REQUEUED=$(printf '%s' "$REQUEUE_JSON" | grep -o '"requeued":[0-9]*' | head -1 | cut -d: -f2)
REQUEUED=${REQUEUED:-0}
if [[ "$REQUEUED" -lt 1 ]]; then
  lth_step "error" "requeue returned requeued=${REQUEUED}"
  lth_cont "response: ${REQUEUE_JSON}"
  exit 1
fi
lth_step "result" "$(lth_dim "requeued=${REQUEUED}")"

lth_step "script" "verify  waiting for item to cycle back to fail-in-dlq"
if ! DLQ_LEN2=$(wait_for_dlq 40); then
  lth_step "error" "post-requeue DLQ item not observed (len=${DLQ_LEN2})"
  lth_cont "Inspect .state/serve.log for retry/dlq events"
  exit 1
fi
lth_step "result" "$(lth_dim "post-requeue fail-in-dlq len=${DLQ_LEN2}")"

echo ""
wait $SERVE_PID 2>/dev/null || true
trap - EXIT

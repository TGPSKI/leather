#!/usr/bin/env bash
# Send a sample payload to /webhooks/github with HMAC signing.
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
# shellcheck source=scripts/pretty.sh
source "${SCRIPT_DIR}/pretty.sh"

cd "${SCRIPT_DIR}/.."

: "${LEATHER_WEBHOOK_SECRET:?LEATHER_WEBHOOK_SECRET must be set}"
API_ADDR="${API_ADDR:-127.0.0.1:7749}"
WEBHOOK_PATH="${WEBHOOK_PATH:-/webhooks/github}"
EVENT_TYPE="${EVENT_TYPE:?EVENT_TYPE must be set}"
PAYLOAD_FILE="${PAYLOAD_FILE:?PAYLOAD_FILE must be set}"

SIG=$(openssl dgst -sha256 -hmac "$LEATHER_WEBHOOK_SECRET" -binary < "$PAYLOAD_FILE" | xxd -p -c 256)

lth_step "POST" "$(lth_dim "${EVENT_TYPE}")  $(lth_dim "${PAYLOAD_FILE}")"
lth_cont "http://${API_ADDR}${WEBHOOK_PATH}"

RESP=$(curl -fsS -X POST \
  -H 'Content-Type: application/json' \
  -H "X-GitHub-Event: ${EVENT_TYPE}" \
  -H "X-Hub-Signature-256: sha256=${SIG}" \
  --data-binary "@${PAYLOAD_FILE}" \
  "http://${API_ADDR}${WEBHOOK_PATH}")

CURING=$(lth_json_get "$RESP" "curing")
HIDE_ID=$(lth_json_get "$RESP" "hide_id")
QUEUE=$(lth_json_get "$RESP" "queue")

lth_cont "$(lth_dim "response=202")  $(lth_dim "${HIDE_ID}  → ${CURING} @ ${QUEUE}")"

#!/usr/bin/env bash
# Send sample/payload.json to the running leather webhook endpoint with HMAC.
#
# Env:
#   LEATHER_WEBHOOK_SECRET   shared HMAC secret (required)
#   API_ADDR                 server address (default 127.0.0.1:7749)
#   WEBHOOK_PATH             path (default /webhooks/demo)
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
# shellcheck source=scripts/pretty.sh
source "${SCRIPT_DIR}/pretty.sh"

cd "${SCRIPT_DIR}/.."

: "${LEATHER_WEBHOOK_SECRET:?LEATHER_WEBHOOK_SECRET must be set}"
API_ADDR="${API_ADDR:-127.0.0.1:7749}"
WEBHOOK_PATH="${WEBHOOK_PATH:-/webhooks/demo}"

PAYLOAD_FILE="sample/payload.json"
SIG=$(openssl dgst -sha256 -hmac "$LEATHER_WEBHOOK_SECRET" -binary < "$PAYLOAD_FILE" | xxd -p -c 256)

lth_step "payload" "$(lth_dim "${PAYLOAD_FILE}")"
while IFS= read -r line; do
  lth_cont "$(lth_dim "$line")"
done < "$PAYLOAD_FILE"
echo ""

lth_step "POST" "$(lth_dim "http://${API_ADDR}${WEBHOOK_PATH}")"
lth_cont "X-Hub-Signature-256: $(lth_dim "sha256=${SIG}")"
echo ""

RESP=$(curl -fsS -X POST \
  -H 'Content-Type: application/json' \
  -H "X-Hub-Signature-256: sha256=${SIG}" \
  --data-binary "@${PAYLOAD_FILE}" \
  "http://${API_ADDR}${WEBHOOK_PATH}")

CURING=$(lth_json_get "$RESP" "curing")
HIDE_ID=$(lth_json_get "$RESP" "hide_id")
QUEUE=$(lth_json_get "$RESP" "queue")

lth_step "http" "$(lth_dim "response=202")  $(lth_dim "${HIDE_ID}  → ${QUEUE}")"
echo ""

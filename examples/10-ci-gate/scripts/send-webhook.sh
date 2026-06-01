#!/usr/bin/env bash
# send-webhook.sh — sign and POST a sample GitHub PR event to the running
#                   leather webhook endpoint.
#
# Env:
#   GITHUB_WEBHOOK_SECRET   shared HMAC secret (required)
#   API_ADDR                server address (default 127.0.0.1:7749)
#   WEBHOOK_PATH            path (default /webhooks/github)
#   PAYLOAD_FILE            payload file to POST (default sample/pr-event-eval.json)
#   GITHUB_EVENT            X-GitHub-Event header value (default pull_request)
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
source "${SCRIPT_DIR}/pretty.sh"

cd "${SCRIPT_DIR}/.."

: "${GITHUB_WEBHOOK_SECRET:?GITHUB_WEBHOOK_SECRET must be set}"
API_ADDR="${API_ADDR:-127.0.0.1:7749}"
WEBHOOK_PATH="${WEBHOOK_PATH:-/webhooks/github}"
PAYLOAD_FILE="${PAYLOAD_FILE:-sample/pr-event-eval.json}"
GITHUB_EVENT="${GITHUB_EVENT:-pull_request}"

SIG=$(openssl dgst -sha256 -hmac "$GITHUB_WEBHOOK_SECRET" -binary < "$PAYLOAD_FILE" \
      | xxd -p -c 256)

lth_step "payload" "$(lth_dim "${PAYLOAD_FILE}")  event=$(lth_dim "${GITHUB_EVENT}")"
while IFS= read -r line; do
  lth_cont "$(lth_dim "$line")"
done < "$PAYLOAD_FILE"
echo ""

lth_step "POST" "$(lth_dim "http://${API_ADDR}${WEBHOOK_PATH}")"
lth_cont "X-Hub-Signature-256: $(lth_dim "sha256=${SIG}")"
lth_cont "X-GitHub-Event:      $(lth_dim "${GITHUB_EVENT}")"
echo ""

curl -fsS -o /dev/null \
  -X POST \
  -H 'Content-Type: application/json' \
  -H "X-Hub-Signature-256: sha256=${SIG}" \
  -H "X-GitHub-Event: ${GITHUB_EVENT}" \
  --data-binary "@${PAYLOAD_FILE}" \
  "http://${API_ADDR}${WEBHOOK_PATH}"

echo ""

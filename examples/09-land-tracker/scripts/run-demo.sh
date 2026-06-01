#!/usr/bin/env bash
# 09-land-tracker demo
#
# Fetches real property pages from land.com, compares to a pre-loaded stale
# baseline, and (optionally) sends a change alert via Telegram.
#
# Usage:
#   bash scripts/run-demo.sh                # run tracker, print report
#   SEND_TELEGRAM=1 bash scripts/run-demo.sh  # also notify Telegram on changes
set -euo pipefail

EX_DIR="$(cd "$(dirname "$0")/.." && pwd)"
ROOT_DIR="$(cd "${EX_DIR}/../.." && pwd)"
LEATHER="${ROOT_DIR}/leather"

# shellcheck source=scripts/pretty.sh
source "${EX_DIR}/scripts/pretty.sh"
# shellcheck source=../../scripts/preflight.sh
source "${EX_DIR}/../scripts/preflight.sh"

lth_mode_banner "09"

# --- Telegram guard -----------------------------------------------------------
if [[ "${SEND_TELEGRAM:-0}" == "1" ]]; then
  if [[ -z "${LEATHER_TELEGRAM_BOT_TOKEN:-}" ]]; then
    if ! command -v pass >/dev/null 2>&1 || ! pass show telegram/YOUR_BOT >/dev/null 2>&1; then
      lth_step "error" "SEND_TELEGRAM=1 but no token is available"
      lth_cont "Set LEATHER_TELEGRAM_BOT_TOKEN or configure: pass insert telegram/YOUR_BOT"
      lth_cont "Also set chat_id in config.yaml."
      exit 1
    fi
  fi
fi

# --- Prepare state dir --------------------------------------------------------
mkdir -p "${EX_DIR}/.state"

# Copy URL list to runtime location.
cp "${EX_DIR}/sample/properties.txt" "${EX_DIR}/.state/properties.txt"
URL_COUNT=$(grep -v '^[[:space:]]*#' "${EX_DIR}/.state/properties.txt" | grep -v '^[[:space:]]*$' | wc -l | tr -d ' ')
lth_step "setup" "tracking ${URL_COUNT} properties from .state/properties.txt"

# Load stale demo baseline so the tracker has something to diff against.
# The demo-state.json contains plausible-but-likely-stale prices from early 2025.
# When the tracker fetches current pages, any price or status changes are flagged.
cp "${EX_DIR}/sample/demo-state.json" "${EX_DIR}/.state/property-state.json"
lth_step "setup" "loaded stale baseline from sample/demo-state.json (Jan 2025 prices)"
lth_cont "tracker will compare live pages to this baseline and flag any differences"

# --- Run tracker --------------------------------------------------------------
lth_step "run" "land-tracker  fetching live pages from land.com..."
cd "${EX_DIR}" && "${LEATHER}" run \
  --config config.yaml \
  --mcp-servers-file mcp-servers.yaml \
  --pretty --stats \
  --api \
  agents/land-tracker.agent.md

lth_step "done" "report complete — updated state in .state/property-state.json"
lth_step "done" "run history in .state/runs/"
lth_cont ""
lth_cont "Schedule for continuous monitoring:"
lth_cont "  leather serve --config config.yaml --mcp-servers-file mcp-servers.yaml"
lth_cont "  demo cadence: land-tracker.lifecycle.yaml (4 fetches/hour, 15-min window)"
lth_cont "  prod cadence: land-tracker-long.lifecycle.yaml (4 fetches/day, random times)"

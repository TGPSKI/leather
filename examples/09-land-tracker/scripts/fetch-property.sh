#!/usr/bin/env bash
# fetch-property.sh — extract structured data from a property listing page.
# Called by shell-mcp as: bash scripts/fetch-property.sh <url>
#
# Uses a two-step session approach: prime a cookie from the site's homepage
# first, then fetch the property page with that session + Referer header.
# This mimics a real browser navigation and bypasses Akamai bot-detection on
# land.com and other CDN-protected real estate listing sites.
set -euo pipefail

URL="$1"

# Dry mode: short-circuit to a deterministic fixture so demo runs never hit
# real listing sites (which rate-limit / bot-block aggressively). Live mode
# requires no auth, just outbound network access — but `make 09` runs in dry
# mode by default; opt in with `make 09-live`.
if [ "${LEATHER_DEMO_MODE:-dry}" != "live" ]; then
  EX_DIR="$(cd "$(dirname "$0")/.." && pwd)"
  fixture="${EX_DIR}/sample/dry/property.txt"
  if [ -f "$fixture" ]; then
    # Substitute the URL into the fixture so downstream agents see a
    # plausible URL line in their output.
    sed "s|{{URL}}|${URL}|g" "$fixture"
    exit 0
  fi
  printf 'ERROR: dry-mode fixture missing at %s\n' "$fixture" >&2
  exit 1
fi

DOMAIN=$(printf '%s' "$URL" | grep -oP '^https?://[^/]+')
COOKIE_JAR=$(mktemp)
trap 'rm -f "$COOKIE_JAR"' EXIT

COMMON_HEADERS=(
  -A 'Mozilla/5.0 (X11; Linux x86_64; rv:125.0) Gecko/20100101 Firefox/125.0'
  -H 'Accept: text/html,application/xhtml+xml,application/xml;q=0.9,image/avif,image/webp,*/*;q=0.8'
  -H 'Accept-Language: en-US,en;q=0.5'
  -H 'Accept-Encoding: gzip, deflate, br'
  -H 'DNT: 1'
  -H 'Connection: keep-alive'
  -H 'Upgrade-Insecure-Requests: 1'
)

_fetch_page() {
  local url="$1"
  # Step 1: prime session cookies from the site homepage.
  curl -L --max-time 15 -s --http2 --compressed \
    "${COMMON_HEADERS[@]}" \
    -H 'Sec-Fetch-Dest: document' \
    -H 'Sec-Fetch-Mode: navigate' \
    -H 'Sec-Fetch-Site: none' \
    -c "$COOKIE_JAR" \
    "$DOMAIN/" -o /dev/null || true

  # Brief pause to simulate user reading the homepage before navigating.
  sleep 2

  # Step 2: fetch the property page using the primed session.
  curl -L --max-time 30 -s --http2 --compressed \
    "${COMMON_HEADERS[@]}" \
    -H 'Sec-Fetch-Dest: document' \
    -H 'Sec-Fetch-Mode: navigate' \
    -H 'Sec-Fetch-Site: same-origin' \
    -H 'Sec-Fetch-User: ?1' \
    -H "Referer: $DOMAIN/" \
    -b "$COOKIE_JAR" -c "$COOKIE_JAR" \
    "$url"
}

HTML=$(_fetch_page "$URL")

# Retry once with a longer back-off if Akamai returned an Access Denied page.
if printf '%s' "$HTML" | grep -q 'Access Denied\|Reference #'; then
  sleep 5
  rm -f "$COOKIE_JAR"
  COOKIE_JAR=$(mktemp)
  HTML=$(_fetch_page "$URL")
fi

if printf '%s' "$HTML" | grep -q 'Access Denied\|Reference #'; then
  echo "ERROR: fetch blocked for $URL" >&2
  echo "fetch_error: blocked_by_cdn"
  exit 0
fi

# --- Address / title ----------------------------------------------------------
echo "=TITLE="
printf '%s\n' "$HTML" | grep -o '<title[^>]*>[^<]*</title>' | sed 's/<[^>]*>//g' | head -1

# --- Open Graph metadata (description, og:title) ------------------------------
echo "=META="
printf '%s\n' "$HTML" \
  | grep -Eo '<meta[^>]+>' \
  | grep -Ei '(og:title|og:description)' \
  | grep -o 'content="[^"]*"' \
  | sed 's/content="//;s/"$//' \
  | head -4

# --- Structured listing data from embedded serverState JSON -------------------
# land.com embeds React serverState as window.serverState = "..." on the page.
# We extract the key fields: price (USD integer), marketStatus, and acreage.
# marketStatus: 1=For Sale  2=Pending  3=Under Contract  4=Sold  5=Off Market
echo "=LISTING_DATA="
printf '%s\n' "$HTML" \
  | grep -oP '"price\\\":\d+|"acres\\\":[0-9.]+|"marketStatus\\\":\d+|"streetAddress\\\":\\\"[^\\\"]+\\\"|"addressLocality\\\":\\\"[^\\\"]+\\\"' \
  | head -10

# Most recent listing event (latest status event title and date)
echo "=LATEST_EVENT="
printf '%s\n' "$HTML" \
  | grep -oP '"listingEvents\\\"\:\[(\{[^\]]+\})' \
  | grep -oP '"date\\\":\\\"[^\\\"]+\\\"|"eventTitle\\\":\\\"[^\\\"]+\\\"' \
  | head -4

# --- Fallback: visible text status keywords -----------------------------------
echo "=STATUS_KEYWORDS="
printf '%s\n' "$HTML" \
  | sed 's/window\.serverState[^<]*/SERVERSTATE_OMITTED/g' \
  | sed 's/<[^>]*>//g' \
  | tr -s ' \t' ' ' \
  | grep -v '^[[:space:]]*$' \
  | grep -Ei '(for sale|\bpending\b|under contract|\bsold\b|price reduced|pre.?market|cancel pending|off market|\$[0-9,]+ [•·] [0-9.]+ acres?)' \
  | head -10

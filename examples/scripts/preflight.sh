#!/usr/bin/env bash
# preflight.sh — sourceable helpers for examples that toggle dry vs live mode.
#
# Examples that hit real external APIs (GitHub, real estate sites, arbitrary
# URLs) source this file from their run-demo.sh to:
#   1. Announce the current mode banner.
#   2. In live mode, fail fast with actionable setup help when required
#      env vars / CLI tools / auth are missing.
#
# Mode is controlled by the LEATHER_DEMO_MODE env var (defaults to "dry").
# `make NN` targets export LEATHER_DEMO_MODE=dry so plain demos never hit
# external APIs and never burn quotas. `make NN-live` runs without that export
# so users opt in to real API calls.
#
# The same env var is read by each example's shell-tools.json command lines,
# which `cat sample/dry/*` fixtures instead of invoking gh/curl/openssl when
# the mode is "dry".

# Resolve mode once. Default: dry. Anything other than literal "live" is dry.
lth_demo_mode() {
  case "${LEATHER_DEMO_MODE:-dry}" in
    live|LIVE) echo live ;;
    *)         echo dry ;;
  esac
}

# Print a one-line mode banner. Pass example name as $1.
lth_mode_banner() {
  local example="${1:-example}"
  local mode
  mode=$(lth_demo_mode)
  if [ "$mode" = "live" ]; then
    printf '  mode: \033[33mlive\033[0m  (real API calls; counts against quotas)\n'
  else
    printf '  mode: \033[32mdry\033[0m   (mocked outbound calls; safe to run repeatedly)\n'
    printf '         export LEATHER_DEMO_MODE=live or run `make %s-live` to hit real APIs\n' "$example"
  fi
}

# Require env vars to be non-empty. Prints a help block and exits 2 if any
# are missing. Usage: lth_require_envs VAR1 VAR2 ...
lth_require_envs() {
  local missing=()
  local v
  for v in "$@"; do
    if [ -z "${!v:-}" ]; then missing+=("$v"); fi
  done
  if [ "${#missing[@]}" -gt 0 ]; then
    printf '\n\033[31mmissing required environment variable(s) for live mode:\033[0m\n' >&2
    for v in "${missing[@]}"; do printf '  - %s\n' "$v" >&2; done
    printf '\nset them in your shell or in examples/.env, then re-run.\n' >&2
    return 2
  fi
}

# Require a CLI command to be on PATH. Usage: lth_require_cli gh "gh auth login (https://cli.github.com)"
lth_require_cli() {
  local cmd="$1"
  local hint="${2:-install $cmd}"
  if ! command -v "$cmd" >/dev/null 2>&1; then
    printf '\n\033[31mmissing required CLI for live mode:\033[0m %s\n' "$cmd" >&2
    printf '  %s\n' "$hint" >&2
    return 2
  fi
}

# Require gh to be authenticated. Returns 2 with help text if not.
lth_require_gh_auth() {
  lth_require_cli gh "install GitHub CLI: https://cli.github.com  then run: gh auth login" || return 2
  if ! gh auth status >/dev/null 2>&1; then
    printf '\n\033[31mgh CLI is installed but not authenticated.\033[0m\n' >&2
    printf '  run: gh auth login\n' >&2
    return 2
  fi
}

# Print a multi-line help block listing what live mode needs for this example.
# Usage: lth_live_requirements "title" "line1" "line2" ...
lth_live_requirements() {
  local title="$1"; shift
  printf '\nLive mode for %s requires:\n' "$title" >&2
  local line
  for line in "$@"; do printf '  • %s\n' "$line" >&2; done
  printf '\n' >&2
}

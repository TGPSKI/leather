#!/usr/bin/env bash
# pretty.sh — shell helpers matching leather's --pretty output style.
# Source this file; do not execute directly.
#   source "$(dirname "$0")/pretty.sh"
#
# Spacing mirrors Go's prettyWriteBlock (prettyLabelWidth=9):
#   line 1: "  [HH:MM:SS] <9-char-label>  content"
#   cont:   "             <dim ┆ label>  content"

# lth_step LABEL MESSAGE — print one log line aligned with leather's pretty output.
# LABEL is right-aligned in a 9-char field (matches prettyLabelWidth=9).
lth_step() {
    local label="${1:-}" msg="${2:-}" ts
    ts="$(date '+%H:%M:%S')"
    printf '  \033[2m[%s]\033[0m \033[1;36m%9s\033[0m  %s\n' "$ts" "$label" "$msg"
}

# lth_cont MESSAGE — continuation line with the ┆ rail (mirrors Go's railLabel).
lth_cont() {
    # 2 spaces + 10-char blank timestamp + 1 space + dim(8 spaces + ┆) + 2 spaces
    printf '             \033[2m        ┆\033[0m  %s\n' "${1:-}"
}

# lth_dim TEXT — dim/low-intensity styling.
lth_dim()   { printf '\033[2m%s\033[0m'    "$1"; }

# lth_cyan TEXT — bold cyan (used for curing/job names).
lth_cyan()  { printf '\033[1;36m%s\033[0m' "$1"; }

# lth_green TEXT — green (used for intake events).
lth_green() { printf '\033[32m%s\033[0m'   "$1"; }

# lth_json_get JSON KEY — extract a string value from a flat JSON object (no jq needed).
lth_json_get() {
    printf '%s' "$1" | grep -o "\"${2}\":\"[^\"]*\"" | head -1 | cut -d'"' -f4
}

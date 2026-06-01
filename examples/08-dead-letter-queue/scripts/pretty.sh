#!/usr/bin/env bash
# pretty.sh — shell helpers matching leather's --pretty output style.
# Source this file; do not execute directly.

lth_step() {
    local label="${1:-}" msg="${2:-}" ts
    ts="$(date '+%H:%M:%S')"
    printf '  \033[2m[%s]\033[0m \033[1;36m%9s\033[0m  %s\n' "$ts" "$label" "$msg"
}

lth_cont() {
    printf '             \033[2m        ┆\033[0m  %s\n' "${1:-}"
}

lth_dim() { printf '\033[2m%s\033[0m' "$1"; }

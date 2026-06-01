#!/usr/bin/env bash
# run.sh — run the agents-doc-lifecycle Go tool from any directory.
#
# Usage (from the repository root):
#   bash .agents/skills/agents-doc-lifecycle/scripts/run.sh sync [--fix]
#   bash .agents/skills/agents-doc-lifecycle/scripts/run.sh audit
#   bash .agents/skills/agents-doc-lifecycle/scripts/run.sh check
#
# The script cd-s into the scripts/ directory so that `go run .` resolves the
# correct go.mod (agents-doc-lifecycle module, not the outer leather module).
# The Go tool auto-discovers the repository root before executing any file I/O.

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
cd "$SCRIPT_DIR"
exec go run . "$@"

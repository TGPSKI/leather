#!/usr/bin/env bash
# scripts/run.sh — leather code-quality-lifecycle runner
#
# Usage (from repository root):
#   bash .agents/skills/code-quality-lifecycle/scripts/run.sh <command>
#
# Commands:
#   audit        Full quality check: fmt + vet + lint + test-race + cover gaps + split-check
#   fmt-check    Check formatting; exit non-zero if any files are unformatted
#   fmt-fix      Auto-fix formatting with gofmt -w
#   cover        Generate coverage report and print exported symbols at 0%
#   gaps         Print only the exported symbols at 0% coverage (subset of cover)
#   split-check  List test files over 500 lines, sorted by size
#   help         Print this message

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/../../../.." && pwd)"

cd "$REPO_ROOT"

GO_FILES_EXPR='$(find . -name "*.go" -not -path "./.agents/*")'

# ── helpers ──────────────────────────────────────────────────────────────────

pass() { printf '\033[1;32mPASS\033[0m  %s\n' "$*"; }
fail() { printf '\033[1;31mFAIL\033[0m  %s\n' "$*"; }
info() { printf '\033[2m      %s\033[0m\n' "$*"; }
header() { printf '\n\033[1m%s\033[0m\n' "$*"; }

# ── commands ─────────────────────────────────────────────────────────────────

cmd_fmt_check() {
    header "Formatting (gofmt)"
    unformatted=$(gofmt -l $(find . -name '*.go' -not -path './.agents/*') 2>/dev/null) || true
    if [ -z "$unformatted" ]; then
        pass "all .go files are formatted"
        return 0
    else
        fail "unformatted files:"
        echo "$unformatted" | while IFS= read -r f; do info "$f"; done
        return 1
    fi
}

cmd_fmt_fix() {
    header "Fixing formatting (gofmt -w)"
    gofmt -w $(find . -name '*.go' -not -path './.agents/*')
    pass "gofmt -w applied"
    cmd_fmt_check
}

cmd_cover() {
    header "Coverage report"
    go test -coverprofile=coverage.out ./... 2>/dev/null
    echo ""
    info "Per-function coverage (sorted, lowest first):"
    go tool cover -func=coverage.out | sort -t'%' -k1 -n
    echo ""
    cmd_gaps
}

cmd_gaps() {
    header "Exported symbols at 0% coverage"

    if [ ! -f coverage.out ]; then
        info "No coverage.out found; running go test -coverprofile=coverage.out ./..."
        go test -coverprofile=coverage.out ./... 2>/dev/null
    fi

    # Match lines where the last field is "0.0%" and the symbol starts with
    # an uppercase letter (exported). The func name follows the last dot.
    gaps=$(go tool cover -func=coverage.out \
        | awk '$NF == "0.0%" { split($1, parts, ":"); fn=$2; gsub(/\(.*\)/, "", fn); split(fn, fparts, "."); sym=fparts[length(fparts)]; if (sym ~ /^[A-Z]/) print $0 }')

    if [ -z "$gaps" ]; then
        pass "no exported symbols at 0% coverage"
    else
        fail "exported symbols with no test coverage:"
        echo "$gaps" | while IFS= read -r line; do info "$line"; done
        echo ""
        info "Action: add tests for each symbol above (see Operation 5 in SKILL.md)"
        return 1
    fi
}

cmd_split_check() {
    header "Test file sizes (candidates for splitting)"

    results=$(find . -name '*_test.go' -not -path './.agents/*' \
        | xargs wc -l 2>/dev/null \
        | grep -v ' total$' \
        | sort -rn)

    over_threshold=$(echo "$results" | awk '$1 > 500 {print}')

    if [ -z "$over_threshold" ]; then
        pass "no test files exceed 500 lines"
    else
        fail "test files over 500 lines (consider splitting):"
        echo "$over_threshold" | while IFS= read -r line; do info "$line"; done
        info "Action: see Operation 6 in SKILL.md for split procedure"
    fi

    echo ""
    info "Top 10 largest test files:"
    echo "$results" | head -10 | while IFS= read -r line; do info "$line"; done
}

cmd_audit() {
    header "leather code-quality-lifecycle: full audit"
    echo ""

    overall=0

    # 1. Formatting
    cmd_fmt_check || overall=1
    echo ""

    # 2. go vet
    header "Static analysis (go vet)"
    if go vet ./... 2>&1; then
        pass "go vet clean"
    else
        fail "go vet reported issues"
        overall=1
    fi
    echo ""

    # 3. Lint
    header "Lint (golangci-lint)"
    if command -v golangci-lint >/dev/null 2>&1; then
        if golangci-lint run 2>&1; then
            pass "golangci-lint clean"
        else
            fail "golangci-lint reported issues"
            overall=1
        fi
    else
        info "golangci-lint not found; skipping (install to enable)"
    fi
    echo ""

    # 4. Tests with race detector
    header "Tests with race detector (go test -race ./...)"
    if go test -race ./... 2>&1; then
        pass "all tests pass under -race"
    else
        fail "test failures detected"
        overall=1
    fi
    echo ""

    # 5. Integration tests (if build tag present)
    header "Integration tests (go test -tags integration ./cmd/leather/...)"
    if go test -tags integration -race ./cmd/leather/... 2>&1; then
        pass "integration tests pass"
    else
        fail "integration test failures detected"
        overall=1
    fi
    echo ""

    # 6. Coverage gaps
    header "Coverage (go test -coverprofile)"
    go test -coverprofile=coverage.out ./... 2>/dev/null
    total=$(go tool cover -func=coverage.out | grep '^total:' | awk '{print $3}')
    info "project total coverage: $total"
    cmd_gaps || overall=1
    echo ""

    # 7. Split check
    cmd_split_check
    echo ""

    # Summary
    header "Audit summary"
    if [ "$overall" -eq 0 ]; then
        pass "all checks passed — ready for PR"
    else
        fail "one or more checks failed — see output above"
        exit 1
    fi
}

cmd_help() {
    sed -n '2,14p' "$SCRIPT_DIR/run.sh" | sed 's/^#[[:space:]]*//'
}

# ── dispatch ─────────────────────────────────────────────────────────────────

command="${1:-help}"

case "$command" in
    audit)       cmd_audit ;;
    fmt-check)   cmd_fmt_check ;;
    fmt-fix)     cmd_fmt_fix ;;
    cover)       cmd_cover ;;
    gaps)        cmd_gaps ;;
    split-check) cmd_split_check ;;
    help|--help|-h) cmd_help ;;
    *)
        printf 'unknown command: %s\n' "$command" >&2
        cmd_help >&2
        exit 1
        ;;
esac

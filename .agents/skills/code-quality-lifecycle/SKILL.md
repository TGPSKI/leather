---
name: code-quality-lifecycle
description: "Audit and improve test coverage, formatting, linting, and test organization for the leather codebase. USE FOR: finding untested exported functions; identifying test files that should be split; checking and fixing gofmt/golangci-lint issues; producing a coverage gap report; catching benchmark regressions; full quality gate pre-PR. DO NOT USE FOR: writing business logic; documentation (use documentation-lifecycle); agents doc sync (use agents-doc-lifecycle)."
compatibility: Requires Go toolchain and golangci-lint. benchstat is optional (needed for bench-compare). Designed for GitHub Copilot and similar coding agents.
metadata:
  argument-hint: 'What quality operation? e.g. "full audit", "find untested methods in internal/session", "fix lint", "coverage gaps", "split cmd_chat_test.go", "benchmark regression"'
  user-invocable: "true"
---

# code-quality-lifecycle

Keeps the leather codebase clean, well-tested, and consistently formatted.
The skill detects drift between implementation and test coverage, identifies
test files that have grown too large, and orchestrates the full quality gate.

**Scope boundary:** This skill does not touch `AGENTS.md`, `.subagents/`, or
`docs/`. Those are owned by `agents-doc-lifecycle` and `documentation-lifecycle`.

---

## When to use this skill

- Before opening a PR: run the full quality audit (Operation 1)
- After adding a new exported function or type: check that it has test coverage
- After a significant package addition: check if test files need splitting
- After suspected formatting drift: fix formatting (Operation 2)
- After a performance optimization: compare benchmarks (Operation 7)
- When `make ci` fails: triage and fix the failing gate

## Executable tool

The skill ships a bash runner at [`scripts/run.sh`](scripts/run.sh) that
orchestrates the checks and produces a structured report. Run it from the
repository root:

```bash
# Full audit: all checks, structured report
bash .agents/skills/code-quality-lifecycle/scripts/run.sh audit

# Coverage report with gap identification
bash .agents/skills/code-quality-lifecycle/scripts/run.sh cover

# Find test files over 500 lines
bash .agents/skills/code-quality-lifecycle/scripts/run.sh split-check

# Run gofmt check only
bash .agents/skills/code-quality-lifecycle/scripts/run.sh fmt-check
```

The script handles mechanical detection. The operations below describe
judgment-based decisions (which tests to write, how to split a test file,
whether a coverage gap is acceptable) that require reading code.

---

## Quality surfaces

| Surface | Tool | Makefile target | Pass threshold |
|---|---|---|---|
| Formatting | `gofmt` | `make check` | Zero unformatted files |
| Static analysis | `go vet` | `make check` | Zero issues |
| Lint | `golangci-lint` | `make lint` | Zero issues |
| Unit tests | `go test ./...` | `make test` | All pass |
| Race detector | `go test -race ./...` | `make test-race` | All pass |
| Integration tests | `go test -tags integration` | `make integration` | All pass |
| Full gate | check + test-race + lint + integration | `make ci` | All of the above pass |
| Coverage | `go test -coverprofile` | `make cover` | No exported symbol at 0% |
| Benchmarks | `benchstat` | `make bench-compare` | No regression > 10% |

---

## Operations

### 1. Full quality audit

Run before a significant PR or after a multi-file change:

```bash
# Full gate
make ci

# Coverage report (text form for agent consumption)
go test -coverprofile=coverage.out ./...
go tool cover -func=coverage.out
```

Produce an **audit report** covering:

| Check | Pass / Fail | Notes |
|---|---|---|
| `gofmt` | | |
| `go vet` | | |
| `golangci-lint` | | |
| `go test ./...` | | |
| `go test -race ./...` | | |
| `go test -tags integration ./cmd/leather/...` | | |
| Exported symbols at 0% coverage | | |
| Test files > 500 lines | | |
| Benchmarks regressed > 10% | | |

Work through failures in this order: formatting → vet → lint → test failures
→ race failures → coverage gaps. Do not move to the next category while the
previous has open issues.

---

### 2. Fix formatting

```bash
# Identify unformatted files
gofmt -l $(find . -name '*.go' -not -path './.agents/*')

# Auto-fix in place
gofmt -w $(find . -name '*.go' -not -path './.agents/*')

# Verify clean
make check
```

`gofmt -w` rewrites only files whose content changes and preserves
permissions. It is safe to run repeatedly. Never apply it to `.agents/`
scripts (they are outside the Go module).

---

### 3. Fix lint issues

```bash
make lint
```

Read each issue and fix at the source. Common patterns:

| Linter | Issue pattern | Fix |
|---|---|---|
| `errcheck` | unchecked error return | Assign to `_` explicitly or handle the error |
| `unused` | unexported symbol never referenced | Delete it, or export it if it should be API |
| `staticcheck SA` | always-true/dead condition | Remove the dead branch |
| `govet` | `printf`-style format mismatch | Correct the format verb |
| `gosimple` | can simplify | Apply the suggested simplification |
| `goimports` | import grouping wrong | Run `goimports -w <file>` |

Do not add `//nolint:` suppressions without an explaining code comment. If
a suppression is genuinely necessary, add a follow-up task to revisit it.

---

### 4. Coverage analysis and gap identification

```bash
go test -coverprofile=coverage.out ./...
go tool cover -func=coverage.out | sort -t'%' -k1 -n
```

Focus on exported symbols at `0.0%`:

```bash
# Exported functions at 0% — uppercase after the last slash or dot
go tool cover -func=coverage.out | awk '$3 == "0.0%" && /\.[A-Z]/'
```

For a single package with precise per-package coverage:

```bash
go test -coverprofile=coverage.out \
    -coverpkg=./internal/session/... \
    ./internal/session/...
go tool cover -func=coverage.out | grep -v '100.0%'
```

**Priority order for closing gaps:**
1. Exported functions/methods at 0% — highest risk
2. Functions returning `error` without an error-path test
3. Internal helpers at low coverage — lower priority

---

### 5. Find and test untested exported symbols

After identifying gaps from Operation 4:

1. Read the source file containing the uncovered function.
2. Read the existing `<pkg>_test.go` for the package to understand style.
3. Write a table-driven test that exercises the success path and at least
   one error path (for functions that return `error`).
4. Add the test to the existing `_test.go` file, or create a new `_test.go`
   if the file would exceed 500 lines.
5. Re-run coverage to confirm the gap is closed.

**Test placement rules (leather conventions):**
- Tests live colocated with code: `internal/<pkg>/<pkg>_test.go`
- Use `t.TempDir()` for all file I/O — never write to the working directory
- Use `t.Helper()` in every test helper function
- Use `MockLLM` (`internal/session/mock_llm.go`) for all LLM-touching tests;
  never call a real model endpoint
- Table-driven: `t.Run(tc.name, func(t *testing.T) { ... })` for multi-case
- Name test functions `Test<Symbol>` or `Test<Symbol>_<Scenario>`
- No package-level state mutations in tests

---

### 6. Identify and split oversized test files

Test files over ~500 lines often contain too many unrelated responsibilities.

**Detection:**

```bash
find . -name '*_test.go' -not -path './.agents/*' \
  | xargs wc -l | sort -rn | head -20
```

**Split heuristics:**
- A file with > 500 lines or > 10 unrelated `Test*` functions is a candidate
- Split on production-code boundary (exported type or subsystem), not line count alone
- If you cannot describe the split in one sentence per file, the boundary is wrong

**Split procedure:**

1. Read the oversized test file in full.
2. Group `Test*` functions by the production symbol they exercise:
   - One group per exported type or subsystem → `<type>_test.go`
   - Integration-style end-to-end tests → `<pkg>_integration_test.go`
   - Shared test helpers → `testhelpers_test.go`
3. Create the new file(s) with the correct package declaration.
4. Move the relevant `Test*` functions; do not move `TestMain`.
5. Run `make test-race` to verify all tests still pass.
6. Remove the moved functions from the original file.

**Fixed rules:**
- `TestMain` stays in the primary `<pkg>_test.go`
- Shared helper functions go in `testhelpers_test.go` in the same package
- Never split a table-driven test across files — keep the table and the
  loop in the same function

---

### 7. Benchmark regression check

Run after performance-sensitive changes:

```bash
# Save baseline before change
make bench-save

# After making changes, compare
make bench-compare
```

Investigate regressions > 10% in `ns/op` or `allocs/op`. Common causes:

| Symptom | Likely cause |
|---|---|
| Higher `ns/op`, same `allocs/op` | Lock contention or added O(n) work |
| Higher `allocs/op` | New allocation in hot path (check `b.ReportAllocs()`) |
| Both higher | Larger input propagation or algorithmic regression |

Benchmarks live in `cmd/leather/bench_test.go`. Add a new benchmark whenever
a codepath processes significant volume (token counting, cron parsing, agent
loading, session compaction).

Benchmark function template:

```go
func Benchmark<Symbol>(b *testing.B) {
    // --- setup (not timed) ---
    b.ResetTimer()
    for i := 0; i < b.N; i++ {
        // exercise the hot path exactly once
    }
}
```

---

## Heuristics

| Signal | Threshold | Action |
|---|---|---|
| Test file line count | > 500 lines | Evaluate split (Operation 6) |
| `Test*` functions in one file | > 10 unrelated functions | Evaluate split |
| Exported symbol coverage | 0% | Write tests (Operation 5) |
| Package aggregate coverage | < 70% | Investigate gaps (Operation 4) |
| Benchmark regression | > 10% `ns/op` or `allocs/op` | Profile before merging |
| Lint issues | Any | Fix before merging; no `//nolint` without explanation |

---

## Guardrails

- Do not edit `.go` source files to work around lint; fix the root cause.
- Do not add `//nolint:` suppressions without an explaining code comment.
- Do not skip the race detector. All tests must pass under `go test -race ./...`.
- Do not write tests that call real LLM endpoints; use `MockLLM`.
- Do not write tests that mutate package-level state.
- Do not close a coverage gap with a trivial test that does not actually
  exercise the logic (e.g., only checking that the function does not panic).
- `make ci` is the gate — do not declare work done until it passes cleanly.
- `go.mod` must have zero `require` entries after any change; verify with
  `cat go.mod`.

---

## Lifecycle operations reference

| Operation | Trigger | Action |
|---|---|---|
| Full audit | Before PR or release | `make ci`; produce audit report table |
| Fix formatting | `make check` reports unformatted files | `gofmt -w`; re-run `make check` |
| Fix lint | `make lint` reports issues | Fix at source; no `//nolint` without comment |
| Close coverage gap | Exported symbol at 0% | Add table-driven tests; re-run cover |
| Split test file | > 500 lines or > 10 unrelated tests | Group by production boundary; split |
| Add benchmark | New hot-path codepath added | Add `Benchmark*` to `cmd/leather/bench_test.go` |
| Benchmark regression | `make bench-compare` shows > 10% | Profile; fix; re-compare |
| Dependency check | New import added | Verify `go.mod` still has zero `require` entries |
| Integration gate | New CLI subcommand or contract | Add `//go:build integration` test in `cmd/leather/` |

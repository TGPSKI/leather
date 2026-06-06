# AGENTS-QUALITY.md — leather tests, build, linting, CI

Subagent guide for the quality domain: test organization, benchmark patterns,
the Makefile, CI pipelines, and linting.

Load this guide when working on `*_test.go`, `Makefile`, or
`.github/workflows`. For the structured-logging API surface (`internal/logging`),
log levels, and status/metrics endpoints, see
[AGENTS-OBSERVABILITY.md](AGENTS-OBSERVABILITY.md). For core runtime types, see
[AGENTS-CORE.md](AGENTS-CORE.md). For serving and config, see
[AGENTS-SERVE.md](AGENTS-SERVE.md). For the **benchmark catalog, allocation
budgets, baseline policy, and pprof workflow**, see
[AGENTS-PERFORMANCE.md](AGENTS-PERFORMANCE.md). For deployment-side
concerns surfaced by tests (file modes, lock, upgrade), see
[AGENTS-OPERATIONS.md](AGENTS-OPERATIONS.md). For trust-boundary tests
(prompt-injection, secret-leak, API auth), see
[AGENTS-SECURITY.md](AGENTS-SECURITY.md).

---

## Build targets

```bash
make build          # go build -o ./leather ./cmd/leather
make test           # go test ./... (unit tests)
make test-race      # go test -race ./...
make check          # gofmt -l (fail on diff) + go vet ./...
make lint           # golangci-lint run
make ci             # check + test-race + lint + integration (full gate; must pass before merge)
make bench          # go test -bench=. -benchmem -benchtime=3s ./...
make bench-save     # save bench output to bench-baseline.txt
make bench-compare  # benchstat bench-baseline.txt current output
make cover          # go test -coverprofile=coverage.out ./... && go tool cover -html=coverage.out
make clean          # remove leather binary and coverage artifacts
```

`make ci` is the gate. Every PR must pass it. Run it locally before pushing.

---

## Test organization

| Test type | Location | Build tag | Makefile target |
|---|---|---|---|
| Unit tests | Colocated with code (`internal/<pkg>/<pkg>_test.go`) | none | `make test` |
| Integration tests | `cmd/leather/integration_cli_test.go` | none | `make test` |
| Benchmarks | `cmd/leather/bench_test.go` | none | `make bench` |

### Unit test conventions

- **Table-driven.** Use `t.Run` with named sub-tests for functions with
  multiple input/output cases.
- **`t.TempDir()` for all file I/O.** Never write to the working directory or
  a hardcoded temp path in tests.
- **`t.Helper()` in every test helper.** Ensures failure lines point to the
  call site, not the helper body.
- **`MockLLM` for all LLM-touching tests.** Never call a real model endpoint
  in tests. `MockLLM` in `internal/session/mock_llm.go` is the canonical
  test double.
- **Error path coverage.** Every function that returns an error must have at
  least one test exercising the error path.
- **No package-level state mutations.** Tests must not set package-level vars
  or modify global config. Use constructor parameters or functional options.

### Race detection

All tests must pass under `-race`. CI always runs `go test -race ./...`.
Shared mutable state must be protected by `sync.Mutex`, `sync.RWMutex`, or
channels. Never use package-level mutable variables without synchronization.

---

## Benchmark patterns

Benchmarks live in `cmd/leather/bench_test.go`. Add a benchmark whenever a
new codepath processes a meaningful volume of data (token counting, cron
scheduling, agent loading).

Example pattern:

```go
func BenchmarkSessionAdd(b *testing.B) {
    budget := model.TokenBudget{MaxTokens: 8192, CompletionReserve: 1024, SummarizeThreshold: 0.85}
    client := session.NewMockLLM(session.MockConfig{TokensPerMessage: 10})
    s := session.New(budget, "test-model", client)
    msg := model.Message{Role: "user", Content: "benchmark message"}
    b.ResetTimer()
    for i := 0; i < b.N; i++ {
        _ = s.Add(context.Background(), msg)
    }
}
```

Prefer `b.ResetTimer()` after setup. Use `-benchmem` to catch unexpected
allocations. Run `make bench-compare` after optimizations to verify
improvement.

---

## Linting

`make lint` runs `golangci-lint`. The `.golangci.yml` at the root enables:

- `govet` — catches subtle bugs
- `staticcheck` — advanced static analysis
- `errcheck` — all errors must be checked
- `gosimple` — flag simplifiable code
- `unused` — flag unused exports
- `gofmt` — formatting (fail on diff)
- `goimports` — import grouping

Lint must pass with zero issues before merge. Do not disable linters inline
(`//nolint:...`) without a code comment explaining why.

---

## `internal/logging`

Structured logging package. Zero external deps. Wraps `log/slog` (stdlib,
Go 1.21+) with leather-specific conventions.

Key exported surfaces:

```go
// New returns a Logger configured for the given component name and level.
func New(component string, level model.LogLevel) *Logger

// Logger wraps slog.Logger with component-scoped methods.
type Logger struct { ... }

// Methods: Debug, Info, Warn, Error — all accept a message and key-value pairs.
// Example: log.Info("job started", "agent", job.Name, "next_run", job.NextRun)
```

Logging conventions:

- Always include `"agent"` or `"component"` as the first key-value pair.
- Include `"job_id"` for scheduler events; `"session_id"` for session events.
- Never log message content, prompt text, or token values from user data.
- Use `Debug` for per-message events; `Info` for state transitions; `Warn`
  for recoverable errors; `Error` for failures that propagate to the caller.
- Log format (`text` or `json`) is configured via `--log-format`; default is
  `text` for human readability during development.

---

## Profiling support

CPU and memory profiling are enabled via standard Go runtime flags. No
special harness is needed:

```bash
# CPU profile
go test -cpuprofile=cpu.prof -bench=BenchmarkSessionAdd ./cmd/leather/
go tool pprof -web cpu.prof

# Memory profile
go test -memprofile=mem.prof -bench=BenchmarkSessionAdd ./cmd/leather/
go tool pprof -web mem.prof

# Trace
go test -trace=trace.out -bench=BenchmarkSessionAdd ./cmd/leather/
go tool trace trace.out
```

---

## CI pipeline

`.github/workflows/ci.yml` is currently checked in disabled (`on: {}`). When
re-enabled for push / pull_request on `main`, it installs `golangci-lint`,
runs the local CI gate, and then builds the binary:

```yaml
name: CI

on: {}

jobs:
  ci:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@11bd71901bbe5b1630ceea73d27597364c9af683  # v4.2.2
      - uses: actions/setup-go@f111f3307d8850f501ac008e886eec1fd1932a34  # v5.3.0
        with:
          go-version-file: go.mod
          cache: true
      - uses: golangci/golangci-lint-action@4afd733a84b1f43292c63897423277bb7f4313a9  # v6.5.2
        with:
          install-only: true
      - run: make ci        # check + test-race + lint
        env:
          LEATHER_TEST_BUILD: "1"
      - run: make build     # verify the binary compiles
```

`make ci` (check + test-race + lint) is the recommended local gate before
pushing. CI runs the same steps individually so lint results appear in
separate job steps; `golangci-lint` must be installed locally to run
`make lint` and `make ci`.

---

## Testdata conventions

`testdata/` directories within each package hold fixtures used by tests:

| Path | Contents |
|---|---|
| `internal/agent/testdata/` | Valid and invalid `*.agent.md` files |
| `internal/config/testdata/` | Valid and invalid `config.yaml` files |
| `internal/scheduler/testdata/` | Cron expression cases (valid + invalid) |
| `cmd/leather/testdata/` | Command-level test fixtures |

Fixtures should be minimal — just enough to exercise the code path under
test. Do not commit real API keys or credentials in test fixtures; use
obviously fake values (`FAKE_KEY_000`).

---

## Adding tests for a new package

When a new `internal/<name>` package is created:

1. Create `internal/<name>/<name>_test.go` in the same package (`package <name>`
   for white-box tests, `package <name>_test` for black-box tests).
2. Cover all exported functions with at least one happy-path and one
   error-path test.
3. Use table-driven tests for functions with multiple cases.
4. Add any fixture files to `internal/<name>/testdata/`.
5. Verify `go test ./internal/<name>/...` and `go test -race ./internal/<name>/...`
   both pass.
6. If the package has meaningful computational work, add a benchmark.

---

## Shared stdlib leaf utilities

Four zero-dependency leaf packages hold helpers that were previously
duplicated across the codebase (ROADMAP "Shared library extraction",
issue #3, phase 1). They import only the stdlib (and, for `jsonstore`,
`internal/fileutil`), so any package may depend on them without risking
an import cycle. Prefer these over re-rolling the same logic inline.

| Package | Exports | Use instead of |
|---|---|---|
| `internal/fileutil` | `Exists`, `AtomicWriteFile`, `AtomicWriteFileFunc` | hand-rolled `os.CreateTemp`→`Rename` blocks; `os.Stat`+`IsNotExist` checks |
| `internal/jsonstore` | `Save`, `Load` | `json.Marshal`+atomic write; `os.ReadFile`+`json.Unmarshal` with not-exist handling |
| `internal/ids` | `TimestampHex`, `RandHex` | inline `"<prefix>_<ts>_<hex>"` IDs; `crypto/rand`→`hex` token blocks |
| `internal/yamlx` | `ParseBlock`, `ParseFlat`, `ParseFlatLines`, `StripQuotes`, `SplitKV` | per-package flat-YAML scanners, quote-strippers, and `key: value` splitters |
| `internal/httpx` | `WriteJSON`, `WriteError` | inline `w.Header().Set(…)`+`json.NewEncoder(w).Encode(…)` patterns in HTTP handlers |

`jsonstore.Load` returns `(found bool, err error)`: a missing file is
`(false, nil)`, never an error — callers map `!found` to empty state.
`yamlx` is line-agnostic today; line-number tracking for schema
`file:line` violations is the next step (`TODO(#3)` in the package).

---

## Verification checklist

Before opening a PR:

- [ ] `make ci` passes locally (check + test-race + lint)
- [ ] New disk persistence / ID / flat-YAML code reuses `internal/{fileutil,jsonstore,ids,yamlx}` rather than re-rolling it
- [ ] New HTTP handler responses use `internal/httpx` (`WriteJSON`, `WriteError`) rather than inline `json.NewEncoder(w).Encode` patterns
- [ ] New packages have colocated `_test.go` with coverage for exported API
- [ ] No `//nolint` directives without a comment explaining the exception
- [ ] Benchmarks added for any new hot-path code
- [ ] No hardcoded temp paths in tests; all use `t.TempDir()`
- [ ] No real credentials or secrets in `testdata/`
- [ ] CI workflow action refs are SHA-pinned with version comments

### Outbound tool resilience (retry, DLQ, rate limits)

PRs touching `internal/tool`, `internal/config`, or `internal/cli/cmd_dlq.go`:

- [ ] `go test ./internal/tool/... ./internal/cli/...` passes
- [ ] `go test -race ./internal/tool/... ./internal/cli/...` passes
- [ ] New tools with `retry:` config have tests covering transient retry and permanent no-retry paths
- [ ] `TestExecute_DLQEnqueueOnExhaustion` and `TestExecute_DLQEnqueueOnPermanent` pass
- [ ] `TestHostLimiter_*` suite passes; no real network calls
- [ ] `TestRunDLQ*` suite passes with `t.TempDir()` state dirs
- [ ] `leather dlq inspect` output includes ID, tool name, agent, attempt, error
- [ ] `leather dlq requeue --state-dir ... <item-id>` (item-id **last**) moves item
- [ ] `tools.rate_limits` in config.yaml parses without error; bad spec is warn+disable, not panic
- [ ] `/metrics` response contains `leather_tool_retry_total`, `leather_tool_backoff_total`, `leather_tool_rate_limit_wait_total`, `leather_outbound_dlq_depth`

---

_Last reviewed: 2026-06-05_ 

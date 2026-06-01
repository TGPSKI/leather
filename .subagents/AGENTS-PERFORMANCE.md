# AGENTS-PERFORMANCE.md — leather performance & benchmarks

Subagent guide for **`leather` performance work**: hot-path inventory,
benchmark catalog, allocation budgets, baseline policy, profiling
workflow, and regression triage.

Load this guide when:

- Adding or changing a benchmark
- Investigating a perf regression or running pprof
- Touching code on a documented hot path
- Changing allocation behavior in `internal/session`,
  `internal/runner`, or `internal/queue`
- Updating the CI perf-baseline policy

For the test suite and CI surface, see [AGENTS-QUALITY.md](AGENTS-QUALITY.md).
For runtime architecture, see [AGENTS-RUNTIME.md](AGENTS-RUNTIME.md).
For session token accounting, see [AGENTS-CORE.md](AGENTS-CORE.md).
For deployment-side resource limits, see
[AGENTS-OPERATIONS.md](AGENTS-OPERATIONS.md).

---

## Scope

Cross-cutting. Owns the **performance posture** and the benchmark
catalog; does not own any single package. Performance-affecting code
changes update both this guide and the owning package's guide.

---

## Hot-path inventory

The paths exercised on every run (or every tool round). Changes here
require a benchmark + a pprof check.

| Path | Package | Frequency | Sensitivity |
|---|---|---|---|
| `Session.Add` → token counting | `internal/session` | per turn | High — runs hot during long conversations. |
| `Session.Add` → summarization trigger | `internal/session` | conditional per turn | Medium — LLM call dominates when triggered. |
| Token counter (`CountTokens`) | `internal/session` | per `Add` | High — pure CPU, allocation-sensitive. |
| Tool dispatch (`runner.runTools`) | `internal/runner` | per tool round | Medium — small N per turn. |
| MCP `tools/call` round-trip | `internal/mcp` | per MCP tool call | Medium — IO-bound; allocation-light. |
| Queue dequeue / enqueue | `internal/queue` | per scheduled job | High — file I/O + serialization. |
| Cache lookup / write | `internal/cache` | per run (cache-enabled) | Medium — file I/O + SHA-256. |
| HTTP API `/jobs`, `/runs` | `internal/cli` (serve) | per scrape | Low — JSON serialization. |
| `LoadDir` (agent loading) | `internal/agent` | startup + reload | Low (startup) — but allocations are unbounded by N agents. |

Cold paths are intentionally not listed; if a change starts touching
turn-loop or queue paths, add it here.

---

## Benchmark catalog

Benchmarks live in `cmd/leather/bench_test.go` (cross-package) and
next to the code for package-local ones (`*_bench_test.go`).

| Benchmark | File | Measures |
|---|---|---|
| `BenchmarkCronParse` | `cmd/leather/bench_test.go` | Cron expression parsing. |
| `BenchmarkAgentLoadDir` | `cmd/leather/bench_test.go` | Loading a directory of agents. |
| `BenchmarkSessionAdd` | `cmd/leather/bench_test.go` | `session.Add` cost. |
| `BenchmarkCacheAgentRunKey` | `cmd/leather/bench_test.go` | Cache key derivation. |
| `BenchmarkCacheGetSet` | `cmd/leather/bench_test.go` | Cache get/set round-trip. |
| `BenchmarkToolLoad` | `cmd/leather/bench_test.go` | Tool registry load. |
| `BenchmarkQueueEnqueueDequeue` | `cmd/leather/bench_test.go` | Queue enqueue + dequeue. |
| `BenchmarkPublish` | `internal/devtools/bus/bench_test.go` | Devtools bus publish. |
| `BenchmarkFanout10` | `internal/devtools/bus/bench_test.go` | Devtools bus 10-subscriber fan-out. |

Additional planned benchmarks are tracked in [ROADMAP.md](../ROADMAP.md).
When you add one, update this table in the same PR.

---

## Allocation budgets

Steady-state allocation targets (per operation). Exceeding the budget
fails review unless the PR explains why.

| Operation | Budget (allocs/op) | Notes |
|---|---|---|
| `Session.Add` (short turn, no summarize) | ≤ 6 | Re-uses message-slice backing array. |
| `CountTokens` (8k history) | ≤ 1 | Pure read; should not allocate. |
| Queue dequeue (1 item) | ≤ 20 | JSON decode of one record. |
| Cache hit | ≤ 5 | Includes hash compute (stack-allocatable). |
| Tool-result JSON marshal (small) | ≤ 8 | One round-trip's worth. |

`go test -bench=. -benchmem` is the canonical measurement command.
Numbers above are targets; today's actuals must be captured and
recorded in the next baseline file (see below).

---

## Baseline policy

A perf baseline is a checked-in JSON file capturing
`ns/op`, `allocs/op`, `B/op` for every benchmark on a fixed reference
machine.

### Location

```
docs/performance/baseline-<YYYYMMDD>.json
docs/performance/baseline-latest.json     # symlink to the newest
```

### Update cadence

- After any intentional perf change (improvement or accepted regression).
- After a Go version bump.
- Never as a side effect of fixing a regression — the regression PR
  must call out the baseline shift in its description.

### CI gate (planned)

When the CI perf job lands ([AGENTS-QUALITY.md](AGENTS-QUALITY.md)),
the gate compares `bench` output against `baseline-latest.json` and
fails on `>10%` regression on any tracked metric. Until then, perf is
a code-review concern only.

---

## Profiling workflow (pprof)

### Capturing a CPU profile from a running `leather serve`

```bash
# Enable the API and pprof side-mount (planned flag).
leather serve --api --pprof

# In another shell:
go tool pprof -http=:8080 http://127.0.0.1:7749/debug/pprof/profile?seconds=30
```

### Capturing from a benchmark

```bash
go test -run=^$ -bench=BenchmarkRunner_ToolRound \
    -cpuprofile=cpu.out -memprofile=mem.out ./internal/runner
go tool pprof -http=:8080 cpu.out
go tool pprof -http=:8080 mem.out
```

### What to look at first

1. **Top 10 by `flat`** — the actual hot functions.
2. **Allocations** (`-sample_index=alloc_objects`) — anywhere `Session`
   or `runner` shows up is interesting; anywhere `internal/model` shows
   up is a smell (model is data only).
3. **GC pressure** — if `runtime.gc*` is in the top 10, the allocation
   budget is being violated somewhere upstream.

---

## Regression triage

When a benchmark regresses by >10% versus baseline:

1. **Confirm** — re-run with `-count=10 -benchtime=3s` on a quiet machine.
2. **Bisect** — `git bisect` against the offending benchmark.
3. **Profile** — capture a CPU + alloc profile against the suspected
   commit.
4. **Classify**:
   - **Intended**: PR explicitly accepts the regression for a feature.
     Update the baseline; document in the PR.
   - **Unintended, fixable**: open an issue, fix in a follow-up PR.
   - **Unintended, not fixable now**: open an issue, update the
     baseline with a `regression:` annotation, and link to the issue.
5. **Record** — the next baseline JSON includes a `notes:` field per
   benchmark when a regression was accepted.

---

## Stdlib-only discipline

leather is committed to a stdlib-only Go module
([AGENTS-QUALITY.md](AGENTS-QUALITY.md)). Performance work follows the
same rule:

- No `golang.org/x/...` benchmarking helpers.
- No third-party profiling agents.
- Use `testing.B`, `runtime/pprof`, and `net/http/pprof`.

---

## Common mistakes

| Mistake | Correct approach |
|---|---|
| Quoting `ns/op` from a single `go test -bench` run | Use `-count=10 -benchtime=3s` and report median. |
| Updating the baseline silently in a "perf fix" PR | Call out the baseline diff in the PR description. |
| Adding allocations to `internal/model` types | `model` is data-only; check the alloc profile after every change. |
| Benchmarking against a real LLM endpoint | Always use `MockLLM`; otherwise the benchmark measures network jitter. |
| Profiling on a busy laptop and trusting the numbers | Use a dedicated reference machine, document its specs in the baseline. |
| Skipping the regression triage because "it's only 11%" | The gate is 10%; investigate or accept-with-note. |

---

## Verification checklist

Before opening a PR that affects performance:

- [ ] Touched hot-path code? Added or updated the matching benchmark
- [ ] `go test -bench=. -benchmem` run with `-count=10` on a quiet machine
- [ ] Allocation budget table updated if the budget changed
- [ ] Baseline file updated (or PR explicitly defers it with an issue link)
- [ ] PR description includes before/after `ns/op`, `allocs/op`, `B/op`
- [ ] Owning package's guide cross-links updated if the regression
      changes a documented invariant
- [ ] No new third-party dependency introduced

---

_Last reviewed: 2026-05-19_

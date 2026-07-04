# 11-high-volume-ci

A high-volume load experiment that fires the same fan-out/fan-in CI gate
pipeline as [example 10](../10-ci-gate/), but sends a configurable burst of
webhooks with randomised timing to stress-test the queue and curing subsystem
under real concurrency pressure.

The key structural difference from example 10 is `queue_pattern`. Instead of
every webhook competing for the same static queue, each webhook creates its own
isolated single-use queue (e.g. `pr-meta/01JXABC123`) so parallel runs never
block or corrupt each other's state.

## What this shows

- **`queue_pattern` / single-use queues** — setting `queue_pattern:
  "pr-meta/{{hide_id}}"` in `tannery.yaml` creates a new queue per webhook
  event. The pattern expands `{{hide_id}}` to the unique hide ID so fan-out
  routes can target independent queues in parallel.
- **High-volume fan-out** — `WEBHOOK_COUNT` webhooks (default 40) are sent in
  configurable bursts. Each webhook spawns 3 parallel analysis agents → 1
  decision agent — without head-of-line blocking.
- **Backpressure under load** — each queue has `max_depth`; when the worker
  pool is saturated, the server returns HTTP 503 with a `Retry-After` header.
- **Burst histograms** — the demo script prints arrival and completion
  histograms so you can see queue depth fluctuations over time.

## Requirements

- A local OpenAI-compatible endpoint at `$LEATHER_LLM_ENDPOINT`.
- `openssl` (for HMAC; preinstalled on macOS/Linux).
- `curl`, `jq`.

## Run

```bash
LEATHER_LLM_ENDPOINT=http://localhost:8000 \
LEATHER_MODEL=/path/to/your/model \
make 11
```

### Tuning knobs

| Variable | Default | Range | Effect |
|---|---|---|---|
| `WEBHOOK_COUNT` | 40 | 25–100 | Total webhooks to fire |
| `BURST_SIZE` | 5 | — | Webhooks per burst |
| `BURST_DELAY_MAX` | 2.0 | — | Max seconds between bursts |
| `JITTER_MAX` | 0.25 | — | Max per-webhook jitter (seconds) |
| `WAIT_TIMEOUT` | 300 | — | Seconds to wait for completion |
| `RUN_DURATION` | 600s | — | `leather serve --run-duration` |

Example — send 80 webhooks in rapid bursts of 10:

```bash
WEBHOOK_COUNT=80 BURST_SIZE=10 BURST_DELAY_MAX=0.5 make 11
```

## Difference from example 10

| Feature | 10-ci-gate | 11-high-volume-ci |
|---|---|---|
| Queue type | Static (`pr-metadata`, `pr-diff`, `pr-ctx`) | Single-use (`pr-meta/<hide_id>`, …) |
| Queue config | `queues:` with `concurrency` and `max_depth` | `queue_pattern:` expands per event |
| Webhook count | 1 (one demo payload) | 25–100 bursts |
| `max_concurrent_jobs` | 2 | 8 |

## Files

| File | Purpose |
|---|---|
| `config.yaml` | leather config — `max_concurrent_jobs: 8` |
| `tannery.yaml` | webhook at `/webhooks/github`, `queue_pattern` routes |
| `mcp-servers.yaml` | registers shell-mcp for gh CLI tools |
| `shell-tools.json` | `get_pr_files`, `get_pr_diff`, `post_pr_comment`, `add_pr_label` |
| `agents/ci-gate.agent.md` | same agent as example 10 |
| `curings/` | same curing set as example 10 |
| `sample/` | sample PR payloads |
| `scripts/run-demo.sh` | fires burst load, prints arrival/completion histogram |
| `scripts/send-webhook.sh` | sign and POST a single payload |

## Reliability & performance fixes

This example was used to load-test the fan-out/fan-in pipeline against a real
local vLLM endpoint (concurrency 8+, bursts of 40-100 webhooks). The issues
below were found and fixed as a result; several are framework-level fixes in
`internal/` that apply to any leather pipeline, not just this example.

### Framework fixes (`internal/`)

1. **Shared agent config mutation under concurrency**
   (`internal/curing/worker.go`). Concurrent goroutines processing different
   queue items wrote directly into a shared `*model.Agent`, garbling prompts
   under `concurrency: 8`+. Fixed by cloning the agent config (`agc := ag`)
   before any per-run mutation, in both `process()` and `handleCollected()`.

2. **HTTP client timeout racing the context deadline**
   (`internal/session/http_client.go`). `http.Client{Timeout: ...}` fired
   independently of the run's context deadline, producing spurious "context
   deadline exceeded" errors well before the real timeout. Fixed by removing
   the separate `Timeout` field — the context deadline (set once in
   `runner.Run` / the curing's `timeout_seconds`) is now the single source of
   truth for request timeouts.

3. **Queue items deleted on failure, no retry** (`internal/curing/worker.go`).
   Per-item queue processing deleted the queue entry regardless of outcome, so
   any transient failure (timeout, malformed response) permanently lost the
   item. Fixed: `handleItemFromQueue` now only deletes on success; failures
   re-enqueue (retry) up to `max_attempts`, then route to `<queue>-dlq`.

4. **Fan-in collect groups silently dropped on failure**
   (`internal/curing/worker.go`, `requeueOrDLQGroup`). This was a second,
   distinct instance of bug #3: `runCollectFromQueue` dequeues all
   `collect_size` items from the queue *before* invoking the agent, so unlike
   the per-item path, a failed `handleCollected` call (e.g. the decision
   agent's LLM call timing out) had nowhere to put the items back — no retry,
   no DLQ, and the source hides leaked on disk (cleanup only runs on
   success). `max_attempts` in the curing YAML was silently unenforced for
   this path. Fixed by mirroring the per-item retry/DLQ convention: bump each
   item's `AttemptCount` and either re-enqueue the whole group (retried next
   scan tick) or route it to `<queue>-dlq` once attempts are exhausted.

5. **`hide_kind` provenance mislabeled on fan-in artifacts**
   (`internal/curing/worker.go`). A fan-in curing's own artifact (e.g.
   `decision`) recorded `hide_kind` as whichever of its 3 input legs happened
   to be collected first (`pr-context` or `pr-metadata`), never `"decision"`
   itself. Cosmetic/provenance-only — the actual dispatch-relevant field
   (`.state/hides/*/meta.json`'s `kind`) was already correct — but fixed so
   the artifact's own record stops lying to any tooling that reads it: now
   uses the curing's own name.

6. **Duplicate tool-call guard** (`internal/runner/runner.go`). Under load,
   the model (a reasoning model via vLLM, `--tool-call-parser qwen3_xml
   --reasoning-parser qwen3`) would occasionally re-issue an already-succeeded
   tool call verbatim instead of progressing to the next step, looping until
   `max tool rounds` was hit. Confirmed via direct replay against the LLM
   endpoint *outside* leather (no framework code involved) that this is
   inherent model/parser flakiness under this backend, not a request-
   construction bug — roughly 25% of isolated replays reproduced it. Since
   this can't be fixed at the prompt level (an explicit "don't call this
   twice" instruction had zero effect), added a runtime guard: a tool call
   with the same name and arguments that already succeeded earlier in the
   run is not re-executed — the model is told it already happened and moves
   on. This also protects against accidentally duplicating a real side effect
   (e.g. posting a comment twice) if the same flakiness recurs.

7. **Self-healing retry on empty final answers** (`internal/runner/runner.go`).
   A related but distinct manifestation of the same reasoning-model
   flakiness: agents would occasionally return a completely empty final
   answer with `finish_reason: "stop"` (not `"length"` — not a truncation,
   the model just produced zero output tokens and stopped). This propagated
   downstream as blank/`N/A` fields. The existing self-healing retry only
   covered the truncation case (`finish_reason: "length"`); extended it to
   also retry once, bare, when the model stops naturally with empty content
   and no tool calls — usually enough to draw a non-empty sample from the
   same stochastic model.

8. **`thinking:` agent frontmatter field** (`internal/agent/frontmatter.go`,
   `internal/agent/agent.go`, `internal/model/model.go`,
   `internal/runner/runner.go`). New per-agent override
   (`model.Agent.DisableThinking`) that sends
   `chat_template_kwargs.enable_thinking=false` to the model. Added because
   disabling the hidden reasoning trace turned out to be the most effective
   mitigation for both bug #6 and #7 above (see performance numbers below) —
   the zero value (`false`) leaves model default behavior untouched
   everywhere else in the codebase.

### This example's configuration

- `decision.agent.md`: fixed a positional fan-in bug — the prompt said "copy
  PR_NUMBER, REPO, SHA verbatim from ANALYSIS 1", assuming the 3 parallel
  analysis agents (`pr-metadata`, `pr-diff`, `pr-context`) always arrive in
  submission order. Only `pr-metadata` emits those fields, but `pr-context`
  (zero tool calls) often won the race for slot 1, leaving nothing to copy
  and producing `PR_NUMBER: N/A`. Fixed to reference the block by its
  `(from: pr-metadata)` tag instead of position.
- All 5 agents now set `thinking: false` and `completion_reserve: 768`
  (down from the reasoning-model default of 8192) — see performance below.
- `pr-comments.agent.md`: added an explicit "do not call `post_pr_comment`
  more than once per PR" instruction (kept as defense in depth alongside the
  runner-level dedupe guard, even though the instruction alone had no
  measurable effect on the underlying flakiness).
- `shell-tools.json` / `curings/*.curing.yaml`: bumped several
  `timeout_seconds` values that were too tight for burst-load latency.
- `run-demo.sh`: fixed the completion-wait logic to track `pr-comments`
  artifacts (the pipeline's actual last stage) instead of `decision`
  artifacts, and moved the queue-drain/server-shutdown step to happen
  *before* printing the results summary. Previously the summary printed
  while `pr-comments` runs and the server's own `--pretty` live trace
  (unredirected, sharing this terminal) were still active, making it look
  like output kept streaming after the script said it was done.

### Performance

Disabling thinking mode and right-sizing `completion_reserve` (the model's
hidden `<think>` trace was consuming hundreds to thousands of tokens per LLM
call) measured a **5.2x speedup** on a 40-webhook burst load test against a
local vLLM endpoint, with equal-or-better correctness:

| Configuration | Wall time (40 webhooks) | Failures |
|---|---|---|
| Baseline (thinking enabled, reserve 8192) | 323s | intermittent `max tool rounds`, empty final answers |
| + `thinking: false` on all agents | 70s | 0 |
| + `completion_reserve: 768` | 62s | 0 |

If you point this example at a different model or backend, re-check whether
`thinking: false` is appropriate — it is a Qwen3 / vLLM
`chat_template_kwargs.enable_thinking` convention and is a no-op (harmlessly
ignored) on backends that don't recognize that key.

### Measured at scale: 100-webhook full-system profile

A 100-webhook burst (`WEBHOOK_COUNT=100 BURST_SIZE=25 BURST_DELAY_MAX=0.5`)
was profiled end to end with [`scripts/profile-run.sh`](../../scripts/profile-run.sh),
which samples host CPU/memory/disk, kernel pressure-stall information (PSI),
GPU telemetry, and vLLM's own `/metrics` alongside the run.

**Test rig:** single RTX PRO 4500 (Blackwell, 32 GB), 20-core host, local
vLLM serving Qwen3.6-35B-A3B (NVFP4), all five agents `thinking: false` with
`completion_reserve: 768`.

| Metric | Value |
|---|---|
| Webhooks completed | 100 / 100 decisions, ~276s end to end |
| LLM jobs / tokens | 499 jobs, 972,518 tokens (881,907 prompt / 90,611 completion) |
| Sustained model throughput | 3,207 prompt tok/s + 329 generation tok/s |
| Host CPU (leather + queueing) | ~6% avg — the pipeline adds almost no host load |
| Host pressure (PSI cpu/io/mem) | 0% — no stalls; disk peak 2% util |
| GPU | 60% avg / 100% peak util, ~30 GB VRAM, 202 W / 77 °C peak |
| In-flight at the model | ~5–8 running, avg 12 (peak 32) queued inside vLLM |

The shape of these numbers is the headline: **the entire orchestration layer
— webhook fan-out, per-event queues, fan-in collect, retries, artifact and
hide persistence for ~500 LLM calls — costs a few percent of one host's CPU
and no measurable IO pressure.** The pipeline is GPU-bound end to end; wall
time scales with model throughput, not with leather. To go faster, feed it a
bigger GPU (or slimmer prompts — this workload is ~10:1 prefill-heavy), not a
bigger orchestrator.

Reproduce with:

```bash
cd examples
WEBHOOK_COUNT=100 BURST_SIZE=25 BURST_DELAY_MAX=0.5 ../scripts/profile-run.sh make 11
```

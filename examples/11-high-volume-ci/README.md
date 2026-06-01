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

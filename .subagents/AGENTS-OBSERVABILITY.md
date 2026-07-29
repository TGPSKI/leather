# AGENTS-OBSERVABILITY.md — leather observability surfaces

Subagent guide for the **observability** domain: structured logging,
log levels per component, run history, status snapshots, and the
metrics export surface.

Load this guide when adding a new log call site, changing a log
level, adding a new status / history endpoint, or wiring a new
metric. For testing of these surfaces, see
[AGENTS-QUALITY.md](AGENTS-QUALITY.md). For deployment-side log
rotation, journald wiring, and dashboard scraping, see
[AGENTS-OPERATIONS.md](AGENTS-OPERATIONS.md). For security
constraints on what may be logged, see
[AGENTS-SECURITY.md](AGENTS-SECURITY.md).

---

## Scope

This guide owns:

- The `internal/logging` API surface and per-component level control.
- What goes into `--log-level` and `LEATHER_LOG_LEVEL`.
- The run-history record (the JSONL trail the scheduler writes under
  `--state-dir`).
- The status / health / metrics HTTP endpoints exposed by
  `leather serve --api`.
- The "what we may safely log" rules at each level.

It does **not** own:

- Test scaffolding (AGENTS-QUALITY).
- Deployment / log rotation / dashboards (AGENTS-OPERATIONS).
- Trust-boundary rules on output (AGENTS-SECURITY).

---

## Log levels

| Level | Use for | Examples |
|---|---|---|
| `error` | Failures that aborted a job, request, or operation. | `agent load failed: …`, `tool call returned 500`. |
| `warn` | Recoverable conditions; degraded behavior. | `cache miss after retry`, `notify backend rate-limited`. |
| `info` | Lifecycle events visible to operators. | `serve started`, `agent <name> tick`, `queue drained`. |
| `debug` | Step-by-step traces for diagnosis; off in production. | `round=2 tool=foo args=…(redacted)`. |
| `trace` | Hot-path detail; off by default; explicitly opt-in only. | Token-count math, per-byte cache key derivation. |

Default level: `info`. Tests use `error` to keep output quiet unless
a test asserts log content.

### Per-component level control

`internal/logging` exposes a `SetComponentLevel(name, level)` so a
single noisy package can be raised to `debug` without flooding the
whole binary. Component names match `internal/<pkg>`.

Examples (flag form):

```
--log-level info
--log-level info,runner=debug,mcp=trace
```

Rules:

- Component names are case-insensitive.
- Unknown component names are accepted with a warning; never fatal.
- Per-component levels override the global level.

---

## What may be logged at each level

Bound by the secret-handling and prompt-injection rules in
[AGENTS-SECURITY.md](AGENTS-SECURITY.md). Summary:

| Field | error/warn/info | debug | trace |
|---|---|---|---|
| Agent name, job id, instance name | yes | yes | yes |
| Tool name, tool call duration, exit code | yes | yes | yes |
| Tool argument **keys** | no | yes | yes |
| Tool argument **values** | no | no | no (always redact) |
| Model response text | no | no | no |
| Token counts (numeric) | yes | yes | yes |
| HTTP URLs of MCP/notify calls (no query string) | yes | yes | yes |
| Query strings / headers / auth | no | no | no |
| Secret values (resolved or not) | no (ever) | no (ever) | no (ever) |
| Replay-id (opaque) | yes | yes | yes |
| Path + size of replay file | yes | yes | yes |
| Replay file *contents* | no | no | no |

Rule of thumb: log identifiers, durations, sizes, status codes; never
log content.

---

## Run history

The scheduler writes a JSONL record per executed job under
`<state-dir>/history/<agent>.jsonl`. Schema (stable):

```json
{
  "ts": 1716115200,
  "agent": "go-release-prep",
  "instance": "morning",
  "job_id": "20260519T090000-abc1",
  "status": "ok",
  "duration_ms": 4831,
  "tokens_prompt": 1240,
  "tokens_completion": 318,
  "rounds": 3,
  "tool_calls": 5,
  "error": null,
  "replay_id": "rpl-20260519T090000-abc1"
}
```

Invariants:

- One record per job, written **after** completion (success or
  failure).
- File is append-only JSONL. Never edit in place.
- `status` ∈ {`ok`, `error`, `canceled`, `timeout`}.
- `error` is a short string when status ≠ `ok`; never includes
  agent content or tool argument values.
- File mode `0600`; directory mode `0700`.

Rotation: keep the file per-agent and let the operator rotate
(`logrotate` or `journald`). Rotation rules live in
[AGENTS-OPERATIONS.md](AGENTS-OPERATIONS.md).

---

## Status / health / metrics endpoints

When `leather serve --api` is enabled, the mux exposes:

| Endpoint | Purpose | Output |
|---|---|---|
| `GET /healthz` | Liveness **and** readiness: state-dir writable, LLM endpoint configured. | JSON `{status, checks}`; `200` ok / `503` degraded |
| `GET /status` | Uptime, version/commit, agent count, scheduler tick, concurrency (plus replay-mode fields). | JSON snapshot |
| `GET /metrics` | Per-agent run/token/latency summaries + tool-resilience counters (see catalog below). | JSON |
| `GET /jobs`, `GET /jobs/{agent}` | Scheduled jobs; per-agent job detail. | JSON |
| `GET /history` | Run-history records. | JSON |
| `GET /queues`, `/queues/{name}…` | Queue listing and per-queue operations. | JSON |
| `GET /workers` | Worker supervisor status. | JSON |
| `GET /config` | Sanitized config snapshot. | JSON |
| `GET /snapshot` | Full state snapshot (for `leather snapshot` / replay). | JSON |
| `GET /cache/stats` | Response-cache hit/miss stats. | JSON |

Rules:

- There is no per-endpoint auth on this API. It is **off by default**
  (`--api`) and binds loopback by default (`--api-addr`, default
  `127.0.0.1:7749`). Exposing it beyond loopback requires the
  reverse-proxy posture from [AGENTS-SECURITY.md](AGENTS-SECURITY.md).
- The devtools surface (`/api/devtools/*`) is separately gated by a
  per-launch bearer token.
- `/healthz` must stay cheap: it never makes outbound requests (the
  LLM endpoint is checked for *presence*, not reachability).
- `/metrics` is JSON, not Prometheus text exposition. An external
  Prometheus scrape needs a translation step (see
  [AGENTS-OPERATIONS.md](AGENTS-OPERATIONS.md)).

---

## Metrics catalog

`GET /metrics` returns a single JSON object (`metricsResponse` in
`internal/cli/cmd_serve.go`). Adding or renaming a field requires a
dashboard update note in the PR description.

Top-level fields:

| JSON field | Type | Meaning |
|---|---|---|
| `agents` | object | Map of agent name → per-agent summary (below). |
| `leather_tool_retry_total` | counter | Tool call attempts beyond the first; tells the operator how often transient failures occur across all tools. |
| `leather_tool_backoff_total` | counter | Times a backoff sleep was applied (retry-after or exponential); indicates rate-limiting pressure from upstream services. |
| `leather_tool_rate_limit_wait_total` | counter | Times a tool call waited for a per-host token-bucket token; nonzero means the configured rate limits are actively throttling traffic. |
| `leather_outbound_dlq_depth` | gauge | Current item count in `outbound-dlq`; nonzero means tool failures need operator attention (`leather dlq inspect`). |

Per-agent summary (`agents.<name>`):

| JSON field | Meaning |
|---|---|
| `run_count`, `error_count` | Cumulative jobs and failures. |
| `total_prompt_tokens`, `total_completion_tokens` | Cumulative tokens. |
| `avg_duration_ms`, `p50_ms`, `p95_ms`, `p99_ms` | Job wall-time stats. |
| `recent_runs` | Recent run records (bounded). |
| `lifecycle_file`, `schedule`, `model`, `tags`, … | Agent-config echo for dashboards. |

Rules:

- The `agents` map is keyed by agent name only; never key or label by
  job id, replay id, or a user-supplied string (unbounded
  cardinality).
- A new field requires a row above and a brief "what does it tell
  the operator?" sentence in the PR.

---

## Common mistakes

| Mistake | Correct approach |
|---|---|
| `fmt.Printf` inside a package | Use `logging.Component("pkg").Info(...)`. Never write to stdout/stderr directly except in `cmd/leather/main.go`. |
| Logging the model response | Log the *length* or token count; never the text. |
| Logging tool argument values at debug | Log keys only; values are bound by the secret/PII rules. |
| Adding a label like `job_id` to a counter | Unbounded cardinality — use a log line, not a metric. |
| Editing the history JSONL after write | Append-only; if you need a correction, append a follow-up record with `status: "amend"` and link by `job_id`. |
| Binding `--api-addr` beyond loopback "for the dashboard" | The API has no built-in auth; keep the default `127.0.0.1` bind or front it with an authenticated reverse proxy. |

---

## Verification checklist

Before opening a PR touching observability surfaces:

- [ ] No new direct `fmt.Println` or `log.Printf` in `internal/`.
- [ ] Any new log call site reviewed against the "what may be
      logged" table above.
- [ ] History JSONL schema unchanged, OR a schema version field
      added with backward-read support.
- [ ] New endpoints added to the table above and covered by a test in
      `internal/cli/cmd_serve_test.go`.
- [ ] New metrics fields added to the catalog above with bounded
      cardinality.
- [ ] `--log-level pkg=debug` for the changed component produces
      useful diagnosis output for a representative failure.
- [ ] No new metric or log line leaks secrets, prompts, or
      responses (cross-checked against
      [AGENTS-SECURITY.md](AGENTS-SECURITY.md)).

---

_Last reviewed: 2026-07-29_

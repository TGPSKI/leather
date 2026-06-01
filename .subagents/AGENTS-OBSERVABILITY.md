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

| Endpoint | Purpose | Auth | Output |
|---|---|---|---|
| `GET /healthz` | Liveness — does the process answer? | none | `200 ok` |
| `GET /readyz` | Readiness — scheduler running + config loaded? | none | `200 ready` / `503` |
| `GET /status` | Scheduler state, next ticks, queue depths. | API auth | JSON snapshot |
| `GET /status/agents` | Per-agent last-run, last-error, next-run. | API auth | JSON array |
| `GET /status/queues` | Per-queue depth + oldest item age. | API auth | JSON array |
| `GET /metrics` | Prometheus-style text exposition. | API auth (or `--metrics-public`) | text/plain |
| `GET /debug/pprof/*` | Standard `net/http/pprof` surface. | API auth + `--debug-api` | pprof |

Rules:

- `/healthz` and `/readyz` are **always** unauthenticated. They MUST
  NOT leak agent names or counts.
- Every other endpoint requires the API auth posture from
  [AGENTS-SECURITY.md](AGENTS-SECURITY.md).
- `/metrics` is text-only; no JSON variant.
- `/debug/pprof/*` is mounted only when `--debug-api` is set; never
  in default production posture.

---

## Metrics catalog

Stable metric names (Prometheus exposition). Adding or renaming a
metric requires a dashboard update note in the PR description.

| Metric | Type | Labels | Meaning |
|---|---|---|---|
| `leather_agent_runs_total` | counter | `agent`, `status` | Cumulative job count. |
| `leather_agent_run_duration_seconds` | histogram | `agent` | Job wall time. |
| `leather_agent_tokens` | counter | `agent`, `kind` (`prompt`/`completion`) | Cumulative tokens. |
| `leather_queue_depth` | gauge | `queue` | Items currently enqueued. |
| `leather_queue_oldest_seconds` | gauge | `queue` | Age of oldest item. |
| `leather_tool_calls_total` | counter | `tool`, `status` | Cumulative tool calls. |
| `leather_mcp_call_duration_seconds` | histogram | `server`, `tool` | MCP roundtrip. |
| `leather_notify_send_total` | counter | `backend`, `status` | Notifier deliveries. |
| `leather_cache_hits_total` | counter | `kind` | Response-cache hits. |
| `leather_cache_misses_total` | counter | `kind` | Response-cache misses. |
| `leather_build_info` | gauge=1 | `version`, `commit` | Build metadata. |

Rules:

- Label cardinality must stay bounded; never label with a job id,
  replay id, or user-supplied string.
- A new metric requires a row above and a brief "what does it tell
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
| Mounting `/debug/pprof` without `--debug-api` | Pprof exposes process internals; never default-on. |

---

## Verification checklist

Before opening a PR touching observability surfaces:

- [ ] No new direct `fmt.Println` or `log.Printf` in `internal/`.
- [ ] Any new log call site reviewed against the "what may be
      logged" table above.
- [ ] History JSONL schema unchanged, OR a schema version field
      added with backward-read support.
- [ ] New endpoints have an auth posture matching the table above
      and a test in `internal/cli/cmd_serve_test.go`.
- [ ] New metrics added to the catalog above with bounded label
      cardinality.
- [ ] `--log-level pkg=debug` for the changed component produces
      useful diagnosis output for a representative failure.
- [ ] No new metric or log line leaks secrets, prompts, or
      responses (cross-checked against
      [AGENTS-SECURITY.md](AGENTS-SECURITY.md)).

---

_Last reviewed: 2026-05-19_

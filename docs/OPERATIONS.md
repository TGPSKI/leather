# leather Operations Guide

This guide is for an operator running `leather serve` in a production-like
setting — a workstation, home-network server, or single-tenant VM. It covers
the on-disk layout, supervisor integration, health/readiness signals, log
discipline, queue and dead-letter recovery, DevTools authentication,
upgrades, and troubleshooting.

If you are looking for *how to write an agent*, see [README.md](../README.md)
and [GUIDE.md](GUIDE.md). For trust boundaries and the v0.3 security
posture, see [SECURITY.md](../SECURITY.md). For architecture context, see
[ARCHITECTURE.md](ARCHITECTURE.md). For the day-2 ops roadmap, see
[ROADMAP.md](../ROADMAP.md).

## State directory layout

`leather serve` keeps all mutable runtime state under `--state-dir`
(default `~/.leather/.state`, env `LEATHER_STATE_DIR`). Files are written
with mode `0600` and directories with mode `0700`.

```text
<state-dir>/
├── leather.lock          flock-held by the running serve process (T4.1)
├── devtools.token        per-launch bearer token for the DevTools UI (0600)
├── .healthz-probe        transient touch file written by /healthz; removed
├── queues/               file-backed JSONL queues, one file per queue
│   ├── <queue>.jsonl              static queues (e.g. triage-in.jsonl)
│   ├── <queue>-dlq.jsonl          dead-letter sibling for each queue
│   └── <prefix>/<id>.jsonl        single-use queues
├── runs/                 run-history JSONL (only when --persist-runs)
│   └── <agent>.jsonl     rotated at --run-max-bytes (default 10 MiB)
└── cache/                file-backed response cache (--cache-dir override)
```

Tannery storage (hides and artifacts) is configured separately via
`tannery.yaml` keys `hide_dir` and `artifact_dir`. Those paths are *not*
required to live under `<state-dir>` and frequently do not.

## Process management

`leather serve` is a long-running foreground process. Run it under any
process supervisor that can restart on failure and capture stdout/stderr.

### systemd unit

```ini
# /etc/systemd/system/leather.service
[Unit]
Description=leather agent runtime
After=network-online.target
Wants=network-online.target

[Service]
Type=simple
User=leather
Group=leather
Environment=LEATHER_CONFIG=/etc/leather/config.yaml
Environment=LEATHER_STATE_DIR=/var/lib/leather
ExecStart=/usr/local/bin/leather serve
Restart=on-failure
RestartSec=5s
# leather writes structured logs to stderr; journald captures them.
StandardOutput=journal
StandardError=journal
# Tighten the sandbox to taste; leather is a normal stdlib Go binary.
NoNewPrivileges=true
ProtectSystem=strict
ReadWritePaths=/var/lib/leather
ProtectHome=true
PrivateTmp=true

[Install]
WantedBy=multi-user.target
```

```bash
sudo systemctl daemon-reload
sudo systemctl enable --now leather
journalctl -u leather -f
```

### launchd (macOS)

A `~/Library/LaunchAgents/com.tgpski.leather.plist` with `KeepAlive=true`,
`StandardOutPath`, and `StandardErrorPath` pointing at log files works the
same way. Redirect both streams so log rotation can manage the file.

### Single-process lock

`leather serve` takes an exclusive `flock` on `<state-dir>/leather.lock`
for the lifetime of the process. A second invocation against the same
state directory exits **immediately with code 2** and prints to stderr:

```text
leather serve: another process holds <state-dir>/leather.lock (or it cannot be created): <reason>
```

This prevents two serve processes from corrupting each other's queues,
caches, and run history. If you need two parallel servers (e.g. per-user
tenancy), give each its own `--state-dir` *and* its own `--api-addr`.

## Health and readiness

When `--api` is enabled the HTTP server binds to `--api-addr` (default
`127.0.0.1:7749`). Three endpoints matter for monitoring.

### `GET /healthz`

Readiness probe that exercises two checks without making outbound LLM
calls:

- `state_dir` — writability via a touch + remove of `.healthz-probe`.
- `llm_endpoint` — non-empty configuration.

Returns `200 OK` with `{"status":"ok","checks":{...}}` when both pass, or
`503 Service Unavailable` with `{"status":"degraded","checks":{...}}` when
either fails. Sample degraded response:

```json
{
  "status": "degraded",
  "checks": {
    "state_dir":    {"ok": false, "error": "open /var/lib/leather/.healthz-probe: permission denied"},
    "llm_endpoint": {"ok": true}
  }
}
```

### `GET /metrics`

Returns a JSON object — **not** Prometheus exposition format. The shape is:

```json
{
  "agents": {
    "<agent-name>": {
      "schedule": "0 9 * * *",
      "model": "llama3",
      "run_count": 42,
      "error_count": 1,
      "total_prompt_tokens": 18234,
      "total_completion_tokens": 4901,
      "avg_duration_ms": 812.3,
      "p50_ms": 740, "p95_ms": 1480, "p99_ms": 2100,
      "recent_runs": [ /* model.RunRecord entries */ ]
    }
  }
}
```

If you need Prometheus, scrape this endpoint and adapt it externally. A
native Prometheus exporter is not in v0.3.

### `GET /cache/stats`

Returns response-cache hit/miss counts when `--cache-dir` is configured
(default `<state-dir>/cache`). Returns a clear JSON error when the cache
is not configured.

### Tannery endpoints

When the tannery is enabled (`hide_dir`, `artifact_dir`, and at least one
loaded curing or webhook route), the API exposes:

| Method | Path | Purpose |
|---|---|---|
| `POST` | `/intake` | Webhook intake. Body is forwarded to `curing.Router`. Signed-webhook secret is validated when configured. |
| `GET` | `/hides` | List persisted hides (newest first). |
| `GET` | `/hides/{id}` | Inspect one hide and its metadata. |
| `GET` | `/artifacts` | List artifacts across all curings. |
| `GET` | `/artifacts/{id}` | Inspect one artifact, including `parent_hide_ids` lineage. |
| `GET` | `/curings` | Loaded curing inventory (definition + queue + output mode). |

These endpoints share the unauthenticated-by-default trust model of the
rest of the HTTP surface; expose them only on loopback or behind a
reverse proxy.

### DevTools API

`/devtools` (HTML bundle) and `/api/devtools/*` (snapshot, inspect, trace,
SSE event stream) are gated by Bearer auth using the per-launch token at
`<state-dir>/devtools.token` (mode `0600`, written on startup). To call
them:

```bash
TOKEN=$(cat <state-dir>/devtools.token)
curl -s -H "Authorization: Bearer $TOKEN" http://127.0.0.1:7749/api/devtools/snapshot | jq
```

Available paths:

| Method | Path | Purpose |
|---|---|---|
| `GET` | `/api/devtools/snapshot` | Aggregate state snapshot (queues, jobs, recent runs, hides, artifacts). |
| `GET` | `/api/devtools/inspect/{kind}/{id}` | One entity (run, hide, artifact, queue item, etc.). |
| `GET` | `/api/devtools/trace/{id}` | Causality trace rooted at one event sequence. |
| `GET` | `/api/devtools/events` | Server-Sent Events stream of live activity. Use a `Last-Event-ID` header to resume. |

### Agent enumeration on startup

`leather serve` always prints a one-line summary plus one line per agent
to stdout before the scheduler starts, regardless of `--pretty`:

```text
leather: 6 agents loaded, 5 enabled, 1 disabled
  - portfolio-digest  schedule="0 9 * * *"
  - pr-monitor        queue=triage-in (consumer)
  - skeptic-leather-audit  disabled (enabled: false)
  ...
```

Use this line to confirm your config actually parses and selects what you
expect. A missing agent here means it failed schema validation; check
stderr for the corresponding `agent load error` log line.

## Backup and restore

Use `leather snapshot save` and `leather snapshot restore`. Both commands
require that `leather serve` is **not running** — they detect the lock file
and exit with an error if the service is active.

```bash
# Save a snapshot (defaults to leather-snapshot-<timestamp>.tar.gz)
leather snapshot save --output /backups/leather-$(date +%F).tar.gz

# Restore into an existing state directory (--force required if non-empty)
leather snapshot restore \
  --input /backups/leather-2026-06-05.tar.gz \
  --force
```

The archive includes `queues/`, `runs/`, and `cache/` from `state_dir`, plus
`hide_dir/` and `artifact_dir/` when tannery is configured. Transient files
(`leather.lock`, `devtools.token`) are excluded from saves and not written
on restore.

**Why stop first.** Queues are append-only JSONL files. A snapshot taken
during a write can capture a partial trailing line, silently dropping the
most recent enqueue. Stopping the service flushes pending writes and
releases `leather.lock`, which the snapshot commands check before proceeding.

## Log rotation

leather writes structured logs (`text` or `json`, controlled by
`--log-format`) to stderr. Pick one of:

- **journald** (recommended under systemd) — set `StandardError=journal`
  and use `journalctl --vacuum-size=…` for retention.
- **`--log-file <path>`** — leather opens the file in append mode with
  mode `0600`. Rotate externally with `logrotate` (`copytruncate`) or
  `multilog`. leather does not re-open on `SIGHUP`; `copytruncate` keeps
  the same inode and is the simplest path.
- **container stdout/stderr** — let your container runtime handle it
  (`docker logs`, k8s log driver).

### Pretty-mode caveat

`--pretty` is for interactive console use. When `--pretty` is set
*without* `--log-file`, structured logs go to `io.Discard` and stdout
prints only the rendered turns. The first line in that case is:

```text
leather: structured logs discarded (--pretty mode). Pass --log-file <path> to capture.
```

For any unattended deployment, either drop `--pretty` or always pair it
with `--log-file`. Pretty mode is also auto-disabled when stdout is not a
TTY (so redirecting to a file in a script still produces parseable
output).

## DLQ workflow

Each queue `<name>` has an automatically-created dead-letter sibling at
`<name>-dlq` to which failed items are routed after the worker's retry
budget is exhausted.

### Inspect

```bash
# Head of the DLQ for queue "triage-in"
curl -s http://127.0.0.1:7749/queues/triage-in-dlq | jq
```

```json
{ "name": "triage-in-dlq", "len": 3, "head": { /* QueueItem */ } }
```

### Requeue

`POST /queues/<name>/requeue` drains the `<name>-dlq` and re-enqueues
every item onto `<name>`. Per-item failures are rolled back into the DLQ
so nothing is lost.

```bash
curl -s -X POST http://127.0.0.1:7749/queues/triage-in/requeue | jq
```

Successful response (`200 OK`):

```json
{ "requeued": 3, "failed": null }
```

Partial success (`207 Multi-Status`) when some items could not be
re-enqueued:

```json
{
  "requeued": 2,
  "failed": [
    { "item_id": "01HXYZ...", "error": "queue: backpressure" }
  ]
}
```

Items in the `failed` array were placed back on the DLQ. Investigate the
underlying worker error, fix the cause, and retry.

### Drain

`DELETE /queues/<name>?confirm=yes` empties a queue. The `confirm=yes`
guard is mandatory; without it the server returns `400`.

## DevTools authentication

`leather serve` generates a fresh bearer token on every launch, writes it
to `<state-dir>/devtools.token` (mode `0600`, hex string), and prints the
access URL to stderr:

```text
Devtools: http://127.0.0.1:7749/ui/devtools.html#token=<hex> (also at <state-dir>/devtools.token)
```

Programmatic access:

```bash
TOKEN=$(cat ~/.leather/.state/devtools.token)
curl -s -H "Authorization: Bearer $TOKEN" http://127.0.0.1:7749/api/devtools/...
```

The token gates only `/api/devtools/*`. The rest of the HTTP surface
(`/healthz`, `/metrics`, `/queues/...`, `/jobs`, `/status`, `/config`,
`/history`, `/snapshot`, `/replay/...`) is unauthenticated and assumes
loopback binding. See [SECURITY.md](../SECURITY.md) for the full trust
model.

### Non-loopback warning

If `--api-addr` binds to anything other than a loopback address, the
process emits a `[SECURITY]` warning at startup:

```text
[SECURITY] HTTP API is bound to a non-loopback address with no authentication; ...
```

Treat this as a hard error in unattended deployments. Put an
authenticating reverse proxy in front, or rebind to `127.0.0.1` and tunnel
in.

## Upgrading

leather is a single static binary with no runtime data migrations in v0.3.

1. Stop the service: `systemctl stop leather` (releases `leather.lock`).
2. Replace the binary: `install -m 0755 leather /usr/local/bin/leather`.
3. (Optional) `leather validate` to confirm the new binary still accepts
   your config and agent files.
4. Start the service: `systemctl start leather`.

On-disk formats — queue JSONL, run-history JSONL, cache files, hides,
artifacts — persist across upgrades. The lockfile prevents accidentally
starting the new binary while the old one is still running.

If an upgrade introduces a breaking config change, `leather validate`
fails closed with a schema violation; the old binary keeps running until
you fix the config and restart.

## Troubleshooting

| Symptom | Where to look |
|---|---|
| `leather serve` exits with code 2 on start | Another process holds `<state-dir>/leather.lock`. Either stop that process or point this one at a different `--state-dir`. |
| Agent never runs | Check the startup enumeration line. Confirm the agent is `enabled` in its lifecycle file and that either a `schedule` or a `queue` is set. For queue consumers, confirm something is enqueueing on the input queue. |
| `503` from `/healthz` | Read the `checks` object in the body. Most often `state_dir` is unwritable (permissions, full disk) or `llm_endpoint` is unset. |
| `429` / token-budget summarisation thrash | Inspect `/metrics` for the agent's `total_prompt_tokens` vs `max_tokens`; tune `--summarize-threshold` or `--max-tokens`. |
| No logs in pretty mode | You ran `--pretty` without `--log-file`. Structured logs were sent to `/dev/null` by design. Add `--log-file <path>` or drop `--pretty`. |
| DLQ keeps growing | Each `<queue>-dlq.jsonl` is the failure record. Read the head with `GET /queues/<queue>-dlq`, fix the root cause, then `POST /queues/<queue>/requeue`. |
| Cannot reach DevTools UI | Confirm `<state-dir>/devtools.token` exists and matches the URL fragment, and that you are connecting from loopback (or through an authenticating proxy). |
| `[SECURITY]` warning at startup | `--api-addr` is non-loopback. Rebind to `127.0.0.1` or front with an authenticating proxy. |
| Snapshot/replay shows no jobs | Replay derives `Jobs` from the snapshot; if the capture happened before any job ran, the array is empty. Capture again after the scheduler has had at least one tick. |

For deeper debugging, `--log-level debug` increases verbosity across the
runtime. Logs identify components and agent names but never include token
content, API keys, or hide payloads.

## What is NOT in v0.3

The following commonly-requested operational features are explicitly
deferred. See [ROADMAP.md](../ROADMAP.md) for the full deferral list.

- **Prometheus exposition** — `/metrics` returns JSON. Adapt externally.
- **Hot config reload** — `SIGHUP` reloads the worker supervisor but not
  every config field. For substantive changes, restart the process.
- **Encryption at rest** — hides and artifacts are protected only by
  filesystem permissions. Use an encrypted volume for sensitive payloads.
- **Multi-tenant isolation** — one serve process is single-user. Run one
  per user with its own `--state-dir` and `--api-addr`.

# Changelog

All notable changes to this project are documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and the project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

## [0.1.3] - 2026-06-04

### Added

- `leather doctor` subcommand: prints effective configuration with source
  attribution (`default` vs. `config/env/flag`) for every key. Secret-bearing
  values (`llm_api_key`) are redacted to a 4-char prefix + mask so operators
  can confirm which credential is loaded without exposing the full token.
- `leather init` subcommand: scaffolds a new project directory with a
  `.env`, `config.yaml`, example `agents/my-agent.agent.md`,
  `agents/my-agent.lifecycle.yaml`, and a `Makefile`.
  - `--dir <path>` selects the target directory (created if absent; defaults
    to `~/.leather`).
  - `.env` pre-populates `LEATHER_LLM_ENDPOINT`, `LEATHER_MODEL`,
    `LEATHER_LLM_API_KEY`, `LEATHER_LOG_LEVEL`, and `LEATHER_AGENT_DIR`
    with comments for `source .env` / direnv usage.
  - Fails closed on existing files — any collision is reported with a hint to
    use `--overwrite`.
  - `--overwrite` replaces existing files.
  - Schema-validates the scaffolded `config.yaml` and lifecycle file before
    reporting success.
- **Qwen/Hermes text tool call fallback**: models that emit
  `<tool_call>{json}</tool_call>` blocks in the content channel instead of
  the API `tool_calls` array now parse and execute correctly. Truncated
  trailing blocks (finish_reason=length) are silently dropped so the run
  continues on the next round.
- **Examples 13–15** — Raspberry Pi 5 + AI HAT+ 2 (Hailo-10H) examples
  validated on live hardware against `qwen3:1.7b`:
  - `13-rpi-hailo-endpoint-canary`: endpoint sanity check.
  - `14-rpi-hailo-local-status-digest`: shell evidence collection → scheduled
    digest without tannery.
  - `15-rpi-hailo-local-status-ingest`: evidence → hide → curing → artifact.
- `docs/integrations/rpi-hailo.md` integration guide for Raspberry Pi 5 +
  Hailo-10H.
- `make install` target and `LEATHER_RPI_*` env vars in the examples Makefile.
- GitHub issue template for agent work items.

### Changed

- Tool call limit raised from 16 to 100 in `internal/schema/defs.go`, removing
  a ceiling that caused batch agents to hit mid-run limits on large workloads.

## [0.1.2] - 2026-06-01

### Changed

- Replaced `LICENSE` with canonical GPL-3.0 SPDX text for `pkg.go.dev` license
  detection.

## [0.1.1] - 2026-06-01

### Added

- `doc.go` package documentation for `pkg.go.dev` landing page.

## [0.1.0] - 2026-05-31

First public release.

### Added

#### Core runtime

- Single-binary CLI (`leather`) with subcommands `serve`, `run`, `chat`,
  `validate`, `test-agent`, `status`, `ingest`, `replay`, `version`,
  `help`.
- Agent definition format: Markdown body with optional YAML front matter
  and a sibling `*.lifecycle.yaml` for schedule, model overrides, and
  per-turn parameters. Lifecycle-only and front-matter-only flows both
  supported; `applyLifecycle` is a non-destructive merge that preserves
  front-matter for fields the lifecycle does not explicitly set.
- Session context management with token-budget tracking against any
  OpenAI-compatible endpoint (local vLLM, OpenAI cloud, etc.), including
  summarisation and truncation strategies before model limits are hit.
- Multi-turn tool-calling with deterministic abort gating and a
  per-turn parameter scope.
- Deterministic runtime variables: tool results can extract values that
  later turns substitute via `{{key}}` templating. Templates supported:
  `{{env:VAR}}`, `{{key}}`, `{{.field}}`, `{{hide_id}}`.
- Buffered "hides" intercept oversized tool output so the agent reads
  scoped cuts/pages instead of saturating the context window.
- Companion `shell-mcp` binary: a Model Context Protocol server that
  exposes a manifest-defined catalog of local shell commands as MCP
  tools, with positional-arg templating and `--no-shell` parsing-only
  mode for CI.

#### Tools, skills, toolsets

- Native stdio-based MCP client. Allowlists per server. Subprocess
  hygiene: `setpgid`, stderr forwarding, `SIGTERM` → `SIGKILL` on the
  process group at shutdown. Decoder is poisoned on read timeout so
  subsequent `Call` invocations return `ErrPoisoned` instead of reading
  garbage off a desynchronised stream.
- Per-skill `required_env` allowlist for `{{env:VAR}}` expansion in
  tool arguments — env-var exfiltration through tool arg templates is
  blocked at the skill boundary.
- Shell, HTTP, and MCP tool definitions resolvable via tools, toolsets,
  or skill manifests with deterministic precedence rules. `*.toolset.yaml`
  files validated by `leather validate`.

#### Tannery (event-driven curing service)

- Event-driven curing pipeline: ingest a hide, route it through one or
  more agents, produce an artifact with full lineage.
- Persistent file-backed hide store with safe-path anchoring (no
  traversal out of the configured root).
- Artifact store with `curing` + `hide_id` lineage fields and parent-dir
  creation on file output routes.
- Webhook intake worker with body-size caps (5 MiB default, 50 MiB hard
  limit), mandatory secret validation (fail-closed on unset env), and
  fan-out idempotency keyed on `X-GitHub-Delivery` (`EnqueueIfAbsent` +
  hide rollback on enqueue failure).
- HTTP poll worker with `Retry-After` honouring (seconds or HTTP-date,
  capped at 5 minutes) for `429` / `503` responses.

#### Scheduler & queues

- Cron-style scheduler with bounded concurrency (`--max-concurrent-jobs`),
  graceful shutdown that drains in-flight work before cancelling, and
  SIGHUP-triggered re-registration when agent files change on disk
  (sha256-hash diff).
- Per-job emit serialisation in the curing worker by default;
  `EventFnConcurrent` opt-out for callers that need concurrent event
  delivery.
- File-backed JSONL FIFO queues with retry counters and per-queue DLQ.
- Single-use ephemeral queues for high-concurrency fan-in / fan-out
  patterns. `queue_pattern` → `queue_prefix` linkage validated at config
  load with a clear error on mismatch.
- HTTP API for `/queues/<name>`, `/queues/<name>-dlq`, and
  `/queues/<name>/requeue` (multi-status 207 on partial requeue
  failures with explicit `failed[]` list).

#### Observability & operations

- `/healthz` reports state-dir writability and LLM-endpoint
  configuration; returns 503 + JSON body when degraded.
- `/metrics` (Prometheus-style text format) and `/status` endpoints.
- DevTools UI at `/devtools` with per-launch hex auth token written to
  `<state-dir>/devtools.token` (`0600`), Bearer-middleware enforced.
- Welcome card with token input + Retry on first-connect failure
  (no-token, network error, 401/403, 503, loading timeout).
- Flow view in DevTools renders curings as pipelines; SSE event stream
  with CR/LF-sanitised `event:` fields.
- Replay subsystem: capture sessions, replay them later via
  `leather replay` (translates to `serve --api --replay` /
  `--replay-live`) for deterministic debugging and demos.
- Single-process lock per `--state-dir` via `flock`; the second process
  exits with code 2 and a clear remediation message.
- Pretty-mode CLI output with auto-disable when stdout is not a TTY,
  Tannery event icons (`→` webhook, `↑` enqueue, `↓` dequeue), inline
  agent responses, and explicit log-discard warning in pretty mode.
- Startup banner enumerates loaded agents with per-agent
  `schedule=…` / `queue=… (consumer)` / `disabled` rows.

#### Configuration

- Stdlib-only YAML loader for `config.yaml`, `tannery.yaml`,
  `*.lifecycle.yaml`, `*.agent.md` front matter, `*.toolset.yaml`, and
  MCP-server manifests.
- Schema validation via `leather validate <dir>` with `version:` field
  reserved on every top-level type for forward compatibility.
- Every flag has a matching `LEATHER_*` env var; flag wins on conflict.
- Schema files under `schemas/` describing every supported document.

#### Examples (12)

End-to-end runnable examples under `examples/`, each with its own
`Makefile` target, README, and `.env.example`:

1. `01-hello-mock` — smoke test against the mock LLM.
2. `02-scheduled-agent` — periodic cron-driven agent.
3. `03-shell-skill` — local tool execution via shell-mcp.
4. `04-tannery-ingest` — ingest a file as a hide, run a curing.
5. `05-tannery-webhook` — receive a GitHub webhook, route to a curing.
6. `06-multi-agent-curing` — two-agent pipeline producing an artifact.
7. `07-external-routing` — outbound notification (Telegram).
8. `08-dead-letter-queue` — DLQ inspection and requeue workflow.
9. `09-land-tracker` — long-running state aggregation.
10. `10-ci-gate` — parallel webhook fan-out for PR checks.
11. `11-high-volume-ci` — single-use queue pattern for high-throughput CI.
12. `12-spa-maintenance` — scheduled multi-step maintenance pipeline.

A `make examples-all` target runs every example end-to-end with a
per-target reliability/summary script.

#### Documentation

- `README.md` with a `Which mode do I want?` decision table,
  Raspberry Pi / small-server sizing guidance, install-verification
  snippet, and an explicit `Not in v0.1` section.
- `docs/GLOSSARY.md` as the authoritative vocabulary reference.
- `docs/ARCHITECTURE.md` with package layout and Mermaid diagrams of the
  Tannery pipeline.
- `docs/OPERATIONS.md` covering state-dir layout, systemd unit,
  `/healthz` + `/metrics` shape, DLQ workflow, DevTools auth, upgrade
  procedure, and troubleshooting table.
- `docs/TEMPLATES.md` single-table reference for `{{env:VAR}}`,
  `{{key}}`, `{{.field}}`, `{{hide_id}}`.
- `docs/GUIDE.md` end-to-end author guide.
- `AGENTS.md` + 17 per-domain `.subagents/AGENTS-*.md` guides for
  AI-coding-agent contribution flow.
- `SECURITY.md` with v0.1 threat model and known limits.
- `CONTRIBUTING.md`, `CODE_OF_CONDUCT.md`, `LICENSE` (GPL v3).

#### CI / release

- `.github/workflows/ci.yml`: SHA-pinned actions, `make check` +
  `make test-race` + `golangci-lint` + integration tests on every push
  and PR; cross-platform `full-scope` matrix (linux/arm64, macos/arm64)
  on `main` push or `full-test` label.
- `.github/workflows/release.yml`: triggered by `v*` tag push, builds
  `leather` + `shell-mcp` for linux/amd64, linux/arm64, darwin/amd64,
  darwin/arm64 with SHA-256 checksums; publishes a GitHub Release with
  notes extracted from this file.
- `.github/ruleset-*.json` declarative branch and tag protection
  rulesets (signed commits, required reviews, immutable release tags).

### Security

- Path-traversal anchoring (`internal/safepath`) applied to hide,
  artifact, queue, cache, and tool `OutputFile` writers.
- Outbound HTTP tool client uses a 30 s timeout.
- HTTP server uses 5/60/120/120 s read/write/idle/handler timeouts.
- Telegram bot tokens scrubbed from `*url.Error` strings before logging.
- DevTools demo bundle gated behind `?demo=1`.
- Non-loopback API bind emits a startup warning.
- SSE `event:` field CR/LF sanitisation to block injection through
  event names.
- Webhook handler validates secrets at startup (fails closed) and uses
  `EnqueueIfAbsent` with idempotency keys to prevent duplicate fan-out
  on partial writes.
- Tannery init wrapped in a success-guard so partial-init failure cannot
  leave a stale lock behind.

### Fixed (selected from release-readiness cycle)

Representative highlights from the multi-phase review sweep:

- `{{hide_id}}` template variable now reads the artifact's `HideID`
  rather than a stale curing-level value.
- Shutdown ordering: scheduler and workers are drained before context
  cancellation; two data races in `cmd_serve.go` closed.
- Metrics summaries snapshot under RLock and iterate outside the lock.
- DLQ requeue propagates per-item errors via `failed[]` and returns
  HTTP 207 on partial failure.
- Lifecycle parser preserves nested-block indentation; mistyped silent
  lifecycles no longer flatten parameter maps.
- Per-run timeout in the runner now covers the whole round loop, not a
  single turn.
- Response cache key includes user prompts (`\x01`-joined) so identical
  system prompts with different inputs no longer collide.
- Bus subscribers can be cleanly removed via `SubscribeWithCloser` with
  an idempotent `sync.Once` closer; publishers do I/O outside the mutex.
- `chat` REPL uses a 1 MiB scanner buffer and per-call SIGINT
  cancellation so Ctrl-C aborts an in-flight call without killing the
  loop.
- `/cache/stats` memoised with a 1000-entry cap and 10 s TTL.
- MCP `tools/list` schema fetching fixed for block-style frontmatter
  lists.

### Known limitations (post-v0.1 roadmap)

Intentionally out of scope for v0.1.0; tracked for v0.2:

- Shared library extraction (`internal/{fileutil,jsonstore,ids,yamlx,httpx,template,synx}`).
- `leather doctor` and `leather init` scaffolding subcommands.
- Backup/restore tooling beyond `tar -czf state-dir`.
- LLM-side prompt-injection mitigation in the summariser (hide buffering
  already isolates untrusted bulk output).
- `seedSeen` persistence in the HTTP poll worker.
- Embedded UI assets via `embed.FS` (UI currently shipped from `ui/`).
- DevTools event-model expansion for queue-input agent lineage.
- Outbound HTTP tool resilience (uniform rate-limiting, retry/backoff,
  outbound-failure DLQ for tool calls hitting external APIs).
- Windows support (Makefile assumes POSIX tools).

See [ROADMAP.md](ROADMAP.md) for the full deferred-item list with
rationales and proposed shapes.

[Unreleased]: https://github.com/tgpski/leather/compare/v0.1.0...HEAD
[0.1.0]: https://github.com/tgpski/leather/releases/tag/v0.1.0

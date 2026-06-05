# Roadmap

This is the public roadmap for leather. Items here are deliberately deferred
from v0.1.0 with stated rationale. The list is curated, not exhaustive —
small UX polish and individual bug fixes land directly on `next` without
appearing here.

For shipped functionality, see [CHANGELOG.md](CHANGELOG.md).
For trust boundaries and known security limits, see [SECURITY.md](SECURITY.md).

---

## v0.1.x (patch line)

Small, low-risk improvements that do not change public contracts.

- **Documentation polish** — quick-pivot links from README, per-doc
  reading-time hints, GLOSSARY split, ARCHITECTURE diagram refresh,
  competitive-landscape cross-links, final docs-render pass before
  each tag.
- **Doc link audit** — pre-tag automated check that every internal
  Markdown link resolves.
- **README values / non-goals** — finish the marketing-tone pass on
  the project values lead.

## v0.2 — operability and refactor

The major v0.2 themes are *day-2 operations*, *codebase hygiene*, and
*UI maturity*.

### New subcommands

- **`leather workflow run`** — a standalone primitive for bounded
  one-shot tannery workflows. Today CLI workflows require exported env
  vars, zsh wrapper functions, temporary hide files, manual `ingest`,
  `serve --max-jobs`, queue counting, and shell-side shutdown logic. The
  command should accept a curing or route name, caller cwd, structured
  args/env bindings, and an optional stdin payload; create the initial
  hide; run the required queues to completion with existing retry/DLQ
  semantics; print a compact status/artifact summary; and exit with a
  meaningful code. This gives agent-backed tasks like signed per-file git
  commits a clear Leather-owned execution path instead of process glue.
- **`leather snapshot save / restore`** — built-in backup tooling.
  Today the procedure is *stop the service, tar the state dir, start
  again* (see [docs/OPERATIONS.md](docs/OPERATIONS.md#backup-and-restore)).

### Runtime

- **`seedSeen` persistence in the HTTP poll worker** — the worker
  currently re-seeds its dedup set on restart. Acceptable for
  low-frequency feeds; needs a small key/value store for high-volume
  pollers. Blocked on the shared library extraction below.
- **Embedded UI assets via `embed.FS`** — `//go:embed` cannot escape
  the package directory, so this needs `ui/` to move to
  `internal/uiassets/ui/` with the corresponding doc and script
  updates. Focused mini-PR.
- **LLM-side prompt-injection mitigation in the summariser** — the
  hide buffer already isolates untrusted bulk output from the model's
  context. A dedicated summariser pass is the next layer; needs a
  design pass before implementation.
- **DevTools event-model expansion** — richer lineage for
  queue-input-driven agents, plus the broader event-stream redesign
  that the JS refactors below depend on.

### Outbound HTTP tool resilience

Today retry/backoff lives in the inbound HTTP poll worker (the v0.1.0
worker honours `Retry-After` on 429/503) and in queue/curing item
processing. Tool-level outbound calls (MCP-backed `gh`, `curl`, etc.
used in examples 09–12) have no shared retry policy and no quarantine
path for 429/5xx responses.

Proposed shape:

- Per-tool `retry: { max_attempts, base_delay, max_delay, honor_retry_after }`
  in `shell-tools.json` / skill manifests. Defaults: `3 / 1s / 30s / true`.
- Classify tool exit codes and parsed status (when JSON/HTTP) into
  `transient` vs `permanent`. Retry only transient failures.
- On permanent failure (or attempts exhausted), enqueue into a
  dedicated `outbound-dlq` queue keyed by `(curing, tool, target)`
  with the original args and last error — mirroring the existing
  curing DLQ shape so `leather requeue` works uniformly.
- Global token-bucket rate limiter per `tool.host` (derived from
  command or URL), configurable in `config.yaml`:

  ```yaml
  tools:
    rate_limits:
      api.github.com: 5000/h
  ```

- Expose counters in `/metrics`: tool retries, backoff sleeps,
  `outbound-dlq` depth.

Touches `internal/tool`, `internal/mcp`, `internal/queue`,
`internal/worker`, `internal/cli` (new `outbound-dlq` admin surface).
Sized at ~3–5 days. Depends on the curing-DLQ refactor stabilising in
the v0.1.x line.

### Codebase hygiene

- **Shared library extraction** —
  `internal/{fileutil,jsonstore,ids,yamlx,httpx,template,synx}`.
  Roughly 5–8 engineer-days. Explicitly non-launch-blocking.
  Unblocks `seedSeen` persistence, schema `file:line` prefixes,
  centralised config doc generation, and several smaller refactors.
- **Schema `file:line` prefix on violations** — needs the parser
  line-number tracking that ships with the shared `yamlx`.
- **Lifecycle `disable:` deprecation**

### UI / DevTools

- **Larger JS refactors** — state management consolidation, list
  virtualisation for the timeline, theming. Current UI is functional;
  this is structural polish.
- **Cosmetic polish backlog** — incremental, non-blocking items.

---

_Last reviewed: 2026-06-02_

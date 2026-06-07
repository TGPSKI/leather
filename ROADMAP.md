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

The major v0.2/v0.3 themes are *day-2 operations*, *codebase hygiene*, and
*UI maturity*. Shipped items are left here as release markers; remaining
forward-looking work is tracked under v0.4 below.

### New subcommands

- ~~**`leather workflow run`** — a standalone primitive for bounded
  one-shot tannery workflows. Today CLI workflows require exported env
  vars, zsh wrapper functions, temporary hide files, manual `ingest`,
  `serve --max-jobs`, queue counting, and shell-side shutdown logic. The
  command should accept a curing or route name, caller cwd, structured
  args/env bindings, and an optional stdin payload; create the initial
  hide; run the required queues to completion with existing retry/DLQ
  semantics; print a compact status/artifact summary; and exit with a
  meaningful code. This gives agent-backed tasks like signed per-file git
  commits a clear Leather-owned execution path instead of process glue.~~
  — shipped in v0.3.0.
- ~~**`leather snapshot save / restore`**~~ — shipped in v0.2 (issue #6).
- ~~**`leather attach`** — join a running `serve` instance and stream
  pretty-printed runtime logs in the terminal. Connects to the API
  server (SSE or a new `/logs/stream` endpoint), renders structured
  log lines with color-coded levels, component labels, and key-value
  pairs. Supports `--filter` by component or level; reconnects with
  backoff if the serve process restarts.~~ — shipped in v0.2.0 (issue #19).

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

- ~~Outbound HTTP tool resilience (retry, DLQ, per-host rate limits)~~ —
  shipped in v0.3.0 (#7, #8, #9).

### Codebase hygiene

- ~~**Shared library extraction** —
  `internal/{fileutil,jsonstore,ids,yamlx,httpx,template,synx}`.
  Roughly 5–8 engineer-days. Explicitly non-launch-blocking.
  Unblocks `seedSeen` persistence, schema `file:line` prefixes,
  centralised config doc generation, and several smaller refactors.~~
  — partially shipped in v0.2.0 (`fileutil`, `jsonstore`, `ids`, `yamlx`,
  `httpx`).
- ~~**Schema `file:line` prefix on violations** — needs the parser
  line-number tracking that ships with the shared `yamlx`.~~ — shipped in
  v0.2.0.
- **Lifecycle `disable:` deprecation**

### UI / DevTools

- **Larger JS refactors** — state management consolidation, list
  virtualisation for the timeline, theming. Current UI is functional;
  this is structural polish.
- **Cosmetic polish backlog** — incremental, non-blocking items.

## v0.4 — planned follow-up

The next planned minor keeps the remaining v0.2/v0.3 deferred work focused:

- **HTTP poll dedup persistence** — persist `seedSeen` for high-volume pollers.
- **Embedded UI assets** — move browser assets under an embeddable package and
  serve them from the binary.
- **Prompt-injection mitigation pass** — add an LLM-side summariser defense
  layer for untrusted hide/tool content.
- **DevTools event-model expansion** — continue queue lineage and event-stream
  work now that `queue.run` causality is in place.
- **Lifecycle `disable:` deprecation** — settle the migration path and docs.
- **DevTools UI refactors** — state management, timeline virtualisation, and
  theming.

---

_Last reviewed: 2026-06-07_

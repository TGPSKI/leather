# `.subagents/` — leather subagent guide index

This directory holds the **domain-scoped subagent guides** referenced by
[../AGENTS.md](../AGENTS.md). Each file is the authoritative source for
one domain. Load the matching guide before doing focused work in that
area instead of reading the full root guide.

The root [`AGENTS.md`](../AGENTS.md) routing table is the canonical
navigation surface. This file is its mirror; if the two ever disagree,
the root guide wins and this file is the bug.

---

## Index

| Guide | Domain | Owns / scope |
|---|---|---|
| [AGENTS-CORE.md](AGENTS-CORE.md) | Core internals | `internal/agent`, `internal/session`, `internal/model` |
| [AGENTS-AGENTDEF.md](AGENTS-AGENTDEF.md) | Author-facing agent file format | `*.agent.md`, `*.lifecycle.yaml`, front-matter, multi-turn syntax |
| [AGENTS-TOOLS-SKILLS-TOOLSETS.md](AGENTS-TOOLS-SKILLS-TOOLSETS.md) | Tool / skill / toolset semantics | Precedence, naming, per-turn scope |
| [AGENTS-RUNTIME.md](AGENTS-RUNTIME.md) | Execution runtime | `internal/runner`, `internal/tool`, `internal/mcp`, `internal/cache`, `internal/notify` |
| [AGENTS-WORKER.md](AGENTS-WORKER.md) | Scheduling & workers | `internal/scheduler`, `internal/queue`, `internal/worker` |
| [AGENTS-TANNERY.md](AGENTS-TANNERY.md) | Event-driven curing | `internal/curing`, `internal/artifact`, `internal/hide`, `internal/safepath` |
| [AGENTS-SERVE.md](AGENTS-SERVE.md) | Serving, config, CLI, HTTP API | `internal/cli`, `internal/config`, `internal/schema`, `internal/secret`, `internal/devtools`, `cmd/leather` |
| [AGENTS-SHELL-MCP.md](AGENTS-SHELL-MCP.md) | `shell-mcp` companion binary | `cmd/shell-mcp` |
| [AGENTS-UI.md](AGENTS-UI.md) | Browser SPA | `ui/` |
| [AGENTS-REPLAY.md](AGENTS-REPLAY.md) | Replay subsystem | Replay capture + `/replay/...` API + replay UI |
| [AGENTS-QUALITY.md](AGENTS-QUALITY.md) | Tests, build, CI, linting | `*_test.go`, `Makefile`, `.github/workflows` |
| [AGENTS-PERFORMANCE.md](AGENTS-PERFORMANCE.md) | Performance posture | Hot paths, benchmark catalog, baseline policy |
| [AGENTS-SECURITY.md](AGENTS-SECURITY.md) | Security posture | Threat model, secret handling, trust boundaries |
| [AGENTS-OPERATIONS.md](AGENTS-OPERATIONS.md) | Deployment & operations | Layouts, systemd/launchd, backup/restore, upgrade |
| [AGENTS-INTEGRATIONS.md](AGENTS-INTEGRATIONS.md) | Integrations authoring | Notifier / MCP server / webhook-worker / scanner patterns |
| [AGENTS-EXAMPLES.md](AGENTS-EXAMPLES.md) | Examples & tutorials | `tanning/`, tutorial sequence, example-as-test policy |
| [AGENTS-OBSERVABILITY.md](AGENTS-OBSERVABILITY.md) | Observability | `internal/logging`, run history, status/health/metrics endpoints |

---

## Conventions

- Every guide ends with `_Last reviewed: YYYY-MM-DD_`. The
  `agents-doc-lifecycle` skill audits this footer.
- Every guide stays in the **80–500 LOC** band. Above 500 LOC is a
  split signal; below 80 LOC is a merge signal.
- Cross-references between guides are markdown links with the bare
  filename (e.g. `[AGENTS-RUNTIME.md](AGENTS-RUNTIME.md)`).
- A guide owns one **load-this-guide** sentence at the top declaring
  its scope and pointing at adjacent guides.


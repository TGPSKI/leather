---
name: documentation-lifecycle
description: "Generate, update, and maintain per-module documentation under docs/modules/, keep docs/ARCHITECTURE.md and README.md current, and produce Mermaid diagrams for data-flow, block, and dependency views. USE FOR: documenting a package; updating architecture docs after structural changes; generating diagrams; auditing documentation freshness; first-time doc generation for the project. DO NOT USE FOR: writing implementation code; debugging runtime errors; updating AGENTS.md or .subagents/ guides (use agents-doc-lifecycle instead)."
compatibility: Designed for GitHub Copilot and similar coding agents working in the leather repository.
metadata:
  argument-hint: 'What doc operation? e.g. "generate all module docs", "update docs/modules/session.md", "audit freshness", "regenerate ARCHITECTURE.md", "update README.md flags"'
  user-invocable: "true"
---

# documentation-lifecycle

Keeps `docs/modules/`, `docs/ARCHITECTURE.md`, and `README.md` synchronized
with the leather codebase. These docs serve contributors and AI agents
navigating the project. Stale docs cause wrong decisions and wasted context.

**Scope boundary:** This skill does not touch `AGENTS.md` or `.subagents/`.
Those are owned by the `agents-doc-lifecycle` skill.

---

## Documentation surfaces

| Surface | Path | Audience |
|---|---|---|
| Module docs | `docs/modules/<package>.md` | Contributors + AI agents |
| Architecture | `docs/ARCHITECTURE.md` | Contributors + AI agents |
| README | `README.md` | Users + contributors |

---

## Required context

Before generating or updating documentation, read:

- `AGENTS.md` — hard constraints (stdlib-only, single binary, zero external deps, error patterns, package responsibilities)
- `internal/model/model.go` — shared domain types (Config, Agent, Job, TokenBudget, Message, LLMResponse, LogLevel)
- `internal/cli/cli.go` — subcommand dispatch table and `usage` string
- Any `.go` source files in the target package (not `_test.go` for API surface; include for test summary)

---

## Package inventory

leather has seven `internal/` packages plus one `cmd/` entrypoint:

| Package | Path | Primary responsibility |
|---|---|---|
| `model` | `internal/model/` | Shared domain types; no logic, no imports of other internal pkgs |
| `logging` | `internal/logging/` | Structured logging wrapping `log/slog`; per-component level control |
| `agent` | `internal/agent/` | Agent definition parsing, validation, registry |
| `config` | `internal/config/` | YAML + flag + env-var config loading, defaults, flag binding |
| `session` | `internal/session/` | Token budget tracking, LLM client interface, HTTP client, summarization |
| `scheduler` | `internal/scheduler/` | Cron parsing, job queue, execution dispatch, graceful shutdown |
| `cli` | `internal/cli/` | Subcommand dispatch, serve loop, chat loop, run, validate, status |
| `main` | `cmd/leather/` | Thin entrypoint; calls `cli.Run()`; only place `os.Exit` is allowed |

---

## Module doc template

Each `docs/modules/<package>.md` follows this structure:

```markdown
# <package>

> One-line purpose statement.

## Responsibility

2–4 sentences describing what this package owns and why it exists as a
separate module.

## Public API

| Symbol | Signature | Description |
|--------|-----------|-------------|
| ...    | ...       | ...         |

## Internal Design

Key data structures, algorithms, concurrency model, or non-obvious design
choices. What a contributor needs to modify the package safely.

## Dependencies

Which internal packages this module imports and why.

## Data Flow

Mermaid diagram showing how data enters, transforms, and exits this module.

## Test Surface

Summary of test files, what they cover, notable helpers (e.g. MockLLM).

## Related Docs

Links to ARCHITECTURE.md sections or other module docs that interact with
this package.
```

---

## Operations

### 1) Generate module doc

Create a new `docs/modules/<package>.md` for a package that has none.

1. Read all `.go` source files in the target package.
2. Extract exported symbols (types, functions, constants) and their doc comments.
3. Extract `import` blocks to build the internal dependency list.
4. Search callers (`grep_search` for the package name in other packages) to
   understand what calls into this package.
5. Write the doc using the template above.
6. Generate a Mermaid `flowchart LR` for the data-flow section.
7. If the package introduces structure not yet in `docs/ARCHITECTURE.md`,
   update it.

### 2) Update module doc

Refresh an existing `docs/modules/<package>.md` after code changes.

1. Re-read source to detect new/removed/changed exports.
2. Diff against the existing doc; apply minimal targeted updates.
3. Regenerate diagrams only if data flow or dependencies changed.

### 3) Generate or update ARCHITECTURE.md

**Trigger:** New `internal/` package added or removed; import edges change;
CLI subcommands added; domain types added to `model`.

1. Read all `internal/*/` package directories (exported symbols + imports).
2. Read `cmd/leather/main.go` and `internal/cli/cli.go` for subcommands.
3. Read `internal/model/model.go` for domain types.
4. Write or update `docs/ARCHITECTURE.md` with:
   - Project overview paragraph
   - Package layout table (package → path → responsibility)
   - Dependency direction rule (direction diagram: model has no internal deps;
     everything imports model; cli sits at the top)
   - Mermaid `graph TD` dependency diagram
   - CLI subcommand table
   - Key domain types table
   - Design decisions section (stdlib-only, single binary, fail-closed, etc.)

### 4) Generate or update README.md

**Trigger:** New subcommand; flag changes; feature additions; folder layout
changes; first-time creation.

README sections (in order):
1. **What it is** — one paragraph, target environment, key capabilities
2. **Install** — `go install` + build from source
3. **Quick start** — minimal working example
4. **Subcommands** — table: name | description
5. **Configuration** — YAML + flag + env-var reference table (all flags)
6. **Agent definition format** — `*.agent.md` + `*.lifecycle.yaml` examples
7. **Token budget defaults** — table from AGENTS.md
8. **Folder layout** — `cmd/`, `internal/`, `Makefile` targets
9. **Development** — `make` targets table, test strategy note

When updating:
1. Read `internal/cli/cli.go` → subcommand list
2. Read `internal/config/config.go` → all `fs.Bool/String/Int/Float64/Duration`
   calls → flag name, env var, default, description
3. Apply minimal edits; preserve existing prose style.

### 5) Audit documentation freshness

1. List all `internal/` packages; flag any without `docs/modules/<pkg>.md`.
2. Compare `docs/ARCHITECTURE.md` package list against actual directories.
3. Compare `README.md` subcommand list against `internal/cli/cli.go` dispatch.
4. Compare `README.md` flag list against `BindFlags` in `internal/config/config.go`.
5. Report drift items; wait for user confirmation before updating.

### 6) Generate all docs (first-time)

Run this sequence when `docs/` does not yet exist:

1. Read `AGENTS.md`, `internal/model/model.go`, `internal/cli/cli.go`, `internal/config/config.go`.
2. Read each `internal/<pkg>/` package source.
3. Generate `docs/ARCHITECTURE.md`.
4. Generate `README.md`.
5. Generate `docs/modules/<pkg>.md` for every package in the inventory.

---

## Diagram conventions

- `graph TD` (top-down) for the package dependency diagram.
- `flowchart LR` (left-right) for pipeline and data-flow diagrams.
- `sequenceDiagram` for LLM request/response flows.
- Node labels: short package or function names, not full paths.
- Embed diagrams directly in the relevant markdown inside fenced `mermaid` blocks.
- leather dependency direction: `model` ← everything; `cli` → all others;
  `cmd/leather` → `cli` only.

---

## Guardrails

- Do not fabricate API surfaces; always read source before documenting.
- Do not add doc comments to `.go` source files; this skill produces standalone docs only.
- Keep module docs factual and concise; no marketing language.
- When updating README or ARCHITECTURE, make minimal targeted edits rather than wholesale rewrites.
- Respect the hard constraints from `AGENTS.md`: stdlib-only, zero external deps,
  `os.Exit` only in `main.go`, no `init()` functions.
- Never document internal implementation details that are subject to change
  as if they were stable API.

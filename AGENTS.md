# AGENTS.md

Guidance for AI coding agents (GitHub Copilot, Claude, etc.) working in this
repository. Read this top section before any change. Route focused work to the
appropriate domain guide rather than loading the entire codebase. 

1. Read this file
2. Identify the domain of the task.
3. Load the matching `.subagents/AGENTS-{DOMAIN}.md` rules file into current 
   context as primary instruction, or spawn a subagent(s) scoped to that domain 
   if the task is complex.
4. If applicable, aggregate subagent outputs in the orchestrating session.

### Subagent routing table

| You're working on… | Load this guide | Owns |
|---|---|---|
| Agent loader internals, session/token management, model types | [.subagents/AGENTS-CORE.md](.subagents/AGENTS-CORE.md) | `internal/agent`, `internal/session`, `internal/model` |
| **Author-facing agent file format** (`*.agent.md`, `*.lifecycle.yaml`, front-matter, multi-turn) | [.subagents/AGENTS-AGENTDEF.md](.subagents/AGENTS-AGENTDEF.md) | The user-visible agent definition spec |
| **Tool / skill / toolset resolution** (precedence, naming, per-turn scope) | [.subagents/AGENTS-TOOLS-SKILLS-TOOLSETS.md](.subagents/AGENTS-TOOLS-SKILLS-TOOLSETS.md) | Resolution semantics across `tools`, `toolsets`, `skills` |
| Agent execution, tool calling, MCP runtime, response caching, bot messaging | [.subagents/AGENTS-RUNTIME.md](.subagents/AGENTS-RUNTIME.md) | `internal/runner`, `internal/tool`, `internal/mcp`, `internal/cache`, `internal/notify` |
| Scheduling, queues, background HTTP poll workers | [.subagents/AGENTS-WORKER.md](.subagents/AGENTS-WORKER.md) | `internal/scheduler`, `internal/queue`, `internal/worker` |
| **Tannery** (event-driven curing service, hides, artifacts) | [.subagents/AGENTS-TANNERY.md](.subagents/AGENTS-TANNERY.md) | `internal/curing`, `internal/artifact`, `internal/hide`, `internal/safepath` |
| Config loading, CLI subcommands, schema validation, flag/env wiring, HTTP API | [.subagents/AGENTS-SERVE.md](.subagents/AGENTS-SERVE.md) | `internal/config`, `internal/cli`, `internal/schema`, `internal/secret`, `internal/devtools`, `cmd/leather` |
| **`shell-mcp` companion binary** (manifest format, templating, `--no-shell`, JSON-RPC conformance) | [.subagents/AGENTS-SHELL-MCP.md](.subagents/AGENTS-SHELL-MCP.md) | `cmd/shell-mcp` |
| **Browser UI** (`ui/`) | [.subagents/AGENTS-UI.md](.subagents/AGENTS-UI.md) | `ui/` SPA, design tokens, API contract layer |
| **Replay subsystem** (capture, storage format, action endpoints, redaction) | [.subagents/AGENTS-REPLAY.md](.subagents/AGENTS-REPLAY.md) | Replay capture + `/replay/...` API + replay UI views |
| Tests, benchmarks, Makefile, CI, linting | [.subagents/AGENTS-QUALITY.md](.subagents/AGENTS-QUALITY.md) | `*_test.go`, `Makefile`, `.github/workflows` |
| **Performance** (hot paths, benchmarks, allocation budgets, pprof, baseline) | [.subagents/AGENTS-PERFORMANCE.md](.subagents/AGENTS-PERFORMANCE.md) | Performance posture; cross-cutting |
| **Security** (threat model, secret handling, API authn/authz, trust boundaries, prompt-injection) | [.subagents/AGENTS-SECURITY.md](.subagents/AGENTS-SECURITY.md) | Trust-boundary policy; cross-cutting |
| **Operations** (deploy layout, systemd/launchd, single-process lock, backup/restore, upgrade) | [.subagents/AGENTS-OPERATIONS.md](.subagents/AGENTS-OPERATIONS.md) | Deployment + lifecycle; cross-cutting |
| **Integrations authoring** (how to add a notifier, MCP server, webhook worker, Skeptic-style scanner) | [.subagents/AGENTS-INTEGRATIONS.md](.subagents/AGENTS-INTEGRATIONS.md) | Authoring patterns across `internal/notify`, `internal/mcp`, `internal/worker` |
| **Examples & tutorials** (`tanning/`, demo agents/skills/toolsets, `docs/tutorials/`) | [.subagents/AGENTS-EXAMPLES.md](.subagents/AGENTS-EXAMPLES.md) | `tanning/`, tutorial sequence, example-as-test policy |
| **Observability** (log levels per component, run history JSONL, status/health/metrics endpoints) | [.subagents/AGENTS-OBSERVABILITY.md](.subagents/AGENTS-OBSERVABILITY.md) | `internal/logging`, history records, `/status`, `/metrics`, `/healthz` |

If a task spans multiple domains, spawn one subagent per domain in parallel and
merge their findings.

**Maintaining this file:** a dedicated agent skill handles keeping AGENTS.md
and all subagent guides synchronized with the codebase. See
[.agents/skills/agents-doc-lifecycle/SKILL.md](.agents/skills/agents-doc-lifecycle/SKILL.md).

---

## What leather is

`leather` is a slim local agent runtime, orchestration harness, and
workflow interface for developer workstations and home-network servers. No external dependencies, stdlib-only Go.

Leather runs scoped agents against local or OpenAI-compatible model endpoints,
connects them to tools, manages context pressure, and turns raw tool or event
output into bounded operational artifacts.

Repository: `github.com/tgpski/leather`  
Binary: `leather`

### Core capabilities:

1. **Local agent runtime** — agents are defined in plain Markdown files with
   optional YAML front matter. Leather loads, validates, runs, and schedules them.
2. **Session context management** — Leather tracks token budgets for local vLLM
   or any OpenAI-compatible endpoint, summarizing or truncating context before
   the model's limit is exceeded.
3. **Tool and skill execution** — agents can call configured tools, including MCP
   tools, shell-backed tools, HTTP tools, and skill-defined toolsets.
4. **Deterministic runtime variables** — tool results can extract values into
   runtime variables and inject them into later turns via `{{key}}`
   substitution.
5. **Buffered hides** — oversized tool results can be intercepted into a hide
   buffer. Agents receive scoped cuts/pages instead of full raw output, allowing
   large PR threads, logs, diffs, and curl responses to be inspected without
   saturating the context window.
6. **Scheduling and background execution** — `leather serve` runs scheduled agents,
   queue-backed jobs, and long-running worker processes.
7. **Flexible runtimes** — `leather serve`, `leather run`, and `leather chat` are
   core runtime modes: long-running services, one-shot runs, and interactive agent 
   sessions.

---

## Vocabulary

All leather terms are defined in [docs/GLOSSARY.md](docs/GLOSSARY.md), the authoritative reference. It also tracks which terms are realized in current code versus planned for future packages.

### Vocabulary quick reference

```text
Leather    = CLI/runtime/binary
Tanning    = local working area: configs, agents, curings, tools
Tannery    = long-running workspace container
Hide       = raw input material
Cut        = scoped page/slice of a buffered hide
Agent      = one scoped model worker
Operation  = Leather-language name for an agent step
Curing     = N-agent workflow that transforms hides into artifacts
Artifact   = stabilized output with lineage
```

---

## Build and test

```bash
make build          # go build -o ./leather ./cmd/leather
make test           # go test ./...
make test-race      # go test -race ./...
make check          # gofmt (fail on diff) + go vet
make lint           # golangci-lint run
make ci             # check + test-race + lint + integration
make bench          # all benchmarks (-benchmem)
```

---

## Universal Must / Must-not

These rules apply to every package. Domain guides may add constraints but
cannot relax these.

### Must

- **stdlib only. Zero external deps.** `go.mod` has no `require` entries.
  This is a hard rule, not a preference.
- **Use the canonical vocabulary.** When naming types, fields, CLI flags, YAML
  keys, or packages, consult [docs/GLOSSARY.md](docs/GLOSSARY.md) first.
  New code uses glossary terms. Existing code is migrated incrementally.
- **Fail closed.** A failed agent load, failed config parse, or missing token
  budget returns a clear error. Never silently continue with an invalid state.
- **Never log secret values.** Log agent names, job IDs, and component names.
  Never log token content, API keys, or user-supplied text verbatim.
- **Keep packages small and auditable.** Each `internal/` package does one
  thing. Files target <500 LOC. A reviewer should read any package in under
  ten minutes.
- **`os.Exit` only in `main.go`.** `cli.Run()` returns an `int`. All other
  code returns errors.
- **Scheduling does not block the CLI response.** `leather serve` starts the
  scheduler loop in a background goroutine; the main goroutine handles
  signals and graceful shutdown.
- **Every flag has a matching env var.** `--flag-name` → `LEATHER_FLAG_NAME`.
  Flag wins when both are set.

### Must not

- Use temporary files with permissions wider than 0600.
- Place business logic in `main.go`. Dispatch to `internal/` packages.
- Use `init()` functions. Explicit initialization only.
- Store compiled regexes in package-level `var` without `sync.Once` or
  compile-time `MustCompile`. Lazy compilation without synchronization is
  a data race.

---

## Code conventions

- **Errors:** wrap with `fmt.Errorf("package/function: %w", err)`. Include
  the agent name or job ID, never raw content.
- **Tests:** table-driven where appropriate. Use `t.TempDir()` for all
  file I/O. Use `t.Helper()` in test helpers. Fake the `LLMClient`
  interface with `MockLLM` — never call a real model in tests.
- **Doc comments:** every exported type and function gets a one-line
  value-focused doc comment. Update comments when behavior changes.
- **Flag sets:** each CLI subcommand uses its own `flag.FlagSet` with
  `flag.ContinueOnError`. Parse errors return to the caller; never call
  `os.Exit` in flag parsing.
- **Context:** all LLM calls and long-running operations take a
  `context.Context`. Pass it through; don't ignore cancellation.
- **JSON/YAML:** use `encoding/json` only (no third-party JSON libs).
  YAML config is parsed via a minimal stdlib-only parser; no `gopkg.in/yaml`.
- **Time:** use `time.Now().Unix()` for timestamp fields. Format
  human-readable timestamps as `2006-01-02 15:04:05`.
- **Concurrency:** the scheduler loop runs one goroutine per periodic job,
  bounded by `--max-concurrent-jobs`. Use `sync.WaitGroup` for graceful
  shutdown; never orphan goroutines.

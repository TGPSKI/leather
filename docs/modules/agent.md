# agent

> Agent-file parsing, lifecycle merging, and validation for scheduled and one-shot agents.

## Responsibility

`agent` turns Markdown agent definitions and lifecycle YAML into validated
`model.Agent` values. It owns front-matter parsing, multi-turn body parsing,
lifecycle inheritance, and the additive merge rules for tags, skills, and
toolsets. The package deliberately stops at parsing and validation; execution,
LLM calls, and scheduling all live elsewhere.

## Public API

| Symbol | Signature | Description |
|--------|-----------|-------------|
| `LoadDir` | `func LoadDir(dir string) ([]model.Agent, []error)` | Scan a directory, parse all `*.agent.md` and `*.lifecycle.yaml` files, merge them, validate the results, and return sorted agents plus per-file errors. |
| `LoadFile` | `func LoadFile(path string) (model.Agent, error)` | Parse one `*.agent.md` file into a base `model.Agent`. |
| `ApplyLifecycleFile` | `func ApplyLifecycleFile(agentPath string, a *model.Agent) error` | Apply a co-located singleton lifecycle file to an already-loaded agent when present. |
| `Validate` | `func Validate(a model.Agent) []error` | Validate required fields and value ranges after parsing and merging. |

## Internal Design

`LoadFile` parses YAML front matter into scalar fields such as `name`,
`schedule`, `model`, `tool_rounds`, `skills`, and `toolsets`, then splits the
Markdown body into a system prompt plus optional user-turn sections. Each turn
section may declare `skills: [...]`, `toolsets: [...]`, or `tools: [...]`
before the actual prompt text.

`LoadDir` works in phases: load all base agent files, load all lifecycle files,
clone and overlay lifecycle records onto the matching base agents, then add any
unclaimed base agents that rely on front-matter scheduling. Lifecycle list form
(`instances:`) produces multiple `model.Agent` values from one base file.

Lifecycle merging is more than schedule/model overlay. It also carries cache
config, output routes, queue settings, parameters, hooks, additive tags,
additive skills, additive toolsets, and prompt overrides. When lifecycle
`prompt` or `prompts` is present, body-derived turn declarations are cleared so
execution uses the lifecycle prompt chain instead.

`Validate` is intentionally narrow: it enforces `name`, `schedule`, and valid
temperature range. `model` may still be filled later by CLI config defaults.

## Dependencies

| Package | Why |
|---|---|
| `internal/config` | Reuses `ParseBlock` for lifecycle YAML parsing. |
| `internal/model` | Produces `model.Agent`, `CacheConfig`, `OutputRoute`, and hook types. |

## Data Flow

```mermaid
flowchart LR
    AF[*.agent.md] --> LF[LoadFile]
    LF --> BODY[splitAgentBody]
    LC[*.lifecycle.yaml] --> LY[loadLifecycleFile]
    BODY --> MERGE[applyLifecycle]
    LY --> MERGE
    MERGE --> VAL[Validate]
    VAL --> OUT[[]model.Agent]
```

## Test Surface

`internal/agent/agent_test.go` covers full front-matter parsing, turn
declarations, defaults when no front matter exists, validation errors,
directory loading, singleton lifecycle files, lifecycle list form, and missing
agent-definition references. Tests use `t.TempDir()` and write fixture files at
runtime.

## Related Docs

- [docs/modules/model.md](model.md)
- [docs/modules/config.md](config.md)
- [docs/modules/runner.md](runner.md)
- [docs/ARCHITECTURE.md](../ARCHITECTURE.md)

# model

> Shared domain types used across leather's runtime, CLI, and file formats.

## Responsibility

`model` is the foundation of the internal dependency graph. It defines the
structs and enums that other packages exchange, persist, expose through APIs,
or serialize in tool and replay flows. The package is otherwise free of
business logic and imports only the standard library, with one narrow
exception: `LookupReserve`, a small static lookup table for reasoning-model
`completion_reserve` defaults (see below).

## Public API

| Symbol | Signature | Description |
|--------|-----------|-------------|
| `LogLevel` | `type LogLevel string` | Structured logging verbosity enum. |
| `JobStatus` | `type JobStatus string` | Run/scheduler status enum. |
| `ToolDefinition` | `type ToolDefinition struct { ... }` | One callable tool, including HTTP or MCP executor config. |
| `MCPToolConfig` | `type MCPToolConfig struct { ... }` | MCP server name and remote tool name for `mcp` tools. |
| `MCPServerConfig` | `type MCPServerConfig struct { ... }` | Parsed `mcp-servers.yaml` entry. |
| `HTTPToolConfig` | `type HTTPToolConfig struct { ... }` | HTTP method, URL, headers, query, and body templates for a tool. |
| `ToolCall` | `type ToolCall struct { ... }` | Model-requested tool invocation. |
| `ToolResult` | `type ToolResult struct { ... }` | Tool execution result content plus optional error string. |
| `Skill` | `type Skill struct { ... }` | Named tool bundle with prompt append and optional parameters. |
| `Toolset` | `type Toolset struct { ... }` | Named bundle of tool names only, used for exposure policy. |
| `SecretRef` | `type SecretRef struct { ... }` | Pass-store / environment reference for secrets. |
| `NotifyBackendConfig` | `type NotifyBackendConfig struct { ... }` | Telegram or Signal backend configuration. |
| `CacheConfig` | `type CacheConfig struct { ... }` | Per-agent response-cache settings. |
| `OutputRoute` | `type OutputRoute struct { ... }` | Post-run output destination descriptor. |
| `WorkerOutput` | `type WorkerOutput struct { ... }` | Queue destination for worker-collected items. |
| `WorkerDefinition` | `type WorkerDefinition struct { ... }` | Parsed polling-worker definition. |
| `QueueItem` | `type QueueItem struct { ... }` | File-queue payload item. |
| `AgentHooks` | `type AgentHooks struct { ... }` | Optional pre/post shell hooks around agent runs. |
| `Agent` | `type Agent struct { ... }` | Parsed and lifecycle-resolved agent definition. |
| `Job` | `type Job struct { ... }` | Scheduler job record and API payload. |
| `Message` | `type Message struct { ... }` | Session message, including tool-call metadata. |
| `TokenBudget` | `type TokenBudget struct { ... }` | Token ceiling, reserve, and summarization threshold. |
| `LLMResponse` | `type LLMResponse struct { ... }` | Parsed completion result from the LLM client. |
| `Config` | `type Config struct { ... }` | Fully merged runtime configuration. |
| `SessionContext` | `type SessionContext struct { ... }` | Snapshot of a conversation window. |
| `Turn` | `type Turn struct { ... }` | One prompt/response pair stored in a run record. |
| `RunTokens` | `type RunTokens struct { ... }` | Prompt, response, and total token counts for a run. |
| `RunTime` | `type RunTime struct { ... }` | Start timestamp and duration for a run. |
| `RunRecord` | `type RunRecord struct { ... }` | Stored/served result of one completed agent execution. |
| `RunOptions` | `type RunOptions struct { ... }` | Per-invocation options for CLI entrypoints. |
| `LogLevelDebug` | `const LogLevelDebug LogLevel = "debug"` | Debug logging level. |
| `LogLevelInfo` | `const LogLevelInfo LogLevel = "info"` | Info logging level. |
| `LogLevelWarn` | `const LogLevelWarn LogLevel = "warn"` | Warn logging level. |
| `LogLevelError` | `const LogLevelError LogLevel = "error"` | Error logging level. |
| `JobStatusPending` | `const JobStatusPending JobStatus = "pending"` | Pending scheduler state. |
| `JobStatusRunning` | `const JobStatusRunning JobStatus = "running"` | Running scheduler state. |
| `JobStatusSuccess` | `const JobStatusSuccess JobStatus = "success"` | Successful run state. |
| `JobStatusError` | `const JobStatusError JobStatus = "error"` | Failed run state. |
| `JobStatusSkipped` | `const JobStatusSkipped JobStatus = "skipped"` | Skipped scheduler state. |
| `LookupReserve` | `func LookupReserve(modelName string) (int, bool)` | Suggested `completion_reserve` for a known reasoning model (substring match against `Agent.Model`), and whether one was found. |

## Internal Design

The exported types cluster into a few durable groups:

- Agent and scheduling state: `Agent`, `Job`, `JobStatus`, `RunOptions`
- Tooling and integration surfaces: `ToolDefinition`, `MCPToolConfig`,
  `MCPServerConfig`, `Skill`, `Toolset`, `ToolCall`, `ToolResult`
- Session and execution reporting: `Message`, `TokenBudget`, `LLMResponse`,
  `SessionContext`, `Turn`, `RunTokens`, `RunTime`, `RunRecord`
- Config and outputs: `Config`, `CacheConfig`, `OutputRoute`,
  `NotifyBackendConfig`, `SecretRef`, `AgentHooks`, worker and queue types

Most runtime-facing structs carry JSON tags because they are serialized in run
history, APIs, queue files, tool requests, or MCP responses. The package stays
otherwise logic-free so other packages can depend on these shapes without
introducing import cycles.

`reasoning.go` is the one exception: a static `map[string]int` of known
reasoning-model name substrings (e.g. `qwen3`, `qwq`, `deepseek-r1`) to a
suggested `completion_reserve`, checked case-insensitively against
`Agent.Model` by `LookupReserve`. It lives here rather than in `runner` or
`cli` because both `curing/worker.go` and `cli/cmd_serve.go` need it when
resolving a `TokenBudget`, and putting it in `model` avoids a new import edge
between those two packages. It is intentionally tiny and leather-internal —
no external manifest, no vanity coupling — and callers always let an explicit
per-agent `CompletionReserve` override win over the lookup's suggestion.

## Dependencies

`model` imports only `time` and `strings` from the standard library.

## Data Flow

```mermaid
flowchart LR
    CFG[config] --> Config
    AG[agent] --> Agent
    SES[session] --> Message
    SES --> TokenBudget
    SES --> LLMResponse
    TOOL[tool/mcp] --> ToolDefinition
    SCH[scheduler] --> Job
    RUN[runner] --> RunRecord
```

## Test Surface

`internal/model/reasoning_test.go` directly tests `LookupReserve` against
known and unknown model names. The rest of the package has no direct tests;
its correctness is exercised indirectly through the packages that parse,
transform, persist, and expose these types.

## Related Docs

- [docs/modules/config.md](config.md)
- [docs/modules/agent.md](agent.md)
- [docs/modules/session.md](session.md)
- [docs/modules/tool.md](tool.md)
- [docs/ARCHITECTURE.md](../ARCHITECTURE.md)

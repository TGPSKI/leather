# AGENTS-CORE.md — leather core internals

Subagent guide for the **core internals**: agent loading code, session
context management, token budgeting, the LLM client interface, and
shared model types.

Load this guide when working inside `internal/agent`,
`internal/session`, or `internal/model`.

> **Author-facing spec for `*.agent.md` / `*.lifecycle.yaml` / front-matter
> fields lives in [AGENTS-AGENTDEF.md](AGENTS-AGENTDEF.md).** This guide
> covers the loader code, type tables, and session machinery only.

For agent execution, tool calling, MCP, caching, and messaging, see
[AGENTS-RUNTIME.md](AGENTS-RUNTIME.md). For scheduling, queues, and
workers, see [AGENTS-WORKER.md](AGENTS-WORKER.md). For serving and
config, see [AGENTS-SERVE.md](AGENTS-SERVE.md). For tests and build,
see [AGENTS-QUALITY.md](AGENTS-QUALITY.md). For tool / skill / toolset
resolution semantics, see
[AGENTS-TOOLS-SKILLS-TOOLSETS.md](AGENTS-TOOLS-SKILLS-TOOLSETS.md).

---

## Package responsibilities

### `internal/model`

Shared domain types only. Zero intra-project imports; stdlib only.

| Type | Kind | Purpose |
|---|---|---|
| `LogLevel` | string enum | `debug`, `info`, `warn`, `error` |
| `JobStatus` | string enum | `pending`, `running`, `success`, `error`, `skipped` |
| `ToolDefinition` | struct | Callable tool: name, type (`"http"`/`"mcp"`), executor config, optional `output_file` |
| `MCPToolConfig` | struct | MCP-backed tool binding: server name plus remote tool name |
| `MCPServerConfig` | struct | One `mcp-servers.yaml` server entry: name, command, transport |
| `HTTPToolConfig` | struct | HTTP tool request template: method, URL, headers, query, body |
| `ToolCall` | struct | LLM-requested tool call: id, name, arguments (map) |
| `ToolResult` | struct | Tool execution result: tool call id, tool name, content, optional error string |
| `Skill` | struct | Loaded skill bundle: name, system prompt append, optional parameters, tools slice |
| `Toolset` | struct | Named bundle of tool names only; no prompt text |
| `SecretRef` | struct | Secret pointer: Pass (pass-store path) and/or Env (env var name) |
| `NotifyBackendConfig` | struct | Messaging backend config: name, type, ChatID/From/To/GroupID/APIURL, Token SecretRef |
| `CacheConfig` | struct | Per-agent cache: Enabled bool, TTL duration |
| `OutputRoute` | struct | Output destination: Type (`file`/`queue`/`http`/`notify`), FilePath, Queue, URL, Method, Headers, NotifyBackend |
| `WorkerOutput` | struct | Worker output config: Queue name, DedupKey field path |
| `WorkerDefinition` | struct | Worker config: name, type (`http_poll`), interval, URL, headers, output queue, dedup_key |
| `QueueItem` | struct | Queue payload: ID, AgentName, Payload (map), EnqueuedAt, AttemptCount |
| `AgentHooks` | struct | Optional shell hooks for pre-run, post-success, post-error |
| `Agent` | struct | Parsed agent definition: prompts, skills/toolsets, turn-scoped tool exposure, parameters, queue/cache/output/hook settings, source paths |
| `Job` | struct | Scheduler job record: agent name, status, last/next run, counts, last error |
| `Message` | struct | One turn in a session: role, content, tokens, tool-call metadata |
| `TokenBudget` | struct | Max tokens, completion reserve, summarization threshold |
| `LLMResponse` | struct | Model output: content, finish reason, usage, tool_calls |
| `Config` | struct | Resolved runtime config: logging, replay, persistence, tool dirs, default toolsets, MCP file |
| `SessionContext` | struct | Current conversation window: messages, token counts, metadata |
| `Turn` | struct | One prompt/response exchange in a run record |
| `RunTokens` | struct | Prompt, response, total token counts for one run |
| `RunTime` | struct | Start timestamp and duration for one run |
| `RunRecord` | struct | Completed run result: agent name, timing, status, tokens, prompts, error |
| `RunOptions` | struct | Per-invocation options such as targeted agent selection |

Rule: **never add behavior to `internal/model`.** Data shapes and enum
constants only. Logic belongs in the package that owns the behavior.

### `internal/agent`

Discovers, parses, validates, and merges agent definition files.

Key exported surfaces:

```go
// LoadDir reads all *.agent.md and *.lifecycle.yaml files from dir,
// merges lifecycle config into matching agent definitions, validates,
// and returns a sorted slice of Agent values. Errors are accumulated
// per-file; a partial list may be returned alongside errors.
func LoadDir(dir string) ([]model.Agent, []error)

// LoadFile parses a single agent definition file. Schedule and model
// are optional at the file level; they may be provided by a paired
// *.lifecycle.yaml file.
func LoadFile(path string) (model.Agent, error)

// Validate checks an Agent for required fields and consistency.
// Returns a list of validation errors (empty means valid).
func Validate(a model.Agent) []error
```

#### Loader phases

`LoadDir` runs four phases. (User-visible behavior is documented in
[AGENTS-AGENTDEF.md § Merge precedence](AGENTS-AGENTDEF.md#merge-precedence--agent--lifecycle).)

1. Parse all `*.agent.md` files → agent-name-keyed map.
2. Parse all `*.lifecycle.yaml` files → for each record, clone the
   matching agent def, set the job name, apply lifecycle fields
   (lifecycle wins on conflict).
3. Agent defs not referenced by any lifecycle file are included as-is
   (front-matter must then provide `schedule` and `model`).
4. Validate all resulting agents; return sorted by name.

Invariants enforced here:

- A lifecycle file referencing an unknown agent name is an error
  (record skipped, caller gets the error).
- An agent with no schedule in either source fails validation.
- Duplicate job names across all lifecycle records fail validation.
- Unknown front-matter or lifecycle YAML keys are silently ignored
  (forward compatibility).
- `schedule: "once"` agents run once at startup and are not re-queued
  (the scheduler enforces this; loader records the value).

### `internal/session`

Manages the conversation context window for a single agent execution.
Tracks token usage, triggers summarization when the budget threshold is
crossed, and exposes a clean interface to the rest of the system.

Key exported surfaces:

```go
// New returns a Session initialized with the given budget and LLM client.
func New(budget model.TokenBudget, modelName string, client LLMClient) *Session

// Add appends a message to the session, counting its tokens.
// If the resulting usage exceeds budget.SummarizeThreshold, it triggers
// a summarization pass before returning.
func (s *Session) Add(ctx context.Context, msg model.Message) error

// Messages returns the current ordered slice of messages in the window.
func (s *Session) Messages() []model.Message

// Usage returns current token count and remaining capacity.
func (s *Session) Usage() (used, remaining int)

// Snapshot returns a point-in-time copy of the session context window.
func (s *Session) Snapshot(metadata map[string]string) model.SessionContext

// Reset clears all messages but preserves the system prompt if present.
func (s *Session) Reset()
```

#### LLMClient interface

```go
// LLMClient is the interface leather uses to communicate with a model backend.
// Implementations: HTTPClient (production), MockLLM (tests).
type LLMClient interface {
    Complete(ctx context.Context, model string, messages []model.Message, opts CompletionOptions) (model.LLMResponse, error)
    CountTokens(messages []model.Message) (int, error)
}

// CompletionOptions carries per-call settings for a model request.
type CompletionOptions struct {
    MaxTokens   int
    Temperature float64
    // ExtraBody contains additional top-level fields merged verbatim into
    // the API request body. Use for model-specific parameters, e.g.
    // chat_template_kwargs for Qwen3 thinking-mode control.
    ExtraBody map[string]any
}
```

`HTTPClient` in `internal/session/http_client.go` targets any
OpenAI-compatible `/v1/chat/completions` endpoint.

`MockLLM` in `internal/session/mock_llm.go` is the test double; returns
configurable fixed responses and optionally tracks call history.

#### Summarization

When `s.Usage().used` crosses
`budget.SummarizeThreshold * budget.MaxTokens`:

1. All messages except the system prompt are extracted.
2. A summarization request is sent to the LLM asking for a concise
   narrative of the conversation so far.
3. The summary response replaces the extracted messages with a single
   `role: assistant` message marked `summarized: true` in its metadata.
4. The system prompt (if present, always the first message) is
   preserved.

Always transparent to callers: `Session.Add` returns only after the
window is safe.

> **Security note (Phase 4 of the refactor plan, cross-cutting C7):**
> the current concat-`role: content` summarization prompt is a
> prompt-injection vector. Migrate to a structured JSON transcript. See
> [AGENTS-SECURITY.md § Prompt-injection trust model](AGENTS-SECURITY.md#prompt-injection-trust-model).

---

## Dependency direction

```
internal/agent   →  internal/model
internal/session →  internal/model
internal/model   →  (stdlib only)
```

`internal/agent` must never import `internal/session`. The session
package is wired in by the runner (via the CLI). `internal/model` must
never import any other `internal/` package.

---

## Common mistakes

| Mistake | Correct approach |
|---|---|
| Adding logic to `internal/model` | `model` is data only; logic lives in the owning package. |
| Calling `LLMClient.Complete` from `internal/agent` | Agent loading is pure parsing; no LLM calls during load. |
| Panicking on unknown front-matter fields | Silently ignore unknown fields for forward compatibility. |
| Counting tokens on the completion response without adding it to usage | Every message received from the model must be counted. |
| Forgetting to count the summarization response's tokens | `Session.Add` must count the summary message it stores. |
| Storing raw conversation content in logs | Log session ID and turn count; never message content. |
| Documenting `*.agent.md` syntax here | That belongs in [AGENTS-AGENTDEF.md](AGENTS-AGENTDEF.md); CORE owns the loader. |

---

## Verification checklist

Before opening a PR touching this domain:

- [ ] `go test ./internal/agent/... ./internal/session/... ./internal/model/...` passes
- [ ] `go vet ./...` is clean
- [ ] New exported symbols have doc comments
- [ ] `MockLLM` is used in all session tests; no real network calls
- [ ] Agent definitions in `testdata/` cover both valid and invalid inputs
- [ ] Token counting is symmetric: counted on `Add` for both user and model messages
- [ ] `internal/model` has no new behavior methods (data only)
- [ ] If user-visible field added: [AGENTS-AGENTDEF.md](AGENTS-AGENTDEF.md) updated in the same PR

---

_Last reviewed: 2026-05-19_

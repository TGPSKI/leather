# AGENTS-RUNTIME.md — leather agent execution runtime

Subagent guide for the runtime domain: multi-round tool execution, skill and
toolset registry, MCP runtime, response caching, output routing, and bot
messaging backends.

Load this guide when working on `internal/runner`, `internal/tool`,
`internal/mcp`, `internal/cache`, or `internal/notify`. For neighbouring
domains, consult the routing table in [AGENTS.md](../AGENTS.md).

---

## Package responsibilities

### `internal/runner`

Executes a single agent invocation: builds the session, drives the tool-call
loop, reads from cache, routes output, and returns a `model.RunRecord`.

Key exported surfaces:

```go
// Runner executes agents with optional tool calling support.
type Runner struct {
  Client        session.LLMClient
  Registry      *tool.Registry
  Log           *logging.Logger
  MaxToolRounds int
  Cache         *cache.FileCache
  QueueMgr      *queue.Manager
  Notifiers     map[string]notify.Notifier
  MCPRegistry   *mcp.Registry
  ProgressFn    func(ProgressEvent)
  Vars          map[string]string
}

// Run executes agent a using the given token budget.
// It handles cache lookup, the LLM + tool-call loop, cache write, and output routing.
// Returns a RunRecord; errors are wrapped with "runner/Run: ...".
func (r *Runner) Run(ctx context.Context, a model.Agent, budget model.TokenBudget) (model.RunRecord, error)

// ExpandPromptPayload applies text/template substitution to the agent's
// SystemPrompt and UserPrompt using payload as the template data.
// Uses missingkey=zero: unknown variables render as empty string.
func ExpandPromptPayload(a model.Agent, payload map[string]any) (model.Agent, error)
```

#### Tool-call loop

`Runner.Run` drives the tool loop:

1. Resolve the base tool scope from `a.Skills` and `a.Toolsets`; append `system_prompt_append` text from the active skills.
2. If `a.Cache.Enabled`, compute the cache key after prompt augmentation and return the cached RunRecord on hit.
3. Build a `session.Session`; add the system prompt and the user prompt chain (after template expansion).
4. On each user turn, if `TurnSkills`, `TurnToolsets`, or `TurnTools` are declared, replace tool exposure with that turn scope.
5. Call `LLMClient.Complete`; add the response to session.
6. If the response contains `tool_calls`, execute each via `tool.Executor` (HTTP or MCP), add results, and repeat up to the agent/tool default round limit.
7. On completion, write to cache (if enabled) and dispatch output routes.

**Security**: tool names returned by the model are validated against the registry.
Unknown names are logged and skipped — never executed. This prevents prompt
injection from triggering arbitrary HTTP calls.

#### Output routing

`routeOutput` dispatches on `route.Type`:

| Type | Behavior |
|---|---|
| `file` | Writes content to path (template-expanded); creates parent dirs; mode 0600 |
| `queue` | Enqueues a `QueueItem` to the named queue via `QueueMgr.Enqueue` |
| `http` | Sends plain-text content to URL using `route.Method` (default `POST`); headers are applied verbatim |
| `notify` | Calls `Notifiers[route.NotifyBackend].Send(ctx, msg)`; unknown backend is warn+continue |

Routing errors are non-fatal: logged as `warn`, remaining routes continue.

---

### `internal/tool`

Loads `*.skill.yaml` and `*.toolset.yaml` files into a `Registry`; executes
HTTP and MCP tool calls on behalf of the runner.

Key exported surfaces:

```go
// Registry holds all loaded skills and their tools.
type Registry struct { ... }

// NewRegistry returns an empty Registry.
func NewRegistry() *Registry

// Load reads all *.skill.yaml and *.toolset.yaml files from dir and returns a populated Registry.
// Returns an error if any file fails to parse or has duplicate names / invalid references.
func Load(dir string) (*Registry, error)

// Register adds a skill to the registry.
// Returns an error if any tool name in the skill duplicates an existing tool.
func (r *Registry) Register(s model.Skill) error

// RegisterToolset adds a named toolset after validating that every referenced tool exists.
func (r *Registry) RegisterToolset(s model.Toolset) error

// GetTool returns a single tool definition by exact name.
func (r *Registry) GetTool(name string) (model.ToolDefinition, bool)

// GetToolset returns a named Toolset.
func (r *Registry) GetToolset(name string) (model.Toolset, bool)

// GetTools returns the tool definitions for all named skills (union, deduplicated).
// Unknown skill names are silently skipped.
func (r *Registry) GetTools(skillNames []string) []model.ToolDefinition

// ResolveTools returns the combined tool definitions for skills, toolsets,
// and explicit tool names, in that order.
func (r *Registry) ResolveTools(skillNames, toolsetNames, toolNames []string) []model.ToolDefinition

// Executor dispatches HTTP- and MCP-backed tools.
// QueueMgr enables the outbound DLQ: permanent/exhausted failures are
// enqueued to "outbound-dlq" when this field is non-nil.
// Limiter enforces per-host token-bucket rate limits before each attempt.
type Executor struct {
    MCP       *mcp.Registry
    QueueMgr  *queue.Manager  // nil = outbound DLQ disabled
    AgentName string           // injected by Runner; stored in DLQ items
    Limiter   *HostLimiter     // nil = no rate limiting
}

// Execute runs a single tool call. Always returns a ToolResult; never panics.
// On failure, ToolResult.Error is set and Content may be empty.
// With def.Retry.MaxAttempts > 0, transient failures (5xx, 429, network)
// are retried with exponential backoff + jitter.
func (e *Executor) Execute(ctx context.Context, def model.ToolDefinition, args map[string]any) model.ToolResult

// MetricSnapshot returns a point-in-time snapshot of outbound tool counters.
// Counters are process-lifetime atomics; they reset only on restart.
func MetricSnapshot() (retryTotal, backoffTotal, rateLimitWaitTotal int64)
```

#### Per-tool retry policy

`ToolDefinition.Retry` (`model.ToolRetryConfig`) controls retry behaviour:

| Field | Default | Meaning |
|---|---|---|
| `max_attempts` | 1 (no retry) | Total attempts including the initial one. |
| `base_delay` | `1s` | Initial backoff; doubles each attempt. |
| `max_delay` | `30s` | Backoff ceiling before jitter. |
| `honor_retry_after` | `true` | Use `Retry-After` header value when present. |

A zero `ToolRetryConfig` (all fields at zero value) means **single attempt, no
retry** — preserving backward compatibility for tools that predate the policy.
Only tools with an explicit `retry:` block in their skill YAML get the retry loop.

`isTransient` classifies the failure to decide whether to retry:
- **Transient** → retry: 429, 500, 502, 503, 504; network/timeout errors;
  403 with `X-RateLimit-Remaining: 0`
- **Permanent** → return immediately without retrying: all other 4xx

#### Outbound DLQ

When `Executor.QueueMgr != nil` and a tool call either:
- exhausts its retry budget on transient errors, or
- fails immediately with a permanent error,

a `model.QueueItem` is enqueued to the well-known `"outbound-dlq"` queue.
DLQ items carry `ToolName`, `ToolTarget`, and the last error in `Payload`.
DLQ enqueue is a fire-and-forget side-effect; failure to enqueue is logged
at `warn` and does not affect the returned `ToolResult`.

Items in `outbound-dlq` can be inspected and requeued via `leather dlq`.

#### Skill file format (`*.skill.yaml`)

```yaml
name: github-issue-manager
system_prompt_append: |
  You have access to the GitHub API.
tools:
  - name: github_list_issues
    description: List open issues for a GitHub repository
    type: http
    http:
      method: GET
      url: "https://api.github.com/repos/{{.repo}}/issues"
      headers:
        Authorization: "Bearer {{env:GITHUB_TOKEN}}"
        Accept: application/vnd.github+json
    retry:
      max_attempts: 3
      base_delay: 1s
      max_delay: 30s
      honor_retry_after: true
```

#### Toolset file format (`*.toolset.yaml`)

```yaml
name: release-read
description: Read-only release inspection tools
tools:
  - git_status
  - changelog_has_version
```

`Load` registers skills first, then validates toolsets in a second pass so
every toolset reference points at an already-known tool.

#### Tool execution

`Executor.Execute` dispatches on `def.Type`:
- `http` (or empty) → `execHTTP`
- `mcp` → `execMCP` through a started `mcp.Registry`

HTTP execution (`execHTTP`):
- Calls `e.Limiter.Wait(ctx, host)` before each attempt; blocks until the
  per-host token bucket allows the request or ctx is cancelled.
- Expands URL template with tool call arguments (`{{.field}}`).
- Expands `{{env:VAR}}` in header values; **never logs auth header values**.
- Sends the request with the runner's context (inherits timeout).
- Response body is capped at 1 MB.
- Non-2xx responses populate `ToolResult.Error` with the status code message.
- Transient failures are retried up to `def.Retry.MaxAttempts` times with
  exponential backoff; permanent failures return immediately.

MCP execution (`execMCP`):
- Looks up the named server in the running registry.
- Calls the remote tool with JSON-RPC `tools/call` over stdio.
- Returns joined text content; server or transport errors populate `ToolResult.Error`.

Tool names must match `^[a-z][a-z0-9_]*$`. Duplicate names across skills are
a load error — fail closed.

---

### `internal/mcp`

Loads MCP server configs, starts long-lived stdio JSON-RPC clients, and
provides named access for `mcp`-type tool execution.

Key exported surfaces:

```go
// LoadServers parses mcp-servers.yaml. Missing files return an empty slice.
func LoadServers(path string) ([]model.MCPServerConfig, error)

// Registry manages configured MCP server clients.
type Registry struct { ... }

// NewRegistry creates a Registry from server configs; clients are not started yet.
func NewRegistry(configs []model.MCPServerConfig) *Registry

// StartAll starts every configured server and runs the initialize handshake.
func (r *Registry) StartAll(ctx context.Context) error

// Get returns a started client by server name.
func (r *Registry) Get(name string) (*Client, bool)

// StopAll best-effort stops all running server processes.
func (r *Registry) StopAll()

// Start launches one server process and completes the MCP initialize handshake.
func Start(ctx context.Context, cfg model.MCPServerConfig) (*Client, error)

// Call invokes a named remote tool on the MCP server.
func (c *Client) Call(ctx context.Context, toolName string, args map[string]any) (string, error)

// Close stops the underlying MCP server process.
func (c *Client) Close() error
```

Only stdio transport is supported. `StartAll` is best-effort: the CLI logs
startup failures and continues with the subset of servers that did start.

---

### `internal/cache`

SHA-256 keyed file cache for agent responses. Prevents redundant LLM calls
when the same agent, prompts, and model repeat within a TTL window.

Key exported surfaces:

```go
// AgentRunKey returns a SHA-256 hex key for the given agent inputs.
// Uses NUL-separated concatenation to prevent boundary-collision attacks.
func AgentRunKey(agentName, systemPrompt, userPrompt, model string) string

// FileCache stores and retrieves cached response strings on disk.
type FileCache struct { ... }

// NewFileCache creates (or opens) a cache directory.
// Returns an error if the directory cannot be created or is not writable.
func NewFileCache(dir string) (*FileCache, error)

// Get returns a cached value for key and true, or ("", false) if missing/expired.
// TTL expiry is checked lazily on read; expired entries are silently skipped.
func (c *FileCache) Get(key string) (string, bool)

// Set writes value to the cache under key with the given TTL.
// Zero TTL means the entry never expires.
// Writes are atomic: write to temp file, then rename to final path.
// Files are created with mode 0600.
func (c *FileCache) Set(key, value string, ttl time.Duration) error
```

Cache entries are stored as JSON files named `<sha256hex>.json` in the
cache directory. Each file is mode 0600. The `Runner` checks the cache
after skill/prompt resolution but before the first LLM call.

---

### `internal/hide` — HideBuffer (runtime component)

> `internal/hide` has two distinct halves:
> - **`buffer.go`** — in-process runtime buffer owned by this guide.
> - **`store.go`** — disk-backed hide storage owned by [AGENTS-TANNERY.md](AGENTS-TANNERY.md).

`HideBuffer` is the in-process runtime store for one hide. It holds raw bytes
in memory and serves paged `Hidecut` slices to agents, ensuring the session
context window never owns the full content at once.

Key exported surfaces from `internal/hide/buffer.go`:

```go
// NewBuffer wraps raw bytes in a HideBuffer for paged access.
// pageSize controls how many bytes each Cut returns.
func NewBuffer(content []byte, pageSize int) *HideBuffer

// Len returns the total byte length of the hide.
func (b *HideBuffer) Len() int

// Pages returns the total number of pages.
func (b *HideBuffer) Pages() int

// Cut returns page i (0-indexed) of the hide with a navigation envelope.
// Returns (cut, true) when page i is valid; (zero, false) when out of range.
func (b *HideBuffer) Cut(page int) (model.Hidecut, bool)
```

`model.Hidecut` fields:
- `Content string` — UTF-8 content for this page
- `Page int` — 0-indexed page number
- `TotalPages int` — total pages in this hide
- `HideID string` — originating hide ID

`Runner` receives a `*HideBuffer` when a curing pipeline calls `buildRunner(buf)`.
It injects `Cut(0)` into the agent's first user message and exposes a
`get_hide_cut` tool for subsequent pages if needed.

---

### `internal/notify`

Delivers agent output to external messaging backends via a clean interface.
Adding a new backend (Matrix, ntfy, email) requires only a new file and an
entry in the `New` factory — no changes to runner or CLI.

> **Authoring patterns** (how to add a new notifier, MCP server, or
> webhook integration, including failure-mode catalogs and secret
> rules) live in [AGENTS-INTEGRATIONS.md](AGENTS-INTEGRATIONS.md).
> This section covers the **interface** the runtime calls; that guide
> covers the **author-facing workflow**.

Key exported surfaces:

```go
// Message is the payload sent to a messaging backend after each successful run.
type Message struct {
    AgentName string
    Content   string
    Tags      []string
    Timestamp time.Time
}

// Notifier sends agent output to a messaging backend.
type Notifier interface {
    Send(ctx context.Context, msg Message) error
    Name() string   // backend config name; used for log context
}

// New constructs the correct backend for cfg.Type ("telegram" or "signal").
// Resolves secrets at construction time. Returns an error if validation fails.
func New(cfg model.NotifyBackendConfig) (Notifier, error)

// BuildMap constructs a name-keyed Notifier map from a slice of configs.
// Backends that fail to initialize are collected in errs; the rest are returned.
// Callers should log each error and continue — partial initialization is valid.
func BuildMap(cfgs []model.NotifyBackendConfig) (map[string]Notifier, []error)
```

#### Secret resolution (`secret.go`)

Resolution order for `model.SecretRef`:

1. **`pass show <path>`** — runs via `os/exec` with a 5 s timeout; first line
   of stdout only (strips pass metadata lines). `exec.LookPath("pass")` first;
   if not in PATH, falls through to env var with a `warn` log.
2. **`os.Getenv(name)`** — env var fallback.
3. If both are empty or both fail: returns an error (fail closed).

Secret values are **never** logged. Env var names are logged at `debug`.
`pass` invocation arguments (path only) are logged at `debug`.

#### Telegram backend

- `POST https://api.telegram.org/bot<token>/sendMessage`
- JSON body: `{"chat_id": "...", "text": "...", "parse_mode": "Markdown"}`
- Message format: `*[agentName]* (tag1, tag2)\n\n<content>`, truncated to 4096 bytes (UTF-8-safe)
- On HTTP 429: reads `Retry-After` header, sleeps, retries once
- Timeout: 15 s per request

#### Signal backend

- `POST <api_url>/v2/send` (default `http://127.0.0.1:8080`)
- JSON body: `{"message": "...", "number": "+1...", "recipients": ["+1..."]}`
  or `{"message": "...", "number": "+1...", "group_id": "..."}` for groups
- Optional `Authorization: Bearer <api-key>` header when token is configured
- API key is **optional**: skip `SecretRef.Resolve` when both `Pass` and `Env` are empty
- Timeout: 15 s per request

---

## Dependency direction

```
internal/runner  →  internal/session, internal/tool, internal/cache,
                    internal/queue, internal/notify, internal/model, internal/logging
internal/tool    →  internal/mcp, internal/model
internal/mcp     →  internal/model
internal/cache   →  internal/model (via key.go), stdlib (encoding/json, os)
internal/notify  →  internal/model, stdlib (net/http, encoding/json, os/exec)
```

`internal/runner` is the only package that wires all runtime components together.
Do not import `internal/runner` from `internal/tool`, `internal/cache`, or
`internal/notify`.

---

## Common mistakes

| Mistake | Correct approach |
|---|---|
| Executing a tool call without validating the name against the registry | Always check `registry.GetTool(name)` first; log and skip unknowns |
| Logging resolved secret values | Log only the env var name or pass path, never the value |
| Writing cache entries with permissions wider than 0600 | `FileCache.Set` uses `os.WriteFile(path, data, 0600)` with atomic rename |
| Returning an error from `Execute` for non-2xx HTTP | Return a `ToolResult` with `Error` set; `Execute` never returns an error |
| Skipping context cancellation in tool HTTP calls | Always use `http.NewRequestWithContext(ctx, ...)` |
| Exposing `mcp` tools without a started registry | Build an `mcp.Registry`, call `StartAll`, and pass it into `tool.Executor` / `runner.Runner` |
| Adding business logic to `internal/cache` | Cache is get/set only; TTL and key logic stay in caller (runner) |
| Hard-coding `pass` binary path | Use `exec.LookPath("pass")`; fail gracefully if absent |

---

## Verification checklist

Before opening a PR touching this domain:

- [ ] `go test ./internal/runner/... ./internal/tool/... ./internal/mcp/... ./internal/cache/... ./internal/notify/...` passes
- [ ] `go test -race ./...` is clean
- [ ] Tool name validation test covers unknown-name rejection
- [ ] Toolset resolution tests cover duplicate names and unknown references
- [ ] Cache tests use `t.TempDir()`; no hardcoded temp paths
- [ ] Notify tests use `httptest.NewServer`; no real API calls
- [ ] MCP tests cover initialize handshake and tool-call error propagation
- [ ] Secret values do not appear in any log assertion in tests
- [ ] New output route types have a test in `runner_test.go`

---

_Last reviewed: 2026-05-19_

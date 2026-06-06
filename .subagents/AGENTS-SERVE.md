# AGENTS-SERVE.md — leather serving, config, and CLI

Subagent guide for the serving domain: config loading, CLI subcommands,
schema validation, flag/env wiring, the HTTP API, and the `cmd/leather`
entrypoint.

Load this guide when working on `internal/config`, `internal/cli`,
`internal/schema`, `internal/secret`, `internal/devtools`, or
`cmd/leather`. For neighbouring domains, consult the routing table in
[AGENTS.md](../AGENTS.md).

---

## Package responsibilities

### `cmd/leather`

Thin entrypoint only. Must contain no business logic.

```go
// main.go
func main() {
    os.Exit(cli.Run(os.Args[1:], os.Stdout, os.Stderr, version, commit))
}
```

`cli.Run` returns an `int` exit code. `os.Exit` is called only here.

### `internal/cli`

Subcommand dispatch, flag parsing, and the serve loop's signal handling.

Each subcommand has:
- Its own `flag.FlagSet` with `flag.ContinueOnError`
- A dedicated `Run*` function that returns `int`
- Flag registration via the config package's wiring helpers

#### Subcommand reference

| Subcommand | Function | Purpose |
|---|---|---|
| `doctor` | `RunDoctor` | Print effective config values with source attribution; redact secrets |
| `init` | `RunInit` | Scaffold a new project directory with config, agent, and Makefile |
| `serve` | `RunServe(args, stdout, stderr, version, commit)` | Start scheduler loop + optional HTTP API |
| `chat` | `RunChat` | Interactive multi-turn chat session with a named agent |
| `run` | `RunOnce` | Load and execute a single agent, then exit |
| `validate` | `RunValidate` | Parse and validate agent files; report errors |
| `test-agent` | `RunTestAgent` | Execute an agent with `MockLLM` and print the turn transcript |
| `dlq` | `RunDLQ` | Inspect and requeue outbound dead-letter queue items |
| `status` | `RunStatus` | Print scheduler state, job history, token usage |
| `ingest` | `RunIngest` | Store raw bytes as a hide and optionally enqueue for curing |
| `snapshot` | `RunSnapshot` → `RunSnapshotSave` / `RunSnapshotRestore` | Save or restore a `tar.gz` point-in-time archive of runtime state |
| `attach` | `RunAttach` | Join a running `serve` instance and stream pretty-printed DevTools events |
| `version` | `RunVersion` | Print version, commit, Go runtime version |

`cli.Run` also accepts `help`, `--help`, and `-h`, which print `usage`
directly without a dedicated `RunHelp` function.

`serve` is the primary operating mode. It:
1. Loads config via `internal/config`
2. Discovers agent definitions via `internal/agent`
3. Registers jobs with `internal/scheduler`
4. Starts the scheduler loop in a goroutine
5. Initializes tool, queue, cache, worker, notify, and MCP runtime dependencies
6. When `--tannery` is set, calls `initTannery` (in `internal/cli/api_tannery.go`) to load tannery config, acquire the single-process hide-dir lock, spin up the curing supervisor, and register tannery HTTP handlers
7. Blocks on `os.Signal` (SIGINT/SIGTERM) for graceful shutdown

#### Graceful shutdown contract

On SIGINT or SIGTERM, `RunServe` must:
1. Cancel the root context (stops the scheduler loop and all running jobs)
2. Call `scheduler.Drain(timeout)` to wait for in-flight jobs to finish
3. Return 0 on clean exit, 1 if drain timed out

Do not call `os.Exit` inside `RunServe`. Return the exit code.

### `internal/scheduler`

See [AGENTS-WORKER.md](AGENTS-WORKER.md) — the scheduler package moved to
the worker domain guide after Phase 2 added `internal/queue` and
`internal/worker`. Scheduling, concurrency control, and cron parsing are
documented there.

### `internal/config`

Loads and merges configuration from (in priority order, highest to lowest):
1. CLI flags on the current subcommand's `flag.FlagSet`
2. Environment variables (`LEATHER_*`)
3. YAML config file (`--config` / `LEATHER_CONFIG` / `~/.leather/config.yaml`)
4. Built-in defaults

Returns a `model.Config`. Config loading must not require a network call or
any external dependency.

#### YAML parsing

YAML is parsed using a minimal stdlib-only approach:
- `internal/config/yaml.go` implements a line-oriented YAML reader covering
  the subset leather needs (scalars, lists, nested maps, quoted strings).
- It does not support anchors, aliases, or multi-document streams.
- Unknown keys are silently ignored for forward compatibility.

For complex nested config values, use JSON as the serialization target and
parse with `encoding/json` after the YAML-to-JSON bridge converts the input.

### `internal/schema`

Flat-schema validation layer used by `leather validate` for config, agent,
lifecycle, skill, worker, and MCP server YAML before deeper parser checks.

Key exported surfaces:

```go
// Violation is one schema validation failure.
type Violation struct {
  Field   string
  Message string
}

func ValidateAgentFrontmatter(src string) []Violation
func ValidateLifecycleYAML(src string) []Violation
func ValidateConfigYAML(src string) []Violation
func ValidateSkillYAML(src string) []Violation
func ValidateWorkerYAML(src string) []Violation
func ValidateMCPServersYAML(src string) []Violation
```

`internal/schema` validates only the flat scalar/list surface for each file
type. Nested blocks stay owned by the dedicated parsers in `internal/agent`,
`internal/config`, `internal/tool`, `internal/worker`, and `internal/mcp`.

#### Complete flag and env-var table

Every flag must be registered here and in the relevant subcommand's `FlagSet`.
Update this table whenever a flag is added or removed.

| Flag | Env var | Default | Description |
|---|---|---|---|
| `--config` | `LEATHER_CONFIG` | `~/.leather/config.yaml` | Config file path |
| `--agent-dir` | `LEATHER_AGENT_DIR` | `~/.leather/agents/` | Directory to scan for `*.agent.md` and `*.lifecycle.yaml` files |
| `--model` | `LEATHER_MODEL` | _(none)_ | Global default model name used when the agent definition omits one |
| `--temperature` | `LEATHER_TEMPERATURE` | `0.7` | Global default sampling temperature |
| `--log-level` | `LEATHER_LOG_LEVEL` | `info` | Log verbosity: `debug`, `info`, `warn`, `error` |
| `--log-format` | `LEATHER_LOG_FORMAT` | `text` | Log format: `text`, `json` |
| `--max-tokens` | `LEATHER_MAX_TOKENS` | `8192` | Global token budget ceiling |
| `--completion-reserve` | `LEATHER_COMPLETION_RESERVE` | `1024` | Tokens reserved for model completion |
| `--summarize-threshold` | `LEATHER_SUMMARIZE_THRESHOLD` | `0.85` | Fraction of budget that triggers summarization |
| `--llm-endpoint` | `LEATHER_LLM_ENDPOINT` | `http://localhost:11434` | Base URL of OpenAI-compatible LLM endpoint |
| `--llm-timeout` | `LEATHER_LLM_TIMEOUT` | `60s` | Timeout for a single LLM call |
| `--scheduler-tick` | `LEATHER_SCHEDULER_TICK` | `1m` | Scheduler wake-up interval |
| `--max-concurrent-jobs` | `LEATHER_MAX_CONCURRENT_JOBS` | `4` | Max simultaneous scheduler job handlers |
| `--run-duration` | `LEATHER_RUN_DURATION` | `0` | Exit cleanly after this duration (`0` = run until signal) |
| `--max-jobs` | `LEATHER_MAX_JOBS` | `0` | Exit cleanly after this many completed jobs (`0` = unlimited) |
| `--state-dir` | `LEATHER_STATE_DIR` | `~/.leather/.state/` | Directory for job state persistence |
| `--api` | `LEATHER_API` | `false` | Enable HTTP status API when running `serve` |
| `--api-addr` | `LEATHER_API_ADDR` | `127.0.0.1:7749` | Bind address for the HTTP API (loopback only by default) |
| `--log-file` | `LEATHER_LOG_FILE` | _(none)_ | Write full structured logs to file; tees with stderr unless `--pretty` |
| `--pretty` | `LEATHER_PRETTY` | `false` | Render agent turns to console; suppress structured log from console |
| `--pretty-mode` | `LEATHER_PRETTY_MODE` | `all` | Pretty console layout: `all` shows tools + messages, `messages` shows transcript only |
| `--stats` | `LEATHER_STATS` | `false` | Show per-turn token counts and a summary at shutdown |
| `--tokens-per-turn` | `LEATHER_TOKENS_PER_TURN` | `false` | In pretty mode, print token usage after each individual turn |
| `--persist-runs` | `LEATHER_PERSIST_RUNS` | `false` | Persist run records to JSONL files |
| `--run-history-dir` | `LEATHER_RUN_HISTORY_DIR` | _(empty; `serve` uses `<state-dir>/runs`)_ | Directory for per-agent JSONL run logs |
| `--run-max-bytes` | `LEATHER_RUN_MAX_BYTES` | `10485760` | Rotate a run-log file after this many bytes |
| `--replay` | `LEATHER_REPLAY` | _(none)_ | Start in snapshot replay mode using a JSON file |
| `--replay-live` | `LEATHER_REPLAY_LIVE` | _(none)_ | Start in live replay mode from a JSONL runs directory |
| `--replay-speed` | `LEATHER_REPLAY_SPEED` | `1.0` | Replay speed multiplier for `--replay-live` |
| `--tool-dir` | `LEATHER_TOOL_DIR` | _(none)_ | Directory containing `*.skill.yaml` and `*.toolset.yaml` files |
| `--default-toolsets` | `LEATHER_DEFAULT_TOOLSETS` | _(none)_ | Comma-separated baseline toolsets applied to every agent before agent-specific scope |
| `--max-tool-rounds` | `LEATHER_MAX_TOOL_ROUNDS` | `5` | Maximum LLM + tool-call cycles per agent run |
| `--worker-dir` | `LEATHER_WORKER_DIR` | _(none)_ | Directory containing `*.worker.yaml` worker definitions |
| `--cache-dir` | `LEATHER_CACHE_DIR` | _(empty; `serve` uses `<state-dir>/cache`)_ | Directory for SHA-256 keyed response cache files |
| `--mcp-servers-file` | `LEATHER_MCP_SERVERS_FILE` | _(empty; run/serve/validate use `~/.leather/mcp-servers.yaml`)_ | Path to `mcp-servers.yaml` |
| `--loop` | `LEATHER_LOOP` | `1` | Repeat `leather run` N times |
| `--tannery` | `LEATHER_TANNERY` | _(empty)_ | Path to `tannery.yaml`; enables tannery mode (see below) |

#### Config file schema (YAML)

```yaml
agent_dir: ~/.leather/agents/
model: ""                   # global default model; overridden per-agent
temperature: 0.7
log_level: info
log_format: text
log_file: ""                # path to log file; empty = stderr only
pretty: false
pretty_mode: all            # pretty layout: all or messages
stats: false
tokens_per_turn: false      # pretty-mode per-turn token lines
persist_runs: false         # persist RunRecord JSONL files
run_history_dir: ""         # empty = <state_dir>/runs
run_max_bytes: 10485760     # rotate JSONL files after this many bytes
replay: ""                  # snapshot replay JSON file
replay_live: ""             # live replay JSONL directory
replay_speed: 1.0           # playback speed multiplier for replay_live
max_tokens: 8192
completion_reserve: 1024
summarize_threshold: 0.85
llm_endpoint: http://localhost:11434
llm_timeout: 60s
scheduler_tick: 1m
max_concurrent_jobs: 4
run_duration: 0             # duration string, e.g. "24h"; 0 = unlimited
max_jobs: 0                 # positive int; 0 = unlimited
state_dir: ~/.leather/.state/
api: false
api_addr: "127.0.0.1:7749"
tool_dir: ""                # local *.skill.yaml + *.toolset.yaml dir; empty = skip
default_toolsets: []        # baseline toolsets for every agent
max_tool_rounds: 5
worker_dir: ""              # *.worker.yaml dir; empty = skip
cache_dir: ""               # empty = serve uses <state_dir>/cache
mcp_servers_file: ""        # empty = run/serve/validate use ~/.leather/mcp-servers.yaml
loop: 1                     # repeat leather run N times
tannery: ""                 # path to tannery.yaml; empty = tannery disabled

tools:
  rate_limits:              # per-host token-bucket rate limits for outbound tool calls
    api.github.com: "60/m"  # format: "N/s", "N/m", or "N/h"
    api.example.com: "10/s"
```

`tools.rate_limits` is a nested map. Each key is a hostname (no port, no
scheme); the value is a rate spec in the form `N/<unit>` where unit is `s`
(seconds), `m` (minutes), or `h` (hours). The second call to the same host
within the interval blocks until the next token is available. Unknown hosts
pass through immediately with no limiting.

YAML keys are the snake_case equivalents of the flag names (strip `--`,
replace `-` with `_`).

---

## Tannery mode (`--tannery`)

When `--tannery <path>` is supplied, `serve` activates the event-driven
curing pipeline in addition to its normal scheduler loop. Tannery mode is
documented fully in [AGENTS-TANNERY.md](AGENTS-TANNERY.md); the serve-layer
responsibilities are:

### `initTannery` / `drainTannery` (`internal/cli/api_tannery.go`)

`initTannery(cfg, deps, mux, log)` performs:
1. Loads `tannery.yaml` from `cfg.TanneryFile` via `config.LoadTannery`.
2. Acquires a `syscall.Flock`-based exclusive lock on `<hide_dir>/leather.lock`
   (single-process enforcement — fails immediately if already locked).
3. Creates `hide.NewStore(hideDir)`, `artifact.NewStore(artDir)`, `queue.NewManager(queueDir)`.
4. Creates `curing.NewRouter(routes)` and `curing.NewSupervisor(...)` to drive workers.
5. Calls `curingSupv.Start(ctx)` to begin processing queue items.
6. Registers tannery HTTP handlers on the shared mux.

`drainTannery(deps)` calls `curingSupv.Drain()` and releases the flock.

### Tannery HTTP endpoints

All endpoints live in `internal/cli/api_tannery.go`.

| Endpoint | Method | Handler | Description |
|---|---|---|---|
| `/webhooks/{name}` | POST | `makeWebhookHandler` | Receives an event, validates optional HMAC secret, writes a hide, enqueues item |
| `/hides` | GET | `handleHidesCollection` | List all hides (JSON) |
| `/hides/{id}` | GET | `dispatchHide` (detail) | Get hide metadata |
| `/hides/{id}` | DELETE | `dispatchHide` (delete) | Delete a hide |
| `/hides/{id}/cuts/{page}` | GET | `dispatchHide` (cut) | Page into a hide; returns `model.Hidecut` |
| `/artifacts` | GET | `handleArtifactsCollection` | List artifacts; optional `?curing=` filter |
| `/artifacts/{id}` | GET | `dispatchArtifact` | Get one artifact by ID |
| `/curings` | GET | `handleCurings` | List curing definitions with queue depth |
| `/intake` | POST | `handleIntake` | Direct-ingest endpoint; writes hide from request body |

### `leather dlq` subcommand (`internal/cli/cmd_dlq.go`)

`RunDLQ(args, stdout, stderr)` dispatches `inspect` and `requeue`:

```
leather dlq inspect [--queue outbound-dlq] [--state-dir ...]
leather dlq requeue [--queue outbound-dlq] [--work-queue <name>] [--state-dir ...] <item-id>
```

- **`inspect`** — lists all items in the DLQ; prints `ID | tool | agent | attempt | enqueued_at | error`.
- **`requeue`** — moves the named item from the DLQ to `--work-queue`, resetting
  `AttemptCount` to 0 so it gets a fresh retry budget. Default `--work-queue` is
  the DLQ name with the `-dlq` suffix stripped.

**Important**: `<item-id>` must come **after** all flags. Go's `flag.FlagSet`
stops parsing at the first non-flag token, so placing `<item-id>` before flags
silently ignores the remaining flags.

### `leather ingest` subcommand (`internal/cli/cmd_ingest.go`)

`RunIngest(args, stdin, stdout, stderr)` — reads body from `--file` or stdin,
POSTs to the running leather instance's `/intake` endpoint, and prints the
returned hide ID. Flags: `--tannery`, `--kind`, `--file`.

---

## HTTP API (optional, `--api`)

When `--api` is set, `serve` starts a minimal HTTP server on `--api-addr`.
The API is for observability only — it does not accept commands that mutate
scheduler state at runtime (v1).

| Endpoint | Method | Response |
|---|---|---|
| `/queues` | GET | JSON array of all queue names and their current lengths |
| `/queues/{name}` | GET | Queue detail: name, length, head item (if non-empty); 404 if queue not found |
| `/healthz` | GET | `{"status":"ok"}` — liveness probe |
| `/jobs` | GET | JSON array of current `model.Job` snapshots |
| `/jobs/{name}` | GET | Single job record by agent name; 404 if not found |
| `/status` | GET | Server status: `started_at`, `uptime_seconds`, `version`, `commit`, `llm_endpoint`, `agent_count`, `scheduler_tick`, `max_concurrent_jobs` |
| `/config` | GET | Sanitised config (explicit allowlist, not raw `model.Config`): `agent_dir`, `log_level`, `log_format`, `model`, `temperature`, `max_tokens`, `completion_reserve`, `summarize_threshold`, `llm_endpoint`, `llm_timeout`, `scheduler_tick`, `max_concurrent_jobs`, `api_addr` |
| `/metrics` | GET | Per-agent aggregated stats + recent run history: `{"agents":{"name":{run_count, error_count, total_prompt_tokens, total_completion_tokens, avg_duration_ms, recent_runs:[…]}}}` |
| `/history` | GET | All recent runs merged across agents, sorted `started_at` desc, capped at 500. Returns `[]model.RunRecord`. |

All endpoints return `Content-Type: application/json`. CORS headers
(`Access-Control-Allow-Origin: *`) are always added when `--api` is
enabled, allowing the `ui/index.html` to be opened directly from the
filesystem without a same-origin server.

The HTTP server uses `net/http` (stdlib only). No router library. Handler
functions are registered directly on `http.NewServeMux()`. The mux is
wrapped by `corsMiddleware` before being passed to `http.Server`.

All dependencies are injected via the unexported `apiDeps` struct:
```go
type apiDeps struct {
    sched     *scheduler.Scheduler
    metrics   *runMetrics   // per-agent ring buffer, 200 runs/agent
    cfg       model.Config
    startedAt time.Time
    version   string
    commit    string
    log       *logging.Logger
}
func apiMux(deps apiDeps) http.Handler
```

Security: the API binds to loopback by default (`127.0.0.1`). If `--api-addr`
is set to a non-loopback address, `RunServe` logs a prominent warning. There
is no authentication in v1 — document this limitation clearly in help text.

### Run history (`runMetrics`)

Per-agent run history is tracked entirely in-process. Each completed handler
invocation appends a `model.RunRecord` to the agent's ring buffer (most-recent
first, capped at 200 records per agent). No disk persistence in v1; history
is lost on restart. The `runMetrics` type lives in `internal/cli/cmd_serve.go`.

### UI (`ui/`)

A browser-side dashboard ships in `ui/` at the repo root. It consumes the
endpoints in the table above and is loaded directly from `file://` against a
running `leather serve --api`. CORS headers are added when `--api` is on so
the SPA can reach the API from the `file://` origin.

**See [AGENTS-UI.md](AGENTS-UI.md) for the file map, design tokens, API
contract layer, state policy, accessibility rules, and the playbook for
adding a new view.** When this file changes the HTTP API surface, update
the corresponding `api.js` wrapper documented in AGENTS-UI.md in the same PR.

---

## Dependency direction

```
cmd/leather      →  internal/cli
internal/cli     →  internal/config, internal/agent, internal/session,
                    internal/scheduler, internal/model, internal/logging,
                    internal/runner, internal/tool, internal/cache,
                    internal/queue, internal/worker, internal/notify,
                    internal/mcp, internal/schema
internal/config  →  internal/model
internal/schema  →  internal/config
```

`internal/cli` is the integration layer that wires all packages together.
`internal/config` is a leaf: it imports only `internal/model` and stdlib.

---

## Adding a new subcommand

1. Create `internal/cli/cmd_{name}.go` with `func Run{Name}(args []string, stdout, stderr io.Writer) int`.
2. Define a `flag.FlagSet` named after the subcommand.
3. Register all flags via `config.BindFlags(fs)` plus any subcommand-specific flags.
4. Add a `case "{name}":` branch in `internal/cli/cli.go`'s dispatch switch.
5. Add the subcommand to the help text in `internal/cli/help.go`.
6. Update the subcommand table in [AGENTS.md](../AGENTS.md) and this file.
7. Add an integration test covering the new subcommand's happy path.

## Adding a new flag

1. Add the flag to the `flag.FlagSet` in the relevant subcommand's `Run*` function.
2. Add the env var lookup in `internal/config/env.go` using the `LEATHER_` prefix convention.
3. Add the default in `internal/config/defaults.go`.
4. Update the flag table in this file.
5. If the flag is globally applicable, add it to the YAML config schema here.

---

## Common mistakes

| Mistake | Correct approach |
|---|---|
| Running LLM call in scheduler tick synchronously | Always wrap in a goroutine with a context timeout |
| `os.Exit` inside `RunServe` | Return the exit code; only `main()` calls `os.Exit` |
| Binding API to `0.0.0.0` by default | Default to `127.0.0.1`; warn loudly if changed |
| YAML parsing with external lib | Use `internal/config/yaml.go` stdlib-only parser |
| Flag name doesn't match env var | `--flag-name` → `LEATHER_FLAG_NAME`; check both |
| Skipping graceful shutdown | Always call `scheduler.Drain` before returning from `RunServe` |
| Flags after positional arg in `leather run` | Go's `flag.FlagSet` stops at the first non-flag token. The agent file path must come **last**: `leather run --config=... --var k=v agent.md` — not `leather run agent.md --config ...` |
| `<item-id>` before flags in `leather dlq requeue` | Same issue: item-id must be **last** after all flags: `leather dlq requeue --state-dir ... <item-id>` |
| `leather init` overwriting without `--overwrite` | `RunInit` fails closed: any pre-existing file causes a non-zero exit and reports `--overwrite` hint. Never silently clobber. |
| Calling `RunValidate` from `RunInit` for post-write validation | `RunValidate` performs a full semantic check including model resolution (fails without `LEATHER_MODEL`). `RunInit` uses schema-only validation (`runInitValidate`) which is syntax-only and does not require a model to be set. |

---

## Verification checklist

Before opening a PR touching this domain:

- [ ] `go test ./internal/cli/... ./internal/config/... ./internal/schema/...` passes
- [ ] `go vet ./...` is clean
- [ ] New flags are in the flag table in this file
- [ ] New flags have matching env vars with `LEATHER_` prefix
- [ ] Graceful shutdown test covers the drain path
- [ ] API endpoints (if changed) are tested with `httptest.NewServer`
- [ ] Config file schema section updated if new YAML keys were added
- [ ] `validate` output/tests cover any new schema-validated file type or flat key


---

_Last reviewed: 2026-06-05_

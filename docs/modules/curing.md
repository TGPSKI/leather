# curing

> Event-driven N-agent workflow that transforms hides into artifacts.

## Responsibility

A **curing** binds one agent to one input queue and produces one of:

- A new artifact written to the artifact store, or
- One or more new queue items dispatched to a downstream curing's queue, or
- A notify dispatch.

`curing` provides four pieces:

1. **`Worker`** — runs one curing definition: drains its queue, builds a `runner.Runner` per item, dispatches output, emits `TanneryEvent`s.
2. **`Supervisor`** — owns the lifecycle of every loaded `Worker`, starting and draining them with the serve process.
3. **`Router`** — matches incoming `(source, event_type)` pairs to one or more `model.TanneryRoute` records so a single webhook can fan out.
4. **`LoadDir`** — loads `*.curing.yaml` files from disk into validated `model.CuringDefinition` values.

## Public API

### Types

| Symbol | Description |
|---|---|
| `TanneryEvent` | `Type, CuringName, AgentName, HideID, ArtifactID, CorrelationID, Payload, At`. Emitted on every state transition; published to the DevTools bus and tannery log. |
| `RunnerDeps` | Shared dependencies injected into every Worker (LLM client, registry, MCP, cache, notifiers, hide store, artifact store, queue manager, logger). |
| `Worker` | One curing in flight. |
| `Supervisor` | Goroutine-per-Worker manager. |
| `Router` | `[]model.TanneryRoute` matcher. |

### Functions

| Symbol | Signature | Description |
|---|---|---|
| `NewWorker` | `(def model.CuringDefinition, deps RunnerDeps, eventFn func(TanneryEvent), eventFnConcurrent bool) *Worker` | Construct a worker. `eventFnConcurrent=false` (default) serializes event delivery so consumers don't need their own locks. |
| `(*Worker).Run` | `(ctx context.Context)` | Block until ctx is canceled. |
| `(*Worker).WaitInflight` | `()` | Wait for all currently-running items to finish; used during drain. |
| `(*Worker).ProcessItem` | `(ctx context.Context, item model.QueueItem) error` | Run one queue item synchronously. Used by tests and `leather ingest`. |
| `NewSupervisor` | `(workers []*Worker, log *logging.Logger) *Supervisor` | Build a supervisor. |
| `(*Supervisor).Start` | `(ctx context.Context)` | Spawn one goroutine per worker. |
| `(*Supervisor).Drain` | `()` | Wait for all in-flight items, then return. |
| `NewRouter` | `(routes []model.TanneryRoute) *Router` | Construct from loaded routes. |
| `(*Router).Match` | `(source, eventType string) (model.TanneryRoute, bool)` | First matching route. |
| `(*Router).MatchAll` | `(source, eventType string) []model.TanneryRoute` | All matching routes (for fan-out). |
| `LoadDir` | `(dir string) ([]model.CuringDefinition, error)` | Load and validate every `*.curing.yaml` under `dir`. |

## Internal Design

### Curing modes

Each curing operates in exactly one input mode:

- **`prefix-scan`** — periodically scans a queue for items sharing a `correlation_id` prefix; runs the agent once N items are present (used for fan-in).
- **`collect`** — accumulates items until `collect_size` is reached; useful for batch curings.
- **`collect-from-queue`** — drains a specific named queue with a single-item loop.

The mode is selected from the `model.CuringDefinition` at load time.

### Per-item execution

`process` builds a fresh `*hide.HideBuffer`, loads any referenced hides, calls
`buildRunner` to produce a `runner.Runner` configured with the buffer and
hide-tool gating, then invokes `Run`. The resulting text is converted to an
artifact (and written) or to a queue item (and dispatched).

### Reflection turns

For curings whose first hide exceeds the inline budget,
`buildReflectionTurns` prepends a synthetic user turn that shows the model
the first cut and instructs reflection before paging — keeps long inputs
controllable.

### Event emission

`emit` invokes `eventFn` either inline (default) or in a goroutine
(`eventFnConcurrent=true`). Events surface in:
- `<state-dir>/tannery.jsonl` for replay.
- The DevTools bus via `sources.Wiring.PublishTannery`.

## Dependencies

| Package | Why |
|---|---|
| `internal/runner` | Per-item execution. |
| `internal/hide` | Buffer + persistent store. |
| `internal/artifact` | Artifact writes. |
| `internal/queue` | Input drain + downstream dispatch. |
| `internal/notify` | Notify-mode output dispatch. |
| `internal/model` | Curing definitions, routes, queue items, artifacts. |
| `internal/logging` | Structured logging. |

## Data Flow

```mermaid
flowchart LR
    Q[input queue] --> W[curing.Worker]
    W --> HB[hide.HideBuffer]
    W --> R[runner.Runner]
    HB --> R
    R -->|artifact mode| AS[artifact.Store]
    R -->|queue mode| Q2[downstream queue]
    R -->|notify mode| N[notify.Dispatcher]
    W -->|TanneryEvent| BUS[devtools bus + tannery.jsonl]
```

## Test Surface

`internal/curing/worker_test.go`, `router_test.go`, `loader_test.go`:

- Worker dispatches artifacts on success.
- Worker requeues on transient failure and routes to DLQ after `max_retries`.
- Router matches single + multi-route fan-out.
- LoadDir rejects unknown fields and invalid `output:` configs.

## Related Docs

- [docs/modules/runner.md](runner.md)
- [docs/modules/hide.md](hide.md)
- [docs/modules/artifact.md](artifact.md)
- [.subagents/AGENTS-TANNERY.md](../../.subagents/AGENTS-TANNERY.md)

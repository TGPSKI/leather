# devtools

> In-process event bus, causality engine, and source aggregators that power the DevTools UI.

## Responsibility

`devtools` exposes a real-time view of leather's runtime state. It is split
into three subpackages:

| Subpackage | Responsibility |
|---|---|
| `bus` | Bounded, ring-buffered, fan-out event bus with sequence numbers, redaction, and back-pressure handling. |
| `causality` | Trace-builder that walks parent/child links across bus events to reconstruct one logical operation. |
| `sources` | Wires the runner, scheduler, tannery, and HTTP layer into the bus by publishing typed events. |

The CLI's `/api/devtools/*` HTTP endpoints read directly from this package.

## Public API — `bus`

| Symbol | Signature | Description |
|---|---|---|
| `Event` | `type Event struct { Seq uint64; ParentSeq uint64; Kind string; Source string; Payload json.RawMessage; Err string; At time.Time; ... }` | Immutable, redacted event published to the bus. |
| `Stats` | `type Stats struct { Capacity, Length, Subscribers int; Dropped uint64; ... }` | Bus instrumentation. |
| `Bus` | `type Bus struct { ... }` | Ring-buffer event hub. |
| `New` | `(capacity int) *Bus` | Capacity is the ring size; events older than `capacity` are evicted. |
| `(*Bus).Publish` | `(ev Event) uint64` | Assign a sequence number, redact, store, fan out. Returns the assigned `Seq`. |
| `(*Bus).AppendCause` | `(parent, child uint64) bool` | Add a parent→child link after the fact. |
| `(*Bus).Subscribe` | `(ctx context.Context, fromSeq uint64) <-chan Event` | Stream events `>= fromSeq`. Backed by the snapshot, then live tail. |
| `(*Bus).SubscribeWithCloser` | `(ctx, fromSeq) (<-chan Event, func())` | Same, plus an idempotent closer (uses `sync.Once`) so SSE handlers can release the slot exactly once. |
| `(*Bus).Snapshot` | `() []Event` | Copy of all currently-buffered events. |
| `(*Bus).Stats` | `() Stats` | Live counts. |
| `RedactEvent` | `(Event) Event` | Strip secrets and truncate long fields. |

## Public API — `causality`

| Symbol | Description |
|---|---|
| `TraceOptions` | `MaxNodes`, `IncludeAncestors` controls. |
| `Node` | One event in the trace tree. |
| `Result` | `Root, Nodes, Truncated`. |
| `Engine` | Stateless tracer. |
| `(*Engine).Trace` | `(ctx, b *bus.Bus, root uint64, opts TraceOptions) Result` — walks parent and child links from a root sequence. |
| `(*Engine).LinkForward` | `(b *bus.Bus, parent, child uint64) bool` — convenience wrapper around `Bus.AppendCause`. |

## Public API — `sources`

| Symbol | Signature | Description |
|---|---|---|
| `Deps` | `Bus, CausalityEngine` | Inputs to `Wire`. |
| `Wiring` | Bus-publishing facade for runtime components. |
| `Wire` | `(b *bus.Bus, deps Deps) *Wiring` | Construct a wiring; usually one per `serve` process. |
| `(*Wiring).PublishHTTP` | `(kind string, payload map[string]any) uint64` | Inbound API request. |
| `(*Wiring).PublishRunner` | `(curingName, agentName string, ev runner.ProgressEvent) uint64` | Convert a runner progress event to a bus event. |
| `(*Wiring).PublishScheduleFire` | `(agentName, scheduleExpr string) uint64` | Scheduler tick. |
| `(*Wiring).PublishTannery` | `(ev curing.TanneryEvent) uint64` | Tannery transitions (hide stored, agent run, artifact written, etc.). |

## Internal Design

### Back-pressure

Slow subscribers do not block publish. `bus.sendDropOldest` evicts from a
subscriber's channel head when it's full, increments `Stats.Dropped`, and
keeps publish O(1).

### Subscription lifecycle

`SubscribeWithCloser` returns a closer guarded by `sync.Once`. SSE handlers
must call it exactly on disconnect; calling twice is a no-op so panic-recover
paths remain safe.

### Redaction

`RedactPayload` walks JSON objects and replaces values for keys matching
`isSensitiveKey` (api keys, tokens, passwords, signing secrets, bearer
prefixes). Long string fields are truncated to a fixed byte cap. The same
function is applied to `Event.Err`.

### Causality semantics

Bus events carry `ParentSeq`. The causality engine walks `(seq → parent)` to
build ancestors and `(parent → seq)` to build descendants, with a hard cap
to prevent runaway cycles in malformed event streams.

## Dependencies

| Package | Why |
|---|---|
| `internal/runner` | `runner.ProgressEvent` for `PublishRunner`. |
| `internal/curing` | `curing.TanneryEvent` for `PublishTannery`. |
| `internal/model` | Event-payload types. |

`bus` and `causality` are stdlib-only; `sources` is the only subpackage with
intra-project imports.

## Test Surface

- `bus`: publish/snapshot eviction, replay+live subscribe, append-cause, slow-subscriber drop counting, payload + error redaction.
- `causality`: includes parent ancestors and child descendants in a trace; `LinkForward` updates the bus.
- (`sources` is exercised end-to-end via `/api/devtools/*` integration tests.)

## Related Docs

- [docs/ARCHITECTURE.md](../ARCHITECTURE.md) — DevTools API endpoints and auth model.
- [.subagents/AGENTS-OBSERVABILITY.md](../../.subagents/AGENTS-OBSERVABILITY.md)

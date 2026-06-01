# AGENTS-WORKER.md — leather scheduling, queues, and background workers

Subagent guide for the worker domain: cron scheduling, file-backed queues,
declarative HTTP poll workers, and the worker supervisor.

Load this guide when working on `internal/scheduler`, `internal/queue`, or
`internal/worker`. For agent execution and tool calling, see
[AGENTS-RUNTIME.md](AGENTS-RUNTIME.md). For serving and config, see
[AGENTS-SERVE.md](AGENTS-SERVE.md). For deployment-side concerns
(single-process lock, supervision, log rotation), see
[AGENTS-OPERATIONS.md](AGENTS-OPERATIONS.md). For the queue-state security
posture (file modes, single-writer invariant), see
[AGENTS-SECURITY.md](AGENTS-SECURITY.md). For queue-throughput benchmarks,
see [AGENTS-PERFORMANCE.md](AGENTS-PERFORMANCE.md).

---

## Package responsibilities

### `internal/scheduler`

Parses cron expressions and drives periodic agent execution. The scheduler
does not import `internal/session` or `internal/agent` directly — it operates
on `model.Job` values and calls registered handler functions.

Key exported surfaces:

```go
// New returns a Scheduler with the given options.
func New(opts Options) *Scheduler

// Register adds a job for the named agent. handler is called on each tick.
// schedule is a cron expression string or "once".
func (s *Scheduler) Register(name, schedule string, handler JobHandler) error

// Start launches the scheduler loop. It blocks until ctx is cancelled.
func (s *Scheduler) Start(ctx context.Context) error

// Drain waits for all in-flight handlers to finish, up to timeout.
// Returns an error if timeout is exceeded before all jobs complete.
func (s *Scheduler) Drain(timeout time.Duration) error

// Jobs returns a snapshot of current job records (safe for concurrent read).
func (s *Scheduler) Jobs() []model.Job

// JobHandler is the function signature for scheduled work.
type JobHandler func(ctx context.Context, job model.Job) error
```

#### Cron expression format

Standard five-field cron: `minute hour day-of-month month day-of-week`.

The cron parser lives in `internal/scheduler/cron.go`. It is a minimal
stdlib-only implementation supporting:
- Numeric values, `*` wildcards, `/` step values, `,` lists
- Special value `"once"` — run once at startup, never re-queued
- No macro aliases (`@hourly`, `@daily`) — add if explicitly requested

#### Concurrency control

- `--max-concurrent-jobs` (default: 4) caps simultaneous handlers.
- Implemented via a semaphore channel (`make(chan struct{}, n)`). A job that
  cannot acquire the semaphore is skipped and logged as `JobStatus.Skipped`.
- Each handler runs in its own goroutine. `sync.WaitGroup` tracks goroutines
  for `Drain`.

#### `schedule: "once"` semantics

Agents with `schedule: "once"` in their lifecycle file are executed exactly
once at serve startup. They are registered with the scheduler but are not
re-queued after the first run. Useful for one-shot setup agents.

---

### `internal/queue`

JSONL file-backed FIFO queue. Supports multiple named queues under a shared
state directory. Used by the worker supervisor to deliver poll results and
by the runner to dequeue items as agent inputs.

Key exported surfaces:

```go
// FileQueue is a single named JSONL queue stored on disk.
type FileQueue struct { ... }

// NewFileQueue creates or opens the queue file at path.
// The directory is created if it does not exist (mode 0700).
// The queue file is created with mode 0600.
func NewFileQueue(path string) (*FileQueue, error)

// Enqueue appends item to the tail of the queue.
func (q *FileQueue) Enqueue(item model.QueueItem) error

// Dequeue removes and returns the head item.
// Returns (item, true, nil) on success; (zero, false, nil) when empty.
func (q *FileQueue) Dequeue() (model.QueueItem, bool, error)

// Peek returns the head item without removing it.
// Returns (item, true) when non-empty; (zero, false) when empty.
func (q *FileQueue) Peek() (model.QueueItem, bool)

// Len returns the current number of items in the queue.
func (q *FileQueue) Len() int

// Manager provides named access to FileQueue instances under a shared directory.
type Manager struct { ... }

// NewManager returns a Manager rooted at dir.
func NewManager(dir string) *Manager

// Get returns (or creates) the named queue. Queue files are created lazily.
func (m *Manager) Get(name string) (*FileQueue, error)

// Names returns the names of all queues currently on disk.
func (m *Manager) Names() []string

// Enqueue is a convenience method: Get(queueName) + Enqueue(item).
func (m *Manager) Enqueue(queueName string, item model.QueueItem) error
```

#### Queue file format

Each queue is a JSONL file at `<state_dir>/queues/<name>.jsonl`. Each line
is a JSON-encoded `model.QueueItem`. Reads load all lines into memory; writes
re-serialize the full slice. Files are mode 0600; directories mode 0700.

`model.QueueItem` fields:
- `ID` — unique item identifier (set by the enqueuer; workers use `agent+nanotime`)
- `AgentName` — optional target agent name
- `Payload` — `map[string]any`; template variables available in agent prompts
- `EnqueuedAt` — Unix timestamp
- `AttemptCount` — incremented by the runner on each dequeue-and-run attempt

---

### `internal/worker`

Declarative background polling workers. Each `*.worker.yaml` definition maps
to one goroutine managed by the `Supervisor`. Workers poll HTTP endpoints and
enqueue new items into named queues.

Key exported surfaces:

```go
// LoadDir reads all *.worker.yaml files from dir and returns parsed definitions.
// Returns an error slice; partial results may be returned alongside errors.
func LoadDir(dir string) ([]model.WorkerDefinition, []error)

// Supervisor manages the lifecycle of all workers.
type Supervisor struct { ... }

// NewSupervisor constructs a Supervisor for the given worker definitions.
func NewSupervisor(defs []model.WorkerDefinition, mgr *queue.Manager, log *logging.Logger) *Supervisor

// Start launches one goroutine per worker definition. Returns immediately.
// Workers run until ctx is cancelled.
func (s *Supervisor) Start(ctx context.Context)

// Drain waits for all worker goroutines to exit (blocking).
// Call after ctx cancellation to ensure clean shutdown.
func (s *Supervisor) Drain()
```

#### Worker file format (`*.worker.yaml`)

```yaml
name: skeptic-issue-poller
type: http_poll
interval: 5m
url: "https://api.github.com/repos/{{env:SKEPTIC_REPO}}/issues"
headers:
  Authorization: "Bearer {{env:GITHUB_TOKEN}}"
output:
  queue: skeptic-issues
  dedup_key: .number     # leading dot stripped; field in each JSON array element
```

#### `HTTPPollWorker` — the only worker type in v1

On each interval tick:
1. GET the configured URL with configured headers (`{{env:VAR}}` expanded).
2. Parse the response as a JSON array. Non-array responses are logged and skipped.
3. For each array element, extract the `dedup_key` field value.
4. If the value has not been seen in this process lifetime, enqueue a `QueueItem`
   with the element as `Payload`.
5. Dedup state is in-memory only — workers start fresh on restart.

**Security**: header values with `{{env:VAR}}` are expanded immediately before
the request; auth header values are never logged.

#### Agent consuming queue input

A lifecycle file connects a worker queue to an agent:

```yaml
# skeptic-triage.lifecycle.yaml
agent: skeptic-issue-triage
schedule: "* * * * *"
model: llama3
queue_input: skeptic-issues   # dequeue one item per scheduler tick
```

When `queue_input` is set, the scheduler tick:
1. Checks if the queue has items (`QueueMgr.Get(name).Len() > 0`).
2. Dequeues one `QueueItem`.
3. Calls `runner.ExpandPromptPayload(a, item.Payload)` to inject payload fields.
4. Runs the agent with the expanded prompts.
5. If the queue is empty, the tick is skipped (no LLM call).

---

## `internal/curing` — event-driven curing pipeline

> Full documentation is in [AGENTS-TANNERY.md](AGENTS-TANNERY.md).
> This section provides a brief orientation.

`internal/curing` owns the event-driven counterpart to the scheduler-driven
worker system. Where `internal/worker` polls HTTP endpoints on a timer,
`internal/curing` consumes items from named queues that are populated by
external events (webhooks, direct ingest).

### CuringWorker

`Worker` dequeues a `model.QueueItem`, loads the referenced hide from
`hide.Store`, runs the configured agent via `runner.Runner`, writes an
`model.Artifact` to `artifact.Store`, and deletes the hide on success.

```go
// NewWorker creates a Worker for the given curing definition.
func NewWorker(def model.CuringDefinition, agents map[string]model.Agent,
    concurrency int, hideStore *hide.Store, artStore *artifact.Store,
    deps *RunnerDeps, qmgr *queue.Manager, notifiers map[string]notify.Notifier,
    log *logging.Logger) (*Worker, error)

// Run starts the polling loop. Returns when ctx is cancelled.
func (w *Worker) Run(ctx context.Context)

// ProcessItem processes one QueueItem without retry logic.
// Exported for testing and one-shot invocations.
func (w *Worker) ProcessItem(ctx context.Context, item model.QueueItem) error
```

### Supervisor

`Supervisor` manages a fleet of `Worker` instances, one per `CuringDefinition`.

```go
// NewSupervisor creates a Supervisor over all curing definitions.
func NewSupervisor(defs []model.CuringDefinition, agents map[string]model.Agent,
    concMap map[string]model.QueueConcurrencyConfig, hideStore *hide.Store,
    artStore *artifact.Store, deps *RunnerDeps, qmgr *queue.Manager,
    router *Router, log *logging.Logger) (*Supervisor, error)

// Start launches one Worker goroutine per definition.
func (s *Supervisor) Start(ctx context.Context)

// Drain waits for all workers to exit.
func (s *Supervisor) Drain()
```

See [AGENTS-TANNERY.md](AGENTS-TANNERY.md) for:
- `CuringDefinition` YAML format
- Worker retry / DLQ policy
- Router semantics (webhook source → curing route matching)
- Hide / artifact storage layout

---

## Dependency direction

```
internal/scheduler →  internal/model, internal/logging
internal/queue     →  internal/model, stdlib (encoding/json, os)
internal/worker    →  internal/queue, internal/model, internal/logging,
                      stdlib (net/http, encoding/json, os/exec)
```

`internal/scheduler` does not import `internal/agent` or `internal/session`.
The CLI wires them together by registering handler closures.

`internal/queue` has no intra-project imports beyond `internal/model`.

---

## Common mistakes

| Mistake | Correct approach |
|---|---|
| Running LLM calls in the scheduler tick synchronously | Always wrap in a goroutine with a context timeout in the CLI handler closure |
| Orphaning worker goroutines on shutdown | Call `Supervisor.Drain()` after context cancellation; always use `WaitGroup` |
| In-process dedup surviving restarts | Expected: dedup is by design in-memory only; document this for users |
| Queue file with permissions wider than 0600 | `NewFileQueue` must use 0600 for the file, 0700 for the directory |
| Skipping `"once"` agents after re-registration | Scheduler must track `once` jobs and not re-queue them |
| Worker polling with no timeout on the HTTP client | Use `http.Client{Timeout: interval}` — never block indefinitely |

---

## Verification checklist

Before opening a PR touching this domain:

- [ ] `go test ./internal/scheduler/... ./internal/queue/... ./internal/worker/...` passes
- [ ] `go test -race ./...` is clean
- [ ] Worker tests use `httptest.NewServer`; no real network calls
- [ ] Queue tests use `t.TempDir()`; no hardcoded paths
- [ ] `Supervisor.Drain()` test verifies all goroutines exit cleanly
- [ ] New cron expressions exercised in scheduler tests (valid + invalid)
- [ ] `dedup_key` edge cases tested (missing field, empty value, repeated value)

---

_Last reviewed: 2026-05-19_

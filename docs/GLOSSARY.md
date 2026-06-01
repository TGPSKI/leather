# Leather Vocabulary

This is the authoritative naming reference for leather. When naming types,
fields, CLI flags, YAML keys, package names, or documentation concepts,
consult this file first.

All new code uses these terms. Existing code is migrated incrementally — the
rename pass happens after vocabulary stability is confirmed by two or more
shipped features that use the new names correctly.

---

## Canonical term table

| Term | Definition | Current code anchor |
|---|---|---|
| **Leather** | The CLI binary and runtime. The top-level product name. | `cmd/leather`, `internal/cli` |
| **Tanning** | A local working directory containing configs, agents, curings, and tools. The checked-in artifact a user authors. Analogous to a project folder. | `tanning/`, `dev/` (example tannings) |
| **Tannery** | The long-running workspace service: composition of queue + worker + scheduler + session + cache + hide store. Not new infrastructure — a name for the composition of primitives already written. | `internal/cli` (serve loop) |
| **Scheduler** | The time- and event-triggered execution plane. Dispatches jobs on cron expressions. | `internal/scheduler` |
| **Hide** | Raw input material. The large, unprocessed content a curing operates over (e.g. a PR diff, a log file, API response). Persisted; never held in memory whole. | `hide.Store` in `internal/hide/store.go` |
| **HideBuffer** | In-process runtime store for one hide. Noun-first, matches Go naming convention (`FileCache`, `FileQueue`). | `hide.HideBuffer` in `internal/hide/buffer.go` |
| **Cut** | A scoped slice or page of a `HideBuffer`. What the session window actually sees per turn. Keeps session from owning giant tool output. | `hide.Cut`; produced by `hide.HideBuffer.Cut()` |
| **Curing** | A named multi-agent workflow that runs a sequence of operations over one or more hides. Produces artifacts. A curing definition is the "what to do"; a curing run is the execution instance. | `model.CuringDefinition`; loader in `internal/curing/loader.go` |
| **Curing Run** | One execution instance of a curing. Has state: active hides, operation sequence, extracted vars, emitted artifacts, cut/page cursors. | _(new)_ |
| **Operation** | One agent working on one or more cuts in a single turn. The user-facing word for what an `Agent` does at runtime. `Agent` remains the file/definition artifact name in code and YAML. | `internal/agent`, `model.Agent` |
| **Agent** | The definition artifact: an `*.agent.md` file plus optional `*.lifecycle.yaml`. The author-facing term. At runtime, an agent executes as an **Operation**. | `internal/agent`, `model.Agent` |
| **IntakeWorker** | A worker that creates hides and enqueues curing work. Does not run agents. Pull-based (HTTP poll) or push-based (webhook, file watch). | `internal/worker` (`HTTPPollWorker`) |
| **CuringWorker** | A worker that executes curing run work items from a queue. | _(new worker type)_ |
| **Queue** | Durable pending-work store. Holds work references (IDs + metadata), never large content. | `internal/queue` |
| **CuringWorkItem** | A queue item: `hide_id` + `curing_name` + metadata. What a `CuringWorker` dequeues and executes. | `model.QueueItem` (extended with omitempty tannery fields; rename deferred) |
| **HideStore** | Persistent storage for raw hides. Addressed by hide ID. | `hide.NewStore` in `internal/hide/store.go` |
| **ArtifactStore** | Persistent storage for stabilized outputs with lineage. | `artifact.NewStore` in `internal/artifact/store.go` |
| **Artifact** | A stabilized output produced by a curing run. Has provenance (curing name, run ID, hide IDs). | `model.Artifact` in `internal/model/model.go`; stored by `artifact.Store` |
| **Intake** | The set of mechanisms that create hides from external input: webhooks, HTTP pollers, CLI ingestion, file ingestion. | `internal/worker` (intake subset) |
| **Router** | Routes hides or events to curings based on rules or content matching. | `curing.NewRouter` in `internal/curing/router.go` |
| **Notify** | The output notification layer. Sends artifacts or events to backends. | `internal/notify` |
| **Backend** | A notify destination: Telegram, Signal, webhook, file, queue. | `model.NotifyBackendConfig` |

---

## Constraint rules

These are the invariants the vocabulary enforces. Treat violations as bugs:

- **Worker does not run agents.** Workers create hides and enqueue curing work items. Curings run agents.
- **Queue does not store content.** Queue items hold IDs and metadata only. Content lives in HideStore.
- **Session does not own large output.** Session receives the current cut only. HideBuffer manages the rest.
- **Curing executor does not fetch external state.** It receives a `hide_id` and requests scoped cuts from HideStore.
- **Tannery is not new infrastructure.** It names the composition: `queue + worker + scheduler + session + cache + hide store`.

---

## Hierarchical view

```
Leather (CLI/binary)
  Tannery (long-running workspace service)
    Intake         — creates hides from external sources
    HideStore      — persists raw hides
    Router         — routes hides to curings
    Queue          — durable pending-work store (CuringWorkItems)
    IntakeWorker   — creates hides, enqueues work
    CuringWorker   — executes curing runs from queue
    Curing         — named multi-agent workflow definition
      Curing Run   — one execution instance
        Operation  — one agent working on one or more cuts per turn
          Cut      — bounded view of a HideBuffer
    ArtifactStore  — persists artifacts with lineage
    Notify         — output notification layer
      Backend      — Telegram, Signal, webhook, file, queue
  Scheduler        — time/event-triggered execution plane
```

---

## Mapping: current code → target vocabulary

Rows marked **Implemented** are live in the codebase under the target
name. Rows marked **Deferred to v0.2** describe a planned rename: the
target term is what we'll use in user-facing docs once the rename PR
lands. Until then, code still uses the **Current** column name.

| Current | Target | Notes |
|---|---|---|
| `internal/worker` (`HTTPPollWorker`) | `IntakeWorker` | **Deferred to v0.2**: rename the type; keep package name |
| `internal/queue` (`QueueItem`) | `CuringWorkItem` | **Deferred to v0.2**: type already extended with omitempty tannery fields |
| `internal/scheduler` | `Scheduler` | No rename needed |
| `internal/session` | Session (internal term, no public rename) | Owns only the current cut |
| `internal/cache` | Cache (internal term) | No rename needed |
| `internal/notify` | `Notify` | No rename needed |
| `model.Agent` | `Agent` (definition) / `Operation` (runtime) | **Deferred to v0.2**: add `Operation` concept as distinct runtime type |
| `model.Job` | `CuringRun` | **Deferred to v0.2**: rename when curing-run promotion lands |
| `internal/hide` | `Hide` / `HideBuffer` | **Implemented**: `buffer.go` (HideBuffer) + `store.go` (HideStore) |
| `internal/curing` | `Curing` / `CuringWorker` | **Implemented**: `worker.go`, `supervisor.go`, `router.go`, `loader.go` |
| `internal/artifact` | `ArtifactStore` | **Implemented**: `store.go` |
| `model.Artifact` | `Artifact` | **Implemented**: in `internal/model/model.go` |
| `model.CuringDefinition` | `CuringDefinition` | **Implemented**: in `internal/model/model.go` |

---

## When this file changes

Update this file when:
- A term is added, removed, or redefined
- A new package is implemented that realizes a planned concept
- A "current code anchor" cell changes due to a rename

Do not add terms here speculatively. A term earns a row when it appears in
code, YAML schemas, CLI flags, or user-facing documentation.

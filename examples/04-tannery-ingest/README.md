# 04-tannery-ingest

The simplest tannery loop:

1. `leather ingest` reads a file, stores it as a **hide**, and enqueues
   a curing work-item.
2. `leather serve` starts the curing worker pool. The worker pages the
   hide through the agent and writes an **artifact** to `.state/artifacts/`.

No webhooks, no scheduling — just the hide → curing → artifact pipeline.

## Requirements

A local OpenAI-compatible endpoint at `$LEATHER_LLM_ENDPOINT`.

## Run

```bash
make 04
```

`make 04` ingests `sample/build.log` and runs the server briefly
to give the worker time to process it. Artifacts are listed at the end.

## What this shows

- `tannery.yaml` declaring `hide_dir`, `artifact_dir`, `curing_dir`, and a queue.
- `routes:` connecting ingest events (`source: ci, event_type: build.log`) to a
  curing definition and queue.
- A `*.curing.yaml` binding a queue → agent → artifact.
- Page-based hide delivery (`page_size_bytes`).
- Per-item retries via `max_attempts`.

## Pipeline

```
leather ingest --kind build.log --source ci sample/build.log
   │
   ├─ writes hide → .state/hides/<id>/
   └─ enqueues item → .state/queues/default.jsonl

leather serve --tannery tannery.yaml
   │
   └─ curing worker polls queue every 1s
         │
         ├─ dequeues item
         ├─ loads hide from .state/hides/<id>/
         ├─ runs log-summary agent (pages the hide content)
         ├─ writes artifact → .state/artifacts/log-summary/<id>.json
         ├─ calls OnComplete → prints turn output + token stats
         └─ deletes hide (cleanup after success)
```

## State directory

After a successful run:

| Path | Contents |
|---|---|
| `.state/artifacts/log-summary/` | One JSON file per completed artifact |
| `.state/runs/` | One JSONL run record per agent invocation (`--persist-runs`) |
| `.state/queues/default.jsonl` | Empty — items removed after processing |
| `.state/hides/` | Empty — hides deleted after artifact write |
| `.state/cache/` | Empty unless response caching is enabled |

Hides are intentionally ephemeral: they hold the raw input only until
the curing worker produces an artifact. On DLQ failure the hide is
retained for inspection.

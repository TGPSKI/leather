# 08-dead-letter-queue

Dead-letter queue (DLQ) demo with deterministic failure.

This example intentionally fails a curing run so you can watch the worker:

1. dequeue the item
2. retry once (`max_attempts: 2`)
3. route the item to `fail-in-dlq`

## How failure is forced

The curing uses an agent with `timeout: 1ms`, which guarantees each model call
fails quickly with a timeout error. That makes retry and DLQ promotion reliable
for demonstration purposes.

## Run

```bash
make 08
```

The script ingests one `demo.failure` hide, starts `serve`, waits for the worker
cycle, then verifies `fail-in-dlq` depth through the API.

Operator recovery follow-up (requeue loop):

```bash
make 08-requeue
```

This run mode performs the same initial DLQ promotion, then calls
`POST /queues/fail-in/requeue` and verifies the item re-enters processing.
Because the agent is intentionally still failing, the item cycles back into
`fail-in-dlq`.

## What this shows

- Retry behavior under worker failure.
- DLQ naming convention (`<queue>-dlq`).
- Queue API visibility (`/queues/fail-in-dlq`).
- DLQ operator recovery path (`POST /queues/fail-in/requeue`).
- Hide retention after DLQ routing (the source hide remains in `.state/hides`).

## Files

- `tannery.yaml` — queue with `max_attempts: 2`
- `curings/fail-demo.curing.yaml` — failing curing bound to `fail-in`
- `agents/always-timeout.agent.md` — deterministic timeout trigger
- `scripts/run-demo.sh` — ingest + serve + DLQ verification
- `scripts/run-requeue-demo.sh` — ingest + DLQ + requeue + re-DLQ verification

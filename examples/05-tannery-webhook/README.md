# 05-tannery-webhook

The marquee example: an **end-to-end event pipeline**.

1. `leather serve --api` starts the HTTP server with a webhook endpoint and
   the curing worker pool.
2. A `curl` script computes HMAC-SHA256 over the payload and POSTs to
   `/webhooks/demo`.
3. The router matches `source=demo` and creates a hide of kind `demo.event`,
   then enqueues it on the `default` queue with curing `event`.
4. The worker pages the hide through the agent and writes an artifact.

## Requirements

- A local OpenAI-compatible endpoint at `$LEATHER_LLM_ENDPOINT`.
- `openssl` (for HMAC; preinstalled everywhere).
- `curl`.

## Run

```bash
make 05
```

The `scripts/run-demo.sh` helper handles the full sequence: start serve in
the background, wait for the API, POST the signed payload, wait for the
worker to process it, then shut serve down and list the artifact.

## What this shows

- HMAC-validated webhook intake with `{{env:VAR}}` secret expansion.
- Route matching on `source` (and optionally `event_type`).
- `--api` mode bringing up the HTTP server alongside the worker pool.
- Backpressure via `max_depth` on the queue.

## Files

- `tannery.yaml` — declares the `demo` webhook (with HMAC) and a `source=demo` route.
- `curings/event.curing.yaml` — binds the queue to the `event` agent.
- `agents/event.agent.md` — the agent.
- `sample/payload.json` — the request body.
- `scripts/send-webhook.sh` — `openssl dgst` + `curl` helper.
- `scripts/run-demo.sh` — orchestrates the whole demo.

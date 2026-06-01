# 07-external-routing

External (ingress) routing demo: one webhook endpoint, multiple
`routes:` selected by `source + event_type` before any curing runs.
The webhook handler **fans out**: every route whose `match:` predicate
matches is enqueued, each sharing one hide. Routes whose predicates would
overlap should target distinct queues, otherwise duplicate items will
reference the same hide and the second to process will fail.

```text
github webhook
  ├─ event_type=pull_request_review_comment -> review-comment curing
  ├─ event_type=issues                      -> issue-event curing
  └─ event_type=deployment_status           -> escalation curing -> telegram notify
```

Unlike example 06 (internal chaining via `output.queue`), this example focuses
on **external route selection** at intake time.

## Requirements

- A local OpenAI-compatible endpoint at `$LEATHER_LLM_ENDPOINT`.
- `openssl` and `curl`.
- Optional for Telegram notification path: either a `pass` entry at
  `telegram/YOUR_BOT` or `LEATHER_TELEGRAM_BOT_TOKEN`, plus a real
  `chat_id` value in `config.yaml`.

## Run

```bash
make 07
```

`make 07` starts `serve --api`, sends two signed webhook events
(`pull_request_review_comment` and `issues`), waits for artifacts, then exits.

To exercise the Telegram path too:

```bash
export LEATHER_TELEGRAM_BOT_TOKEN=...   # your bot token
# edit config.yaml chat_id to your real chat/channel id
SEND_TELEGRAM_EVENT=1 make 07
```

If `SEND_TELEGRAM_EVENT=1` is set and no token can be resolved, the demo
exits early with a clear error instead of running with notifications disabled.

## What this shows

- External fan-out routing on webhook intake (`source`, `event_type`).
- One shared hide per webhook delivery; one queue item per matched route.
- Distinct hide kinds per route.
- Optional `output.notify` via Telegram on escalation events.

## Files

- `tannery.yaml` — webhook + ordered routes + queues.
- `curings/*.curing.yaml` — queue-to-agent bindings.
- `agents/*.agent.md` — per-route processors.
- `scripts/send-webhook.sh` — HMAC-signed POST helper with selectable event type.
- `scripts/run-demo.sh` — orchestrates the full demo.

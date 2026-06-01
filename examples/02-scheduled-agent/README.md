# 02-scheduled-agent

A minimal `leather serve` setup: one agent, one cron schedule, runs for a
bounded duration so the demo exits on its own.

## Requirements

- A running OpenAI-compatible endpoint at `$LEATHER_LLM_ENDPOINT`
  (default `http://localhost:11434` — Ollama).
- A model named `$LEATHER_MODEL` (default `llama3`).

## Run

```bash
export LEATHER_LLM_ENDPOINT=http://localhost:11434
export LEATHER_MODEL=llama3
make 02
```

The schedule fires every 10 seconds; `--run-duration` keeps the demo
short. You'll see the agent fire a few times.

## What this shows

- Cron scheduling via `*.lifecycle.yaml`
- Token-budget management in `--pretty --stats` output
- Per-run persistence under `.state/runs/`
- Graceful shutdown when `--run-duration` elapses

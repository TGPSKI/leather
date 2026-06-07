# rpi-02-hailo-local-status-digest

A real-value tiny endpoint example.

This example collects a deterministic local snapshot with shell commands, then
asks a tiny local model endpoint to compress that evidence into a JSON status
digest.

The model does not inspect the machine. The snapshot does. The model only
classifies and summarizes bounded evidence.

## What this shows

- Leather can run against a tiny local OpenAI-compatible endpoint.
- A weak local model can still provide value as a semantic reducer.
- Deterministic checks provide facts; the model provides a compact digest.
- Leather provides schedule, runtime output, stats, and persisted run history.

## Run

Start the Hailo/OpenAI proxy from the standalone RPi onboarding repo, then run:

```bash
make doctor
make run
```

`make snapshot` writes `.snapshot/` and renders
`agents/local-status.lifecycle.yaml`. Those files are generated and ignored.

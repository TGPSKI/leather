# 13-rpi-hailo-endpoint-canary

A scheduled tiny local endpoint summarization canary.

This example runs one Leather agent against a small local model, such as `qwen3:1.7b` served through a Hailo/OpenAI compatibility proxy.

The point is not deep reasoning. The point is proving that Leather can send bounded text to a tiny endpoint, get structured compression back, and report runtime, timing, and token stats.

## Run

```bash
make doctor
make run
```

Or directly:

```bash
../../leather serve \
  --config config.yaml \
  --llm-endpoint http://localhost:8080 \
  --model qwen3:1.7b \
  --pretty \
  --stats \
  --max-jobs 1 \
  --run-duration 180s
```

## Useful pattern

```text
bounded input -> strict output contract -> local semantic compression
```

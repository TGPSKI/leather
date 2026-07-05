# Raspberry Pi 5 + AI HAT+ 2

Leather can run against a Raspberry Pi 5 with AI HAT+ 2 when the device exposes
a small OpenAI-compatible endpoint. Keep Leather focused on the runtime path:
endpoint validation, bounded summarization, ingest, queues, curings, and
artifacts.

The hardware and Hailo package setup lives in the companion onboarding repo:
**[TGPSKI/rpi5-ai-hat2-onboarding](https://github.com/TGPSKI/rpi5-ai-hat2-onboarding)**

That repo covers Raspberry Pi OS imaging, PCIe setup, `hailo-h10-all`,
Hailo-Ollama, the OpenAI compatibility proxy, and systemd user units.

## Local Endpoint Shape

```text
Leather
  -> OpenAI-compatible proxy on http://localhost:8080
  -> Hailo-Ollama on http://127.0.0.1:8000
  -> Hailo-10H / AI HAT+ 2
```

The proxy owns Hailo-specific compatibility quirks. Leather should see a normal
OpenAI-style chat-completions endpoint.

## Examples

The repo ships three dedicated Raspberry Pi / Hailo examples under
[`examples/`](../../examples/):

```bash
cd examples

# Validate config shape without running the endpoint:
make validate-rpi-01
make validate-rpi-02
make validate-rpi-03

# Run the examples end to end:
make rpi-01
make rpi-02
make rpi-03
```

Examples:

- [`examples/rpi-01-hailo-endpoint-canary/`](../../examples/rpi-01-hailo-endpoint-canary/)
  is a scheduled tiny-endpoint canary: bounded input, strict output contract,
  local semantic compression.
- [`examples/rpi-02-hailo-local-status-digest/`](../../examples/rpi-02-hailo-local-status-digest/)
  collects deterministic local status evidence and asks the tiny model to
  compress that evidence into a JSON digest.
- [`examples/rpi-03-hailo-local-status-ingest/`](../../examples/rpi-03-hailo-local-status-ingest/)
  collects deterministic local status evidence, ingests it as a hide, and cures
  it into an operational artifact.

From each example directory, the common local flow is:

```bash
make doctor
make run
```

The top-level `examples/Makefile` also wires the Raspberry Pi-specific endpoint
defaults:

- `LEATHER_RPI_LLM_ENDPOINT` defaults to `http://localhost:8080`
- `LEATHER_RPI_MODEL` defaults to `qwen3:1.7b`

Override them when your proxy runs elsewhere:

```bash
cd examples
LEATHER_RPI_LLM_ENDPOINT=http://pi-host:8080 \
LEATHER_RPI_MODEL=qwen3:1.7b \
make rpi-01
```

The useful pattern is:

```text
deterministic local checks -> Leather hide/queue -> tiny model compression
```

Do not make the tiny model responsible for discovering machine truth. Let shell
checks gather evidence and let the model summarize bounded input.

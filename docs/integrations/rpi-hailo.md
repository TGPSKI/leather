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

Run these from `examples/`:

```bash
make validate-13
make validate-14
make validate-15

make 13
make 14
make 15
```

Examples:

- `examples/13-rpi-hailo-endpoint-canary/` checks that a tiny local endpoint can
  return a strict three-line summary.
- `examples/14-rpi-hailo-local-status-digest/` collects deterministic local
  evidence and runs a scheduled digest without tannery.
- `examples/15-rpi-hailo-local-status-ingest/` collects deterministic local
  evidence, ingests it as a hide, and cures it into an operational artifact.

The useful pattern is:

```text
deterministic local checks -> Leather hide/queue -> tiny model compression
```

Do not make the tiny model responsible for discovering machine truth. Let shell
checks gather evidence and let the model summarize bounded input.

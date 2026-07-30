# leather examples

A growing set of self-contained examples that walk you from "does it even
build" to "webhooks driving multi-agent curings." Each one is runnable in
isolation from a fresh clone with **a single `make` target**.

| # | Example | Needs LLM? | Demonstrates |
|---|---|---|---|
| [01](01-hello-mock/) | `01-hello-mock` | no | `leather test-agent` against `MockLLM` — instant proof the binary works |
| [02](02-scheduled-agent/) | `02-scheduled-agent` | yes | `leather serve` running a cron-scheduled agent |
| [03](03-shell-skill/) | `03-shell-skill` | yes | An agent that calls local shell tools via `shell-mcp` |
| [04](04-tannery-ingest/) | `04-tannery-ingest` | yes | `leather ingest` → curing worker → artifact |
| [05](05-tannery-webhook/) | `05-tannery-webhook` | yes | HMAC-validated webhook → router → curing → artifact |
| [06](06-multi-agent-curing/) | `06-multi-agent-curing` | yes | Two curings chained via `output.queue` (triage → summarize) |
| [07](07-external-routing/) | `07-external-routing` | yes | External ingress routing via ordered `routes:` (`source` + `event_type`) + optional Telegram notify |
| [08](08-dead-letter-queue/) | `08-dead-letter-queue` | yes | Deterministic worker failure → retry → DLQ (`<queue>-dlq`) |
| [09](09-land-tracker/) | `09-land-tracker` | yes | **Advanced** — scheduled polling agent with Telegram alerts; introduces multi-step polling + notify |
| [10](10-ci-gate/) | `10-ci-gate` | yes | **Advanced** — GitHub webhook → agent gates an expensive CI pipeline via PR analysis and `gh` tool calls |
| [11](11-high-volume-ci/) | `11-high-volume-ci` | yes | **Advanced** — high-volume burst of CI webhooks using `queue_pattern` single-use queues |
| [12](12-spa-maintenance/) | `12-spa-maintenance` | yes | **Advanced** — scheduled SPA health-check agent with artifact persistence |
| [13](13-git-workflow-commit/) | `13-git-workflow-commit` | yes | **Advanced** — `leather workflow run`: concurrent fan-out; planner enqueues per-file GPG commits picked up immediately by executor workers |
| [14](14-sig-triage/) | `14-sig-triage` | yes | **Advanced** — classify unsigged k8s issues → SIG on a small local model; ships the full eval harness (250-issue gold corpus, ablation matrix, paired verdicts) that measured a 62.4→81.6% range on one frozen 4B |

### RPi/Hailo examples

These require a Raspberry Pi 5 with AI HAT+ 2 and Hailo-Ollama on `127.0.0.1:8000`. They are numbered separately (`rpi-NN`) so the mainline sequence stays stable as either track grows.

| # | Example | Demonstrates |
|---|---|---|
| [rpi-01](rpi-01-hailo-endpoint-canary/) | `rpi-01-hailo-endpoint-canary` | Local OpenAI-compatible endpoint canary for Hailo-Ollama |
| [rpi-02](rpi-02-hailo-local-status-digest/) | `rpi-02-hailo-local-status-digest` | Local status snapshot → scheduled digest |
| [rpi-03](rpi-03-hailo-local-status-ingest/) | `rpi-03-hailo-local-status-ingest` | Local status snapshot → hide → curing → artifact |

## Prerequisites

Basic (`01`–`06`): Go 1.22+, `bash`, `curl`.

Webhook examples (`04`–`08`, `10`–`12`): also `openssl` (for HMAC signing).

Advanced (`09`–`14`): also `jq`.  Examples 09 and 10 optionally use the `gh`
CLI and a Telegram bot token; both degrade gracefully if absent. Example 14's
live mode uses `gh`; its eval targets (`14-eval*`) want an OpenAI-compatible
endpoint (vLLM, Ollama, ...) and `python3`.
Example 13 also requires `gpg` and a signing key on the keyring.

RPi/Hailo (`rpi-01`–`rpi-03`): require a Raspberry Pi 5 with AI HAT+ 2,
Hailo-Ollama on `127.0.0.1:8000`, and the OpenAI compatibility proxy on
`http://localhost:8080`.

A quick preflight check:

```bash
command -v openssl jq curl || echo "Install missing tools first"
```

## Quick start

```bash
# From repo root:
make build && make build-shell-mcp

# Zero-dependency smoke test (no LLM required):
cd examples && make 01

# Anything LLM-backed — point at your local endpoint and pick an example:
export LEATHER_LLM_ENDPOINT=http://localhost:11434
export LEATHER_MODEL=llama3
cd examples && make 02

# RPi/Hailo examples default to the local Hailo proxy:
cd examples && make rpi-01
```

For RPi/Hailo targets, override the hardware endpoint separately from the
general examples endpoint:

```bash
LEATHER_RPI_LLM_ENDPOINT=http://pi-host:8080 LEATHER_RPI_MODEL=qwen3:1.7b make rpi-01
```

## Conventions

- Every example lives in its own directory and never touches anything outside it.
- `.state/`, `hides/`, `artifacts/`, and `*.log` are git-ignored.
- LLM-backed examples honor `LEATHER_LLM_ENDPOINT` and `LEATHER_MODEL` and
  default to `http://localhost:11434` + `llama3`.
- Outbound side effects are gated by `LEATHER_DEMO_MODE` (default `dry`);
  `make NN-live` opts into real API calls. The full env-var reference,
  including the dry-mode idiom, is
  [docs/CONVENTIONS.md](../docs/CONVENTIONS.md).
- `make clean` wipes per-example state but leaves source files alone.
- `make help` lists every target with a one-line description.

## Layout of one example

```text
NN-name/
  README.md          # what it shows, how to run it, what to look for
  config.yaml        # leather config (scoped to this example's dirs)
  agents/            # *.agent.md (and *.lifecycle.yaml when scheduled)
  tools/             # *.skill.yaml, *.toolset.yaml (when applicable)
  tannery.yaml       # only present in tannery examples
  curings/           # only present in tannery examples
  sample/            # canned input you can feed in
  scripts/           # helper shell scripts (e.g. send-webhook.sh)
```

## Adding a new example

```
make new-example NAME=<slug>
```

The scaffolder (`scripts/new-example.sh`) allocates the next free `NN`
index, creates the standard tree above, and appends the `NN` / `NN-live`
Makefile targets. It prints the two registrations that stay hand-written —
the index-table row in this README and the `make help` line — plus the
TODOs to replace.

The contract every example follows:

- `make NN` runs the demo in **dry mode** (`LEATHER_DEMO_MODE=dry`): every
  outbound side effect is mocked with fixtures under `sample/dry/`, and
  side-effect tools print `dry-mode: would …` instead of acting.
  `make NN-live` opts into real API calls.
- `scripts/run-demo.sh` sources `../scripts/preflight.sh` for the mode
  banner and fail-fast env checks, and drives its output through the
  example's own `scripts/pretty.sh` copy (no central copy — clone the
  newest sibling's, which the scaffolder does for you).
- The example never touches anything outside its own directory;
  `.state/`, `hides/`, `artifacts/`, and `*.log` are git-ignored.
- Env-var conventions (including the dry-mode idiom) live in
  [docs/CONVENTIONS.md](../docs/CONVENTIONS.md).

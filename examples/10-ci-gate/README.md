# 10-ci-gate

A leather agent that gates an expensive CI eval pipeline by analyzing GitHub
pull requests before deciding whether to trigger a full run.

The target context: a real-time speech and voice AI team running STT/TTS evals
against OpenAI voice models. Each full eval run creates voice datasets, sends
eval configs to a costly inference endpoint, and takes 30–60 minutes. Most PRs
don't need it — docs, formatting, and CI config changes are safe to skip.

## What this shows

- **Webhook intake with HMAC** — GitHub signs every payload; leather validates
  `X-Hub-Signature-256` before accepting it.
- **`event_type` route matching** — `X-GitHub-Event: pull_request` is extracted
  from the header and matched against `event_type: pull_request` in the route.
  A separate push or issue event would hit a different route (or be dropped).
- **Hide → curing → artifact** — the PR JSON is stored as a hide, paged to the
  agent in cuts, and the decision is written as a persistent artifact.
- **Real tool calls via gh CLI** — `get_pr_files` and `get_pr_diff` fetch
  file-level detail for precise reasoning. If `gh` isn't authed, the agent
  falls back to the payload metadata.
- **Actionable output** — FULL_EVAL posts `/test-{sha}` (picked up by prow,
  Buildkite, or any CI bot listening for PR comment commands) and adds the
  `eval-requested` label. SKIP posts a brief rationale and adds `eval-skip`.

## Requirements

- A local OpenAI-compatible endpoint at `$LEATHER_LLM_ENDPOINT`.
- `openssl` (for HMAC; preinstalled on macOS/Linux).
- `curl`.
- `gh` CLI with `gh auth login` for real PR tool calls (optional — the demo
  uses sample payloads against a fictional repo; tool call failures are handled
  gracefully).

## Run

```bash
LEATHER_LLM_ENDPOINT=http://localhost:8000 \
LEATHER_MODEL=/path/to/your/model \
make 10
```

The demo sends `sample/pr-event-eval.json` — a beam search + eval baseline
change that should trigger **FULL_EVAL**. To see the **SKIP** path send the
docs-only variant after the server is running:

```bash
cd examples/10-ci-gate
PAYLOAD_FILE=sample/pr-event-skip.json bash scripts/send-webhook.sh
```

## Decision criteria

| FULL_EVAL | SKIP |
|---|---|
| Model architecture or weights | README / docstrings only |
| Eval config or baseline targets | CI config (no model logic) |
| Voice dataset or data pipeline | Pure reformatting / imports |
| Inference code (beam search, decoding) | Dependency version bump |
| STT/TTS wrappers or audio processing | |
| API handlers for voice endpoints | |

When in doubt the agent chooses **FULL_EVAL**.

## Files

| File | Purpose |
|---|---|
| `config.yaml` | leather config — `tool_dir: tools` loads the github skill |
| `tannery.yaml` | webhook at `/webhooks/github`, `pull_request` route → `ci-gate` curing |
| `mcp-servers.yaml` | registers shell-mcp for gh CLI tools |
| `shell-tools.json` | `get_pr_files`, `get_pr_diff`, `post_pr_comment`, `add_pr_label` |
| `tools/github.skill.yaml` | skill wiring the four tools with graceful-failure guidance |
| `agents/ci-gate.agent.md` | 6-step agent: read payload → fetch files → diff → decide → act |
| `curings/ci-gate.curing.yaml` | binds `github.pull_request` hides to the agent |
| `sample/pr-event-eval.json` | decoder tune + eval baseline bump → **FULL_EVAL** |
| `sample/pr-event-skip.json` | docs-only change → **SKIP** |
| `scripts/run-demo.sh` | full demo: start serve, POST payload, print artifact |
| `scripts/send-webhook.sh` | sign and POST any payload file |

## Production wiring

Point your GitHub repo's webhook settings to your leather server:

| Setting | Value |
|---|---|
| Payload URL | `http://<host>/webhooks/github` |
| Content type | `application/json` |
| Secret | value of `$GITHUB_WEBHOOK_SECRET` |
| Events | **Pull requests** (or send me everything and let the route filter) |

The `GITHUB_WEBHOOK_SECRET` env var is read at serve time via
`{{env:GITHUB_WEBHOOK_SECRET}}` in `tannery.yaml`.

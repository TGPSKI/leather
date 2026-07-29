# Conventions — environment variables

The central reference for every environment variable that carries meaning in
this repo, in one place: name, default, scope, and effect. If you add a new
variable, add a row here (doclint gates docs against the code, and this file
against reality).

## Scopes

- **binary** — read by `leather` (or set by it for child processes). Every
  CLI flag has a matching `LEATHER_<FLAG_NAME>` env var (`--log-level` →
  `LEATHER_LOG_LEVEL`); flag > env > config-file > default. The full
  flag/env table lives in the config reference
  ([GUIDE.md § configuration](GUIDE.md)) and
  [.subagents/AGENTS-SERVE.md](../.subagents/AGENTS-SERVE.md) — this file
  only calls out the load-bearing ones.
- **shell-mcp** — read by the `shell-mcp` companion binary.
- **example shell** — consumed by example scripts and `shell-tools.json`
  command lines at the shell; the leather binary itself never reads them.

## Binary

| Variable | Default | Effect |
|---|---|---|
| `LEATHER_MODEL` | — | Model name sent to the LLM endpoint (`--model`, config `model`). |
| `LEATHER_LLM_ENDPOINT` | — | OpenAI-compatible chat-completions endpoint (`--llm-endpoint`, config `llm_endpoint`). Without it, agents cannot run. |
| `LEATHER_LLM_API_KEY` | — | Bearer key for the endpoint (`--llm-api-key`, config `llm_api_key`). Prefer `env:`/`pass:` secret refs in config over inline values. |
| `LEATHER_LLM_TIMEOUT` | — | Per-LLM-call timeout (`--llm-timeout`). |
| `LEATHER_TOOL_TIMEOUT` | `600s` | Per-tool-call timeout (`--tool-timeout`); `0` disables. |
| `LEATHER_LLM_FIXTURE` | — | Replay a recorded JSONL fixture instead of a live LLM (`--llm-fixture`, config `llm_fixture`). Mutually exclusive with record. |
| `LEATHER_LLM_RECORD` | — | Capture live completions to a replayable JSONL fixture (`--llm-record`, config `llm_record`). |
| `LEATHER_STATE_DIR` | — | State root: queues, run history, `.state` (`--state-dir`). |
| `LEATHER_AGENT_DIR` / `LEATHER_TOOL_DIR` | — | Agent / tool definition directories (`--agent-dir`, `--tool-dir`). |
| `LEATHER_LOG_FILE` / `LEATHER_LOG_LEVEL` | stderr / `info` | Log destination and level (`--log-file`, `--log-level`). |
| `LEATHER_INTAKE_URL` | *(set by leather)* | **Set, not read**: `leather workflow run` and `serve` export it so MCP child processes (e.g. shell-mcp tools) can `POST` hides back to the `/intake` endpoint. |

## shell-mcp

| Variable | Default | Effect |
|---|---|---|
| `SHELL_MCP_CONFIG` | `~/.leather/shell-tools.json` | Path to the tool config. Resolution order: positional argument, then this variable, then the default. |

## Example shell conventions

These are contracts between example scripts, not inputs to the binary.

| Variable | Default | Effect |
|---|---|---|
| `LEATHER_DEMO_MODE` | `dry` | Repo-wide dry/live switch. `make NN` exports `dry` (mocked outbound calls, fixtures from `sample/dry/`); `make NN-live` opts into real API calls. Side-effect tools follow the idiom below. |
| `LEATHER_GIT_SIGNING_KEY` | — | Example 13: GPG key ID (on the current keyring) used to sign the demo commit. Required; the demo fails fast without it. |
| `LEATHER_GIT_DIFF_LINES` | `12` | Example 13: lines of diff shown per file to the planner agent. |
| `GITHUB_WEBHOOK_SECRET` | — | HMAC secret for GitHub-webhook examples; read at serve time via `{{env:GITHUB_WEBHOOK_SECRET}}` in `tannery.yaml`. Note: `workflow run` validates declared webhook secrets even though it never serves the webhook — set the variable or omit the webhook block. |
| `GH_TOKEN` | — | GitHub token for `gh`-CLI-backed tools and corpus-fetch scripts (raises rate limits; required for write operations in live mode). |

### The dry-mode idiom

Every side-effect tool guards on the mode and *narrates* what it would have
done:

```sh
if [ "${LEATHER_DEMO_MODE:-dry}" = live ]; then
  gh pr comment "$1" --body "$2"
else
  echo "dry-mode: would comment on PR $1"
fi
```

Helpers (`lth_demo_mode`, `lth_mode_banner`, fail-fast env checks) live in
`examples/scripts/preflight.sh`; source it from `run-demo.sh`. The
`dry-mode: would …` message shape is load-bearing — demo output and tests
grep for it.

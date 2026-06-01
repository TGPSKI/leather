# 03-shell-skill

An agent that calls **local shell commands as tools** via the `shell-mcp`
companion binary. The agent uses one skill (`repo`) that exposes three git
tools (`git_status`, `git_log`, `git_branch`) over MCP stdio.

## Requirements

- `leather` and `shell-mcp` built (`make build && make build-shell-mcp`
  at the repo root).
- A local LLM at `$LEATHER_LLM_ENDPOINT` (default Ollama).

## Run

```bash
make 03
```

## What this shows

- `*.skill.yaml` mapping logical tool names to MCP server tools.
- `shell-tools.json` manifest format: `command` + templated `args[]`,
  per-call output cap, and `required[]` arguments.
- An agent invoking tools and synthesizing results.

## Files

- `mcp-servers.yaml` — wires the `shell` server up via `shell-mcp -tools shell-tools.json`.
- `shell-tools.json` — defines what the `shell` MCP server exposes.
- `tools/repo.skill.yaml` — bundles the tools into a single skill the agent can attach.
- `agents/inspector.agent.md` — attaches the skill and writes a short summary.

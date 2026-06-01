# 12-spa-maintenance

Three scheduled agents and a webhook-driven curing that keep a SPA website
healthy, audited, and documented — running autonomously on `leather serve`.

```
┌──────────────────────────────────────────────────────────────────┐
│  leather serve (scheduled + tannery webhook)                     │
│                                                                  │
│  site-health    [daily  08:00]  HTTP status + TLS cert check     │
│  dep-audit      [Mon    09:00]  npm outdated + security scan     │
│  content-drift  [Fri    10:00]  README / CHANGELOG version sync  │
│                                                                  │
│  GitHub push ──► /webhooks/github ──► deploy-check curing        │
│                   (HMAC-validated)     verify site is live        │
└──────────────────────────────────────────────────────────────────┘
```

The demo runs all three scheduled agents with `* * * * *` cadence so they fire
within the 90-second window. After the first round, a sample push event is sent
to trigger the deploy-check curing.

## What this shows

- **Native scheduling** — `leather serve` runs all three maintenance jobs on
  their lifecycle cron schedule with no external cron, no shell wrapper, no
  APScheduler or Celery. Compare to goose's headless mode tutorial, which
  documents `0 2 * * * /usr/local/bin/goose run ...` as the scheduling answer.
- **File-defined agents** — each agent is a Markdown file. The schedule, model,
  parameters, and output routing live in a matching `*.lifecycle.yaml`. No
  Python class, no framework, no decorator.
- **Shell tools via shell-mcp** — `curl`, `openssl`, and `npm` are wired as
  callable tools through `shell-tools.json`. The agent calls them by name;
  leather executes them and returns bounded output.
- **Webhook-driven curing** — a GitHub push event is HMAC-validated and routed
  to the `deploy-check` curing. The agent reads the push payload as a hide and
  calls `check_http` to verify the deploy.
- **Artifacts with lineage** — every agent run writes a persistent artifact to
  `.state/artifacts/<agent>/`. Each artifact records curing name, agent name,
  hide id, and timestamp.
- **Graceful degradation** — if `npm outdated` can't run (no `node_modules`),
  `dep-audit` reads `package.json` directly and notes the limitation. If TLS
  check fails, `site-health` reports UNKNOWN rather than crashing.

## Requirements

- A local OpenAI-compatible endpoint at `$LEATHER_LLM_ENDPOINT`.
- `curl` (preinstalled on macOS/Linux).
- `openssl` (for HMAC signing and TLS cert checks — preinstalled on macOS/Linux).
- `npm` (for dep-audit; optional — agent degrades gracefully if absent).
- `shell-mcp` binary (run `make build-shell-mcp` from the repo root).

Optional:
- Telegram bot token for notifications (set `LEATHER_TELEGRAM_BOT_TOKEN` or
  configure `pass: telegram/bot-token` in config.yaml).

## Run

```bash
LEATHER_LLM_ENDPOINT=http://localhost:8000 \
LEATHER_MODEL=/path/to/your/model \
make 12
```

The demo runs for 90 seconds. All three scheduled agents fire within the first
65 seconds (demo cadence: `* * * * *`). After that, a sample push event is
sent and the deploy-check curing runs.

### Manual webhook (after `make 12` is running)

```bash
cd examples/12-spa-maintenance
GITHUB_WEBHOOK_SECRET=spa-demo-secret bash scripts/send-webhook.sh
```

### Production schedules

Swap the demo lifecycle schedules for the production ones by editing each
`*.lifecycle.yaml`:

| Agent | Demo | Production |
|---|---|---|
| `site-health` | `* * * * *` | `0 8 * * *` (daily 08:00) |
| `dep-audit` | `* * * * *` | `0 9 * * 1` (Monday 09:00) |
| `content-drift` | `* * * * *` | `0 10 * * 5` (Friday 10:00) |

### Real site

Replace `site_url` in `agents/site-health.lifecycle.yaml` and `SITE_URL`
in `agents/deploy-check.agent.md` with your actual deployed site URL.

### Real repo

Replace `repo_path: "sample"` in `dep-audit.lifecycle.yaml` and
`content-drift.lifecycle.yaml` with the path to your actual SPA project.
Run `npm install` in that directory so `npm_outdated` can report real data.

## Files

| File | Purpose |
|---|---|
| `config.yaml` | leather config — agent/tool dirs, token budget, API addr |
| `tannery.yaml` | webhook at `/webhooks/github`, push → `deploy-check` curing |
| `mcp-servers.yaml` | registers shell-mcp for all shell tools |
| `shell-tools.json` | 7 tools: `check_http`, `check_tls`, `read_package_json`, `npm_outdated`, `npm_audit`, `git_log`, `read_file` |
| `tools/web.skill.yaml` | skill wrapping HTTP/TLS tools for site-health and deploy-check |
| `tools/repo.skill.yaml` | skill wrapping npm/git/file tools for dep-audit and content-drift |
| `agents/site-health.agent.md` | HTTP + TLS health check agent |
| `agents/site-health.lifecycle.yaml` | daily schedule + site_url parameter + Telegram notify |
| `agents/dep-audit.agent.md` | npm outdated + security audit agent |
| `agents/dep-audit.lifecycle.yaml` | weekly schedule + repo_path parameter |
| `agents/content-drift.agent.md` | README/CHANGELOG version sync agent |
| `agents/content-drift.lifecycle.yaml` | weekly schedule + repo_path parameter |
| `agents/deploy-check.agent.md` | push payload → HTTP verify agent (used by curing) |
| `curings/deploy-check.curing.yaml` | binds `github.push` hides to deploy-check agent |
| `sample/package.json` | demo SPA manifest (v2.4.0, newer than CHANGELOG) |
| `sample/CHANGELOG.md` | demo changelog (last entry: v2.3.1 — intentional version drift) |
| `sample/README.md` | demo README (mentions v2.3.0 — intentional staleness) |
| `sample/push-event.json` | sample GitHub push event for deploy-check demo |
| `scripts/run-demo.sh` | full demo: start serve, wait for scheduled round, send webhook |
| `scripts/send-webhook.sh` | sign and POST any push payload |
| `scripts/pretty.sh` | shared pretty-print helpers |

## Artifacts

After a run, artifacts are written to `.state/artifacts/`:

```
.state/artifacts/
  site-health/       health report per run
  dep-audit/         dependency audit per run
  content-drift/     drift report per run
  deploy-check/      deploy verification per webhook event
```

Each artifact is a JSON file with fields: `id`, `curing_name`, `agent_name`,
`hide_id`, `content` (the agent's final response), `created_at`, and `metadata`.

## Sample drift

The sample files are intentionally out of sync to demonstrate detection:

- `package.json` declares version **2.4.0**
- `CHANGELOG.md` latest entry is **2.3.1** — missing 2.4.0 release notes
- `README.md` says "Version: **2.3.0**" — two minor versions behind

The `content-drift` agent will flag all three on first run.

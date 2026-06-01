# AGENTS-OPERATIONS.md — leather deployment & operations

Subagent guide for **running `leather` in production-like environments**:
reference deployment layouts, install paths, process supervision,
upgrade paths, backup/restore, single-process invariants, secrets
rotation, and metrics export.

Load this guide when:

- Designing or documenting a deployment (workstation, home server, NAS)
- Writing systemd/launchd units or container images
- Changing on-disk layout, file modes, or default paths
- Implementing the `leather upgrade` / migration story
- Reviewing log-rotation, backup, or rollback procedures

For hardening (threat model, secret references, API auth), see
[AGENTS-SECURITY.md](AGENTS-SECURITY.md). For flags and config schema,
see [AGENTS-SERVE.md](AGENTS-SERVE.md). For metrics surface, see
[AGENTS-SERVE.md § runMetrics](AGENTS-SERVE.md). For test-suite gates,
see [AGENTS-QUALITY.md](AGENTS-QUALITY.md).

---

## Scope

Cross-cutting. Owns the operator-facing documentation for **deployment
and lifecycle**, not Go code. When an implementation gap requires code
(e.g. file-mode `WARN` on startup, advisory lock), the implementing
package is jointly responsible with this guide.

---

## Reference layouts

### Workstation (single user)

```
~/.leather/
├── config.yaml                  0600
├── mcp-servers.yaml             0600
├── agents/                      0700
│   ├── *.agent.md               0600
│   └── *.lifecycle.yaml         0600
├── tools/                       0700
│   ├── *.skill.yaml             0600
│   ├── *.toolset.yaml           0600
│   └── *.tool.yaml              0600
├── workers/                     0700
│   └── *.worker.yaml            0600
├── logs/                        0700
│   ├── leather.log
│   └── debug/                   0700
└── .state/                      0700
    ├── leather.lock             0600
    ├── queues/                  0700
    ├── scheduler.json           0600
    ├── cache/                   0700
    └── replay/                  0700
```

leather binary itself: `~/.local/bin/leather` or `/usr/local/bin/leather`.
The companion `shell-mcp` binary follows the same install path
([AGENTS-SHELL-MCP.md](AGENTS-SHELL-MCP.md)).

### Home-network server (systemd user unit)

Same layout as workstation, but `XDG_STATE_HOME` and `XDG_CACHE_HOME`
may redirect parts of `.state/`:

```
~/.config/leather/   → config files
~/.local/share/leather/ → agents, tools, workers
~/.local/state/leather/ → .state, logs (XDG_STATE_HOME)
```

leather respects `LEATHER_STATE_DIR`, `LEATHER_LOG_DIR`,
`LEATHER_AGENT_DIR`, `LEATHER_TOOL_DIR` (see
[AGENTS-SERVE.md](AGENTS-SERVE.md)). XDG mapping is a deployment
convention, not enforced by the binary.

### Multi-user shared server (advisory)

leather v1 is **single-user, single-process per state directory**. To
host multiple users on the same machine:

- One install of the binary, multiple per-user state dirs.
- Each user runs their own `leather serve` instance under their own
  systemd user unit.
- Do **not** share a `.state/` directory across users or processes —
  the queue and scheduler state are not concurrency-safe.

---

## Process supervision

### systemd user unit (recommended on Linux)

```ini
# ~/.config/systemd/user/leather.service
[Unit]
Description=leather agent orchestrator
After=network-online.target

[Service]
Type=simple
ExecStart=%h/.local/bin/leather serve --api
Restart=on-failure
RestartSec=5s
# Optional resource limits
MemoryHigh=512M
MemoryMax=1G

[Install]
WantedBy=default.target
```

Enable with `systemctl --user enable --now leather`. Use
`loginctl enable-linger <user>` so the unit survives logout.

### launchd (macOS)

```xml
<!-- ~/Library/LaunchAgents/dev.leather.plist -->
<plist version="1.0"><dict>
  <key>Label</key><string>dev.leather</string>
  <key>ProgramArguments</key>
  <array>
    <string>/usr/local/bin/leather</string>
    <string>serve</string>
    <string>--api</string>
  </array>
  <key>RunAtLoad</key><true/>
  <key>KeepAlive</key><true/>
  <key>StandardOutPath</key><string>/Users/USER/.leather/logs/leather.log</string>
  <key>StandardErrorPath</key><string>/Users/USER/.leather/logs/leather.log</string>
</dict></plist>
```

Load with `launchctl load ~/Library/LaunchAgents/dev.leather.plist`.

### Container (advisory)

leather is stdlib-only and trivially containerised. Mount the state
directory; never bake secrets into the image; use environment refs
(`env:VAR`) or a mounted pass store.

---

## Single-process invariant

See [AGENTS-SECURITY.md § Single-process invariant](AGENTS-SECURITY.md#single-process-invariant)
for the rule and its required mitigations. Implementation lives in
`internal/cli` and is jointly owned with [AGENTS-SERVE.md](AGENTS-SERVE.md).

---

## Log rotation

leather writes plain JSONL to `~/.leather/logs/leather.log` and debug
streams to `~/.leather/logs/debug/`. The binary does **not** rotate
logs itself.

### Operator playbook

- **logrotate (Linux):**

  ```
  # /etc/logrotate.d/leather  (run as user via systemd timer, or daily root)
  ~/.leather/logs/leather.log {
      daily
      rotate 14
      compress
      delaycompress
      missingok
      notifempty
      copytruncate
  }
  ```

  `copytruncate` is required because leather keeps the file open.

- **journald (when started via systemd):** route stdout/stderr to the
  journal and skip the file log. `journalctl --user-unit leather`
  gives full retention control.

- **Debug-log churn:** `--debug-log` files in `logs/debug/` are
  per-run; manually purge via cron / launchd timer:

  ```
  find ~/.leather/logs/debug -mtime +14 -delete
  ```

---

## Upgrade & state-migration policy

leather has no migration framework yet. Per-version policy:

- **Pre-1.0 releases:** breaking on-disk format changes are allowed;
  the changelog must call them out and provide a manual migration
  recipe.
- **Post-1.0 releases:** any state-format change must include an
  in-process migration on first start, with a one-shot backup copy
  of the prior state directory next to the live one
  (`.state.bak-YYYYMMDD/`).
- **Rollback:** `leather` MUST NOT write to a state directory whose
  on-disk version is newer than the binary. Refuse to start with a
  clear error.

### Operator playbook for an upgrade

```bash
# 1. Stop the service.
systemctl --user stop leather

# 2. Snapshot state.
cp -a ~/.leather/.state ~/.leather/.state.bak-$(date +%Y%m%d)

# 3. Install the new binary.
cp leather ~/.local/bin/leather

# 4. Dry-run validation.
leather validate

# 5. Start.
systemctl --user start leather

# 6. Tail logs.
journalctl --user-unit leather -f
```

---

## Backup & restore

The **state directory** (`~/.leather/.state/`) is the only source of
runtime truth. Config, agents, and tools are recreatable from a dotfile
repo.

### What to back up

| Path | Frequency | Why |
|---|---|---|
| `~/.leather/.state/scheduler.json` | hourly | Job-run history. |
| `~/.leather/.state/queues/` | hourly | In-flight work. |
| `~/.leather/.state/replay/` | daily | Run records (large; trim by age). |
| `~/.leather/config.yaml` | on change | Tracked in dotfiles repo, not backup. |
| `~/.leather/agents/`, `tools/`, `workers/` | on change | Tracked in dotfiles repo, not backup. |
| `~/.leather/.state/cache/` | **do not** | Re-derivable, churn-heavy. |
| `~/.leather/logs/` | optional | Already log-rotated. |

### Snapshot recipe

```bash
tar --exclude '.state/cache' \
    -czf ~/backups/leather-$(date +%Y%m%d-%H%M).tgz \
    -C ~ .leather/.state .leather/config.yaml .leather/agents .leather/tools .leather/workers
```

### Restore recipe

```bash
systemctl --user stop leather
rm -rf ~/.leather/.state
tar -xzf ~/backups/leather-YYYYMMDD-HHMM.tgz -C ~
chmod 700 ~/.leather/.state
systemctl --user start leather
```

---

## Secret rotation playbook

leather resolves `SecretRef` (env / pass) values **at startup** and
holds them in memory for the process lifetime. Rotation requires a
restart in v1.

### Procedure

1. Update the secret in the source (`pass insert leather/telegram/token`
   or `systemctl --user edit leather` for `Environment=`).
2. `systemctl --user restart leather`.
3. Verify with `leather status` or `/jobs` API.

### Roadmap

A SIGHUP-driven re-resolve is on the roadmap; until then, all backends
treat restart as the rotation event. Document this in
[AGENTS-SECURITY.md](AGENTS-SECURITY.md) when the change lands.

---

## Metrics export

The `--api` surface exposes `/metrics` (Prometheus-style text) and
`/runs` / `/jobs` (JSON). For external scraping:

- Loopback bind by default; scrape from the same host.
- For multi-host scraping, use an SSH tunnel or front the API with an
  authenticating reverse proxy.
- The `runMetrics` block in [AGENTS-SERVE.md](AGENTS-SERVE.md) is the
  source of truth for the exported metric names.

---

## Health endpoints

- `GET /healthz` — liveness; returns `200 OK` whenever the HTTP server
  is up.
- `GET /readyz` — readiness; returns `200` only after the scheduler is
  registered and the queue store is opened.
- `GET /version` — `{ "version": "...", "git": "..." }`.

Use `/readyz` for systemd `ExecStartPost=` health checks.

---

## File permission startup audit

`leather serve` should `WARN` on startup if any of the paths listed in
[AGENTS-SECURITY.md § File permission expectations](AGENTS-SECURITY.md#file-permission-expectations)
is found with broader permissions than required. The audit runs once
at startup, never during normal operation.

---

## Common mistakes

| Mistake | Correct approach |
|---|---|
| Running two `leather serve` processes against one state dir | Use the advisory lock; refuse to start the second instance. |
| Sharing `.state/` between users | One state dir per OS user. |
| Mounting `.state/` from a network filesystem with weak locking | Use local disk; NFS without proper `flock` corrupts queues. |
| Rotating logs with `mv` and SIGHUP | Use `copytruncate`; leather does not reopen its log on signal in v1. |
| Backing up `.state/cache/` | Skip it; it's regenerated. |
| Restoring state into a different `--state-dir` without restart | Stop the service first; restore; start. |
| Rotating a `pass` secret without restarting | Resolved values are cached for the process lifetime. |

---

## Verification checklist

Before opening a PR that affects deployment surface:

- [ ] Reference layout table updated if a path or mode changes
- [ ] systemd / launchd snippets in this file still work end-to-end
- [ ] `~/.leather/.state/leather.lock` acquisition test passes
- [ ] Upgrade playbook still applies; backup recipe still excludes `cache/`
- [ ] Cross-link to [AGENTS-SECURITY.md](AGENTS-SECURITY.md) for any
      file-mode or secret change
- [ ] [AGENTS-SERVE.md](AGENTS-SERVE.md) flag/env table reflects any
      new operator knob added here

---

_Last reviewed: 2026-05-19_

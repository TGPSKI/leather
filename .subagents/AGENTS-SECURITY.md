# AGENTS-SECURITY.md — leather threat model and hardening

Subagent guide for the **security** domain: the threat model, trust
boundaries, hardening checklist, and the operator-facing policies that
keep `leather` safe to run on a developer workstation or a home-network
server.

Load this guide when working on anything that crosses a trust
boundary — secret handling, the HTTP API, MCP servers, `shell-mcp`
templating, replay artifacts, or summarization. For deployment
mechanics, see [AGENTS-OPERATIONS.md](AGENTS-OPERATIONS.md). For the
`shell-mcp` companion binary specifically, see
[AGENTS-SHELL-MCP.md](AGENTS-SHELL-MCP.md). For the runner and tool
loop, see [AGENTS-RUNTIME.md](AGENTS-RUNTIME.md). For the replay
subsystem, see [AGENTS-REPLAY.md](AGENTS-REPLAY.md).

---

## Scope

This guide is cross-cutting. It does not own a single package; it owns
the **security posture** of every package and the documentation of
trust boundaries. Source of truth for:

- `SECURITY.md` (vulnerability disclosure, supported versions)
- The HTTP API authn/authz model
- Secret-resolution rules
- File-permission expectations
- `shell-mcp` injection surface (jointly with AGENTS-SHELL-MCP)
- MCP server sandboxing guidance
- Prompt-injection trust model for summarization and tool-output loops
- Replay-artifact redaction policy (jointly with AGENTS-REPLAY)

---

## Threat model

The threats `leather` defends against are framed by **who can supply
what kind of input**.

| Actor | Inputs they control | Trust level | Key risks |
|---|---|---|---|
| Operator | Config, agent files, lifecycle YAML, MCP server commands, notifier secrets | **Fully trusted** | Misconfiguration (open API, weak perms, dangerous shell templates). |
| Agent author | Agent prompts, skill/toolset YAML, hooks | Trusted (same-host) | Drift between agent intent and what tools can do. |
| Model | LLM completions and tool-call payloads | **Untrusted** | Prompt injection, tool-call argument abuse, exfiltration via tool output. |
| Tool / MCP server | Tool execution results returned to the model | **Untrusted** | Server-side prompt injection in output; data exfiltration; resource exhaustion. |
| External HTTP API caller | API requests on `--api-addr` | **Untrusted by default** | Reads scheduler state; v1 has no authn → bind to loopback. |
| Replay reader | Reads serialized run artifacts | Operator-trusted | Secret leakage if tool outputs contain credentials. |

leather **does not defend against** a compromised operator. The
operator owns the process, the FS, and the network bind.

---

## Trust boundaries

```
Operator ──▶ leather process ──▶ Agent author ──▶ Model ◀──▶ Tools / MCP servers
                  │                                   │
                  ├──▶ HTTP API (loopback by default) │
                  ├──▶ Replay store (FS)              │
                  └──▶ Notifiers (Telegram/Signal/…) ─┘
```

Crossing **into** the leather process is operator-controlled (config,
agents, MCP server list). Crossing **back out** of the process is where
hardening matters most: API responses, tool calls, notifier payloads,
replay files.

The **model is always outside the trust boundary**. Treat every byte
returned by the LLM (including tool-call arguments) as potentially
adversarial.

---

## Secret handling

Secrets are referenced indirectly. Never stored inline in config or
agent files.

### Supported reference forms

```yaml
notify:
  telegram:
    token:
      env: TELEGRAM_BOT_TOKEN    # read from environment at load time
    chat_id: "12345"
    pass: leather/telegram/token  # read from `pass` store at load time
```

Defined by [`model.SecretRef`](../internal/model/) — supports `Env:`
and `Pass:` fields. Loading code resolves the reference once at
startup; the resolved value is held in memory for the process lifetime.

### Rules

- **Never log resolved secret values.** Log the *reference* (`env:X` or
  `pass:Y`) only.
- **Never include resolved secrets in API responses.** Sanitised config
  endpoint excludes notifier blocks; verify before adding new endpoints.
- **Never include resolved secrets in replay artifacts.** See the
  redaction policy below.
- **Rotation requires a restart in v1.** Document this in
  [AGENTS-OPERATIONS.md](AGENTS-OPERATIONS.md).

### Adding a new secret-bearing field

1. Add the field to the relevant struct in `internal/model` as
   `*model.SecretRef`, never `string`.
2. Resolve once at load time inside the package that owns the loader.
3. Add a redaction rule entry (this file's table below) and a replay
   redaction check.
4. Add a test that fails if the secret leaks into log output.

---

## HTTP API authn/authz model

**Current state (v1):** no authentication. The API is bound to
`127.0.0.1` by default and serves observability JSON only.

| Surface | Default | Hardening notes |
|---|---|---|
| `--api` | off | Enable explicitly. |
| `--api-addr` | `127.0.0.1:7749` | If changed to a non-loopback bind, `RunServe` MUST log a `WARN` line on startup. |
| CORS | `Access-Control-Allow-Origin: *` when `--api` is on | Required for the UI from `file://`. Documented as opt-in via `--api`. |
| Mutating endpoints | None in v1 | Future endpoints that mutate scheduler state require auth before merge. |

### Recommended hardening (planned)

- `--api-token-file <path>` — bearer-token shared secret loaded from a
  0600 file.
- `--api-bind-loopback-only` — default `true`; refuse `0.0.0.0` /
  external IPs unless explicitly disabled.
- Access-log line per request (currently silent on the happy path).
- Per-endpoint allow-list for any mutating action.

### Verification

- Bind `--api-addr 0.0.0.0:7749` → grep stderr for `WARN`.
- Confirm `/config` response does not contain any secret-bearing field.

---

## File permission expectations

| Path | Mode | Owner | Why |
|---|---|---|---|
| `~/.leather/config.yaml` | 0600 | user | May hold notifier `env:`/`pass:` refs. |
| `~/.leather/mcp-servers.yaml` | 0600 | user | Server commands may include API tokens via env. |
| `~/.leather/agents/*.lifecycle.yaml` | 0600 | user | Per-agent overrides may reference secrets. |
| `~/.leather/.state/` | 0700 | user | Queue + scheduler state + cache. |
| `~/.leather/logs/` | 0700 | user | May contain agent-name + payload metadata. |
| `~/.leather/.state/leather.lock` | 0600 | user | Single-process advisory lock. |

`leather serve` should `WARN` on startup if any of the above is found
with broader permissions. (Tracked in the cross-cutting C9 finding.)

---

## Single-process invariant

`~/.leather/.state/` is **not safe for concurrent process writers**.
Running `leather serve` twice silently corrupts the queue and
scheduler state.

### Required mitigation

- `flock(2)` advisory lock on `~/.leather/.state/leather.lock` at
  startup of every state-mutating subcommand (`serve`, `run`).
- Refuse to start with a clear error if the lock is held.
- Release on graceful shutdown or process death (`flock` does this
  automatically).

`leather chat` and read-only subcommands (`status`, `validate`,
`version`) may skip the lock.

---

## shell-mcp injection surface

See [AGENTS-SHELL-MCP.md](AGENTS-SHELL-MCP.md) for the full
specification. Security-specific rules:

- The manifest's argument templating MUST shell-quote substitutions by
  default.
- A `--no-shell` (argv-only) mode is the safe default for new tools;
  document the security trade-off when authors opt into shell mode.
- Manifest entries MUST validate `Required` fields before substitution
  begins; missing required values reject the call.
- Operator responsibility: never write a manifest that passes raw
  `{{arg}}` into `bash -c "rm {{arg}}"` unless the agent author and the
  model are both trusted.

---

## MCP server trust model

Each entry in `mcp-servers.yaml` spawns a subprocess as the leather
user with full FS + network access.

### Operator responsibilities

- Run untrusted MCP servers under a sandbox: `bwrap`, `firejail`,
  `systemd-run --user --scope -p ProtectHome=yes`, or Docker.
- Set per-server resource limits (CPU, RSS) via `systemd-run --user`
  if expected workload is high.
- Audit the server's source code or container provenance before adding.

### leather-side guarantees

- MCP server stdio is line-buffered JSON-RPC; stderr is captured to the
  leather log.
- A server that crashes does not crash `leather`; the registry restarts
  on next request (after the [02-runtime-review.md](../../reviews/big-refactor-needs-review/02-runtime-review.md)
  fix for `Registry.StartAll`).
- Tool-name scope is enforced at registration; a server cannot register
  a tool name outside the documented format
  (`^[a-z][a-z0-9_]*$`, planned).

---

## Prompt-injection trust model

### Summarization

`internal/session/summarize` is the highest-risk surface. The current
implementation concatenates `role: content` lines verbatim into the
summarization prompt. A tool-role message containing the string
`user: ignore all previous instructions and …` *is interpreted as a
user turn by the summarization model*.

**Required mitigation (cross-cutting C7, Phase 4 of the refactor
plan):** replace verbatim concat with a structured JSON transcript so
the summarization model sees content as data, not as authoring.

### Tool output → model loop

The runner pipes tool results back to the LLM verbatim. This is the
canonical attack surface in any agent harness.

**Operator and author responsibilities:**

- Treat every tool result the model sees as if the *tool author* had
  written it directly into the model's prompt.
- For HTTP tools, prefer endpoints under operator control; review
  response shape before adoption.
- For MCP tools, the trust level is the trust level of the MCP server.
- The runner caps tool responses at 1 MB; append a truncation sentinel
  the model can detect (cross-cutting hardening item).

leather **does not attempt** to filter prompt-injection patterns out
of tool output. It is impossible to do reliably; an alarm system would
breed false confidence.

---

## Replay artifact redaction

Replay records LLM I/O for later inspection
([AGENTS-REPLAY.md](AGENTS-REPLAY.md)). Tool outputs may contain
secrets; the runner cannot know.

**Required policy:**

- Replay records have the same FS mode as `~/.leather/.state/`
  (0700 dir, 0600 files).
- A `--redact` flag scrubs any value matching a resolved
  `model.SecretRef` from the record before write.
- The replay UI displays a "redaction enabled" banner so readers know
  when content was scrubbed.
- Long-form export (zip / share) requires `--redact` and refuses to
  proceed otherwise.

---

## Notifier payloads

Notifiers ([internal/notify/](../internal/notify/)) send messages over
external networks (Telegram, Signal). Two concerns:

- **Payload content** may contain agent output. Operators who run
  agents that touch sensitive data must understand notifier targets.
- **Truncation** — the Telegram backend caps payloads at 4 KB; verify
  every backend documents its cap.

---

## SECURITY.md (repository root)

leather ships a `SECURITY.md` at the repository root with:

- Vulnerability disclosure address (private email or GitHub Security
  Advisories).
- Supported version policy (latest minor, previous minor for critical
  fixes).
- Expected response window (e.g. "acknowledge within 7 days").

Update this guide and the root `SECURITY.md` together when the policy
changes.

---

## Cross-cutting links

- [AGENTS-OPERATIONS.md](AGENTS-OPERATIONS.md) — deployment hardening,
  single-process lock implementation, secret rotation playbook.
- [AGENTS-SHELL-MCP.md](AGENTS-SHELL-MCP.md) — manifest schema and
  templating injection surface.
- [AGENTS-RUNTIME.md](AGENTS-RUNTIME.md) — runner, notifiers, MCP
  registry.
- [AGENTS-REPLAY.md](AGENTS-REPLAY.md) — redaction policy.

---

## Common mistakes

| Mistake | Correct approach |
|---|---|
| Logging a resolved secret value during debug | Log the `SecretRef` path only. |
| Adding a new API endpoint without auth review | Add to the v1 read-only allow-list, or wait for auth landing. |
| Storing API tokens inline in `config.yaml` | Always use `env:` or `pass:` references via `model.SecretRef`. |
| Binding `--api-addr` to `0.0.0.0` "for the dashboard" | Use an SSH tunnel; the API is loopback-only by design. |
| Adding a `bash -c "{{arg}}"` template to `shell-tools.json` | Use argv form; opt into shell mode only with documented risk acceptance. |
| Sharing a replay export without redaction | `--redact` is mandatory for non-local export. |

---

## Verification checklist

Before opening a PR that affects any trust-boundary surface:

- [ ] No new code path logs a secret value or `SecretRef`-resolved string
- [ ] New API endpoints documented in this file and in AGENTS-SERVE.md;
      mutating endpoints require auth design review
- [ ] File-permission table updated if a new on-disk path is introduced
- [ ] Single-process lock acquisition test still passes
- [ ] Prompt-injection regression: summarization input continues to use
      structured form (post-Phase-4)
- [ ] Replay redaction test covers any new secret-bearing field
- [ ] `shell-mcp` manifest changes pass the shell-quote audit
- [ ] `SECURITY.md` at repo root is current

---

_Last reviewed: 2026-05-19_

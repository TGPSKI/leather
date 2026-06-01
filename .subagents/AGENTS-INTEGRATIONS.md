# AGENTS-INTEGRATIONS.md — leather integration authoring

Subagent guide for the **integrations** domain: adding a new notifier,
a new MCP server, a new webhook receiver, or a third-party service
adapter such as Skeptic.

Load this guide when you are answering "how do I add a new X?" for X ∈
{ notifier, MCP server, webhook integration, Skeptic-style external
scanner }. For the runtime that calls these integrations, see
[AGENTS-RUNTIME.md](AGENTS-RUNTIME.md). For the worker / poll loop, see
[AGENTS-WORKER.md](AGENTS-WORKER.md). For secret handling, see
[AGENTS-SECURITY.md](AGENTS-SECURITY.md). For the `shell-mcp`
companion (a frequent MCP target), see
[AGENTS-SHELL-MCP.md](AGENTS-SHELL-MCP.md).

---

## Scope

This guide owns the **authoring patterns** for new integration code.
It does not own the runtime that invokes those integrations
(AGENTS-RUNTIME) or the secret-handling policy itself (AGENTS-SECURITY).

| Integration kind | Owning package | Configured via |
|---|---|---|
| Notifier (chat/IM/email gateways) | `internal/notify` | `config.yaml` `notify:` block |
| MCP server (tool source) | `internal/mcp` + `cmd/shell-mcp` (optional) | `mcp-servers.yaml` |
| HTTP poll worker (webhook-style ingest) | `internal/worker` | `*.worker.yaml` |
| External scanner / report consumer (Skeptic-style) | `internal/notify` or a tool | Mix of the above |

---

## Pattern 1 — Add a new notifier

A notifier delivers an agent's outbound message to a chat target. The
canonical example is Telegram.

### File map

```
internal/notify/notify.go         Notifier interface + Registry
internal/notify/telegram.go       Reference backend
internal/notify/signal.go         Second backend (proves the abstraction)
internal/notify/<your>.go         New backend goes here
internal/notify/<your>_test.go    Required
```

### Interface contract

Every backend implements:

```go
type Notifier interface {
    Name() string                                       // stable id used in config
    Send(ctx context.Context, msg Message) (string, error) // returns provider message id
}
```

### Steps

1. **Pick a stable name.** Lowercase, no whitespace, used in
   `config.yaml`. Example: `discord`.
2. **Create `internal/notify/<name>.go`.** Implement `Notifier`. Take
   constructor arguments from a `NotifyBackendConfig` (see
   `internal/model`).
3. **Resolve secrets through `secret.Resolve`** — never read env vars
   directly inside the backend. The config layer hands you resolved
   values or an error.
4. **Use `http.Client` from the runtime** with a 10–30 s timeout. Set
   `User-Agent: leather/<version>`.
5. **Map HTTP status codes** to `error` clearly: 4xx → permanent,
   5xx → retryable. Return a typed error so the runtime can decide.
6. **Register the backend** in the constructor switch in
   `internal/notify/registry.go` (or wherever the factory lives).
7. **Add a fake** in `internal/notify/fake.go` if the existing fake
   does not already cover your message shape.
8. **Tests:** table-driven success + failure + auth-failure cases
   against `httptest.NewServer`. No live network in CI.
9. **Schema:** extend the `notify:` schema in `internal/schema` to
   accept the new backend's required fields.
10. **Docs:** add a one-row entry to the notify backends table in
    [AGENTS-RUNTIME.md](AGENTS-RUNTIME.md) and a minimal
    `config.yaml` snippet here.

### Minimal `config.yaml` snippet template

```yaml
notify:
  discord:
    webhook_url: ${DISCORD_WEBHOOK_URL}
    timeout: 15s
```

### Failure-mode catalog

| Symptom | Cause | Fix |
|---|---|---|
| `401 Unauthorized` | Wrong/expired token | Rotate secret; see AGENTS-OPERATIONS rotation playbook. |
| `429 Too Many Requests` | Rate-limited | Respect `Retry-After`; do not retry inside `Send`, surface error. |
| Timeout | Network or provider slowness | Bump `timeout` in config; do not raise default. |
| Silent success but no delivery | Webhook URL pointing to wrong channel | Verify URL with `--once` chat run. |

---

## Pattern 2 — Add a new MCP server

leather speaks the Model Context Protocol over stdio. Any compliant
server can be wired in.

### Steps

1. **Choose execution path:**
   - **Inline binary** — write a Go program under `cmd/<your>-mcp/`.
     Use [AGENTS-SHELL-MCP.md](AGENTS-SHELL-MCP.md) as a worked
     example.
   - **External binary** — install separately, reference by path in
     `mcp-servers.yaml`.
2. **Add an entry to `mcp-servers.yaml`:**

   ```yaml
   servers:
     - name: my-server
       command: /usr/local/bin/my-mcp
       args: ["--config", "/etc/my-mcp.yaml"]
       env:
         MY_TOKEN: ${MY_TOKEN}
       timeout: 30s
   ```

3. **Verify with `leather validate`** — schema parsing for
   `mcp-servers.yaml` lives in `internal/schema`.
4. **Expose tools to an agent** by listing the server name in
   `tools:` or via a `toolset:` include. See
   [AGENTS-TOOLS-SKILLS-TOOLSETS.md](AGENTS-TOOLS-SKILLS-TOOLSETS.md)
   for precedence and per-turn scoping.
5. **Trust:** every MCP server is a privileged tool source. Add it to
   the trust-boundary table in
   [AGENTS-SECURITY.md](AGENTS-SECURITY.md) before merging.

### Failure-mode catalog

| Symptom | Cause | Fix |
|---|---|---|
| Server starts but `tools/list` empty | Manifest schema mismatch | Run the server stand-alone and compare to MCP spec; check `initialize` capability flags. |
| Hang on first call | Server blocking on stdin without `initialize` response | Send `initialize` before `tools/call`; runtime does this — your server must respond. |
| Tools appear then vanish next run | Server killed by parent timeout | Increase `timeout` per server or move long work into the tool body. |
| Tool output truncated | `max_output_bytes` exceeded | Configure higher cap in the server's manifest (shell-mcp) or stream less. |

---

## Pattern 3 — Add a new HTTP poll worker (webhook-style ingest)

Workers pull data from an external system on a cadence and write items
to a queue for an agent to consume. See
[AGENTS-WORKER.md](AGENTS-WORKER.md) for the supervisor and queue
contract; this section is the integration-author-facing checklist.

### Steps

1. **Create `*.worker.yaml`** under `--worker-dir`. Required fields:
   `name`, `kind: http-poll`, `url`, `interval`, `queue`.
2. **Authenticate:** use `headers:` with `${SECRET}` references —
   resolved through the same `secret.Resolve` pipeline as notifiers.
3. **Set `dedup_key:`** to a stable per-item field (typically `id`).
   Without it, every poll re-enqueues every item.
4. **Pick an interval that respects upstream rate limits.** Document
   the upstream limit in a comment at the top of the worker file.
5. **Validate** with `leather validate`. Schema rules live in
   `internal/schema`.
6. **Add an agent** that drains the queue (see
   [AGENTS-AGENTDEF.md](AGENTS-AGENTDEF.md) `queue:` field).
7. **Trust:** the response body is untrusted input — the consuming
   agent must treat it as such per the prompt-injection model in
   [AGENTS-SECURITY.md](AGENTS-SECURITY.md).

### Failure-mode catalog

| Symptom | Cause | Fix |
|---|---|---|
| Queue grows unbounded | Agent slower than poll cadence | Lengthen `interval`, shorten agent run, or add `--max-queue` cap. |
| Duplicate items processed | `dedup_key` missing or unstable | Use a server-issued id; never use timestamps. |
| Auth header logged | Header value not redacted in debug logs | File a bug in `internal/worker` log scrubber; never quick-fix in your worker. |

---

## Pattern 4 — Skeptic-style external scanner

Pattern for any service that runs *outside* leather and posts results
*back into* leather (Skeptic is the reference case).

Two valid wirings:

1. **Skeptic → MCP server** — the scanner exposes `tools/list` over
   stdio; an agent calls `skeptic.scan` like any other tool. Follow
   pattern 2 above. This is the recommended path.
2. **Skeptic → HTTP poll worker** — leather pulls a results URL on a
   cadence and enqueues findings for a triage agent. Follow
   pattern 3 above.

Pick the integration shape that matches the upstream's *natural*
output channel; do not adapt a push-based system into a poll loop if
an MCP server is feasible.

---

## Secret resolution rules

All integrations share the same secret-resolution surface. The rules
are owned by [AGENTS-SECURITY.md](AGENTS-SECURITY.md); the summary
relevant to integration authors:

- Reference secrets only as `${ENV_NAME}` or `file:/path/to/secret`.
  Never hard-code.
- Never log a resolved secret value, not even at `debug` level.
- A failed resolution at startup is fatal — fail closed.
- Rotating a secret requires only restarting the process; never
  requires editing source.

---

## Verification checklist

Before opening a PR that adds a new integration:

- [ ] New code under `internal/notify`, `internal/mcp`,
      `internal/worker`, or `cmd/<x>-mcp` only — no business logic in
      `cmd/leather/main.go`.
- [ ] Backend / server / worker is registered and discoverable via
      `leather validate`.
- [ ] Schema entry added in `internal/schema` for any new config
      keys; tests cover required-field enforcement.
- [ ] Secrets resolved through `secret.Resolve` — `grep -r 'os.Getenv'
      internal/notify` shows no new direct reads.
- [ ] Trust-boundary table in [AGENTS-SECURITY.md](AGENTS-SECURITY.md)
      updated for new external actors.
- [ ] Failure-mode catalog above expanded with anything you hit
      during development.
- [ ] Unit tests use `httptest.NewServer`; no live network in CI.
- [ ] Doc cross-reference added in the matching package guide
      (AGENTS-RUNTIME / AGENTS-WORKER) listing the new backend.

---

_Last reviewed: 2026-05-19_

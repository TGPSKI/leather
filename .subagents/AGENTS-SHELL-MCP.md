# AGENTS-SHELL-MCP.md — leather shell-mcp companion binary

Subagent guide for **`cmd/shell-mcp`**: the stdio JSON-RPC MCP server
that exposes operator-defined shell commands as tools to any MCP
client (including `leather` itself).

Load this guide when:

- Editing `cmd/shell-mcp/main.go` or its tests
- Changing the `shell-tools.json` manifest format or templating rules
- Adding or modifying flags / env vars for `shell-mcp`
- Reviewing the shell-injection surface (with [AGENTS-SECURITY.md](AGENTS-SECURITY.md))
- Documenting `shell-mcp` for operators or agent authors

For neighbouring domains, consult the routing table in [AGENTS.md](../AGENTS.md).

---

## Purpose

`shell-mcp` is a **separately-shipped binary** that:

- Reads a manifest file (`shell-tools.json` by default) describing
  callable commands.
- Speaks stdio JSON-RPC 2.0 conformant with the Model Context Protocol.
- Exposes each manifest entry as a tool.
- Executes the command on each `tools/call`, returning stdout/stderr
  as the tool result.

It is a thin, audited bridge between an operator's shell environment
and any MCP-aware agent. **`shell-mcp` is not loaded by `leather`
unless an entry in `mcp-servers.yaml` invokes it.**

---

## Scope

| In scope | Out of scope |
|---|---|
| `cmd/shell-mcp/main.go` and tests | The MCP **client** in `leather` (see [AGENTS-RUNTIME.md](AGENTS-RUNTIME.md)). |
| The `shell-tools.json` manifest schema | Other MCP servers (operator-supplied). |
| Templating, quoting, env-var resolution | `leather`'s tool registry. |
| `--no-shell` (argv-only) mode | Hardening of arbitrary 3rd-party MCP servers. |
| JSON-RPC conformance for tool listing / invocation | The full MCP spec surface beyond `tools/list` and `tools/call`. |

---

## CLI surface

`shell-mcp` is invoked by an MCP client (operator's `mcp-servers.yaml`
entry); it is rarely run interactively except for testing.

```text
shell-mcp [--manifest PATH] [--no-shell] [--log-level LEVEL] [--debug-log PATH]
```

### Flags

| Flag | Env | Default | Description |
|---|---|---|---|
| `--manifest` | `SHELL_MCP_MANIFEST` | `./shell-tools.json` | Path to the manifest file. |
| `--no-shell` | `SHELL_MCP_NO_SHELL=1` | `false` | Refuse to honor manifest entries that use a shell (`bash -c …`); only argv-form entries are exposed. |
| `--log-level` | `SHELL_MCP_LOG_LEVEL` | `info` | `debug`, `info`, `warn`, `error`. |
| `--debug-log` | `SHELL_MCP_DEBUG_LOG` | — | Optional per-process debug stream (stderr-bound by default). |

Stdout is reserved for JSON-RPC frames. **All logs go to stderr.**
Mixing logs into stdout corrupts the JSON-RPC stream.

---

## Manifest format — `shell-tools.json`

```json
{
  "tools": [
    {
      "name": "git-log",
      "description": "Recent git history for the given ref",
      "args": [
        { "name": "ref", "type": "string", "required": true,
          "description": "branch/tag/sha; quoted on use" }
      ],
      "exec": {
        "argv": ["git", "log", "--oneline", "-n", "20", "{{ref}}"]
      },
      "cwd": "{{env.PROJECT_ROOT}}",
      "timeout": "5s"
    },
    {
      "name": "find-large",
      "description": "Find files larger than a threshold (shell form)",
      "args": [
        { "name": "path", "type": "string", "required": true },
        { "name": "size_mb", "type": "int", "required": false, "default": 50 }
      ],
      "exec": {
        "shell": "find {{path|shq}} -type f -size +{{size_mb}}M -print"
      },
      "timeout": "30s"
    }
  ]
}
```

### Top-level

| Key | Type | Required | Description |
|---|---|---|---|
| `tools` | array | yes | One manifest entry per exposed tool. |

### Tool entry

| Key | Type | Required | Description |
|---|---|---|---|
| `name` | string | yes | Tool name; matches `^[a-z][a-z0-9_-]*$`. Becomes the MCP `tools/list` entry. |
| `description` | string | yes | One-line description shown to the LLM. |
| `args` | array | no | Argument schema; see Argument schema below. |
| `exec` | object | yes | Execution config; **exactly one** of `argv:` or `shell:`. |
| `cwd` | string | no | Working directory; templated (`{{env.X}}` and `{{argname}}` allowed). |
| `env` | map[string]string | no | Extra env vars passed to the child. Templated. |
| `timeout` | duration | no | Per-call timeout; default `30s`. |
| `max_output_bytes` | int | no | Truncate combined stdout+stderr at this size; default 1 MiB. |

### Argument schema

| Key | Type | Required | Description |
|---|---|---|---|
| `name` | string | yes | Matches `^[a-z][a-z0-9_]*$`. |
| `type` | enum | yes | `string`, `int`, `bool`. |
| `required` | bool | no | Default `false`. |
| `default` | any | no | Default value used when not supplied. |
| `description` | string | no | Surfaced to the LLM via MCP. |
| `enum` | []string | no | Restrict allowed values (string args only). |

---

## Templating & quoting

Templates use `{{name}}` syntax. Resolution order:

1. `{{argname}}` — substituted from the call's `args`.
2. `{{env.NAME}}` — substituted from the **server process** environment.
3. Anything unresolved is a hard error; the call returns an MCP error
   without executing the command.

### Quoting

| Form | Behavior |
|---|---|
| `{{name}}` in `exec.argv` | Direct substitution as a single argv element. **No shell, no quoting needed.** |
| `{{name}}` in `exec.shell` | **Implicit shell-quote.** Equivalent to `{{name|shq}}`. |
| `{{name|shq}}` | Explicit shell-quote; safe for sh/bash. |
| `{{name|raw}}` | **Unquoted.** Refuses to substitute unless the manifest entry has `"allow_raw": true` and `--no-shell` is off. Audit logged. |
| `{{env.NAME|raw}}` | Same `allow_raw` rule as above. |

`shq` implementation: wrap in single quotes; replace any `'` in the
value with `'\''`. Reject any value containing a NUL byte before
substitution.

### `--no-shell` mode

When `--no-shell` is active:

- Any manifest entry with `exec.shell` is **dropped** from
  `tools/list` and rejected from `tools/call` with a clear error.
- `{{x|raw}}` is unconditionally rejected.
- Only `exec.argv` entries are exposed.

This is the recommended posture for production deployments.

---

## Required-field enforcement

For every `tools/call`:

1. Validate `args` against the schema. Missing `required: true` args
   → MCP error, no execution.
2. Coerce `int` and `bool` types; type mismatch → error.
3. Apply defaults for non-required, omitted args.
4. Enforce `enum` membership where declared.
5. Resolve templating (above).
6. Execute via `os/exec` with the configured `cwd`, env, timeout.

---

## Execution model

- `exec.argv`: `exec.Command(argv[0], argv[1:]...)` after templating.
  No shell.
- `exec.shell`: `exec.Command("sh", "-c", "<rendered shell string>")`.
  POSIX `sh` is required; `bash`-specific syntax is the operator's
  responsibility.

### Output capture

- stdout and stderr are captured separately.
- Returned to the MCP client as a single text-content block with this
  structure:

  ```text
  --- stdout ---
  <stdout>
  --- stderr ---
  <stderr>
  --- exit ---
  exit_code: <int>
  duration: <ms>
  truncated: <bool>
  ```

- Combined output is truncated at `max_output_bytes`; a trailing
  `… [truncated]` sentinel is appended so the model can detect it.
- Non-zero exit is **not** a JSON-RPC error; the model sees the exit
  code in the result.

### Timeouts

- Default 30 s. Override per tool with `timeout:`.
- On timeout, send SIGTERM, wait 1 s, then SIGKILL. The result includes
  `exit: -1, timeout: true`.

---

## JSON-RPC conformance

`shell-mcp` implements the MCP subset:

| Method | Status |
|---|---|
| `initialize` | required; advertises `tools/list_changed: false`. |
| `tools/list` | required; returns manifest entries (subject to `--no-shell` filter). |
| `tools/call` | required; per-call execution as above. |
| `ping` | optional; implement as no-op `200`. |
| Anything else | respond with method-not-found per JSON-RPC 2.0. |

### Framing

- LSP-style: `Content-Length: N\r\n\r\n<N bytes of JSON>` on stdio.
- One request per frame; response framed identically.

### Errors

- JSON-RPC `-32600` invalid request (bad JSON).
- JSON-RPC `-32601` method not found (any unrecognised method).
- JSON-RPC `-32602` invalid params (schema mismatch on a `tools/call`).
- JSON-RPC `-32603` internal error (template resolution, exec failure
  pre-spawn). Spawn-time failures after fork return a normal result
  with non-zero exit, not an error.

---

## Trust model

See [AGENTS-SECURITY.md § shell-mcp injection surface](AGENTS-SECURITY.md#shell-mcp-injection-surface)
for the operator-facing summary. Implementation-side invariants:

- `{{x|raw}}` requires both `allow_raw: true` in the entry **and**
  `--no-shell` being off. Both gates must agree.
- `exec.shell` templates are rendered **after** quoting transformations
  run, never before.
- Argument values containing NUL bytes are rejected pre-render.
- The server never reads from stdin for any purpose other than
  JSON-RPC frames; piping data through `shell-mcp` to a child is not
  supported.

---

## Operator integration with `leather`

Operator's `mcp-servers.yaml`:

```yaml
servers:
  - name: shell
    command: ["shell-mcp", "--manifest", "/home/me/.leather/shell-tools.json", "--no-shell"]
```

Each manifest tool is then addressable in `leather` as
`shell/<tool-name>` (see [AGENTS-TOOLS-SKILLS-TOOLSETS.md](AGENTS-TOOLS-SKILLS-TOOLSETS.md)
for naming and collision rules).

---

## Common mistakes

| Mistake | Correct approach |
|---|---|
| Writing log lines to stdout for "easy debugging" | Stdout is JSON-RPC only. Use stderr. |
| Using `exec.shell` with naked `{{arg}}` (no `\|shq`) | Either switch to `argv:`, or rely on the implicit shell-quote (do not opt into `raw`). |
| Adding a new manifest field without updating the schema audit | Update this file and the manifest validator in lockstep. |
| Forgetting `--no-shell` in a production `mcp-servers.yaml` entry | Default to `--no-shell`; opt in to shell mode only with a documented reason. |
| Implementing `tools/list_changed: true` | Not supported in v1; manifest is read once at startup. |
| Returning a JSON-RPC error for a non-zero child exit | Non-zero exit is normal data; only protocol errors return JSON-RPC errors. |

---

## Verification checklist

Before opening a PR that affects `shell-mcp`:

- [ ] `go test ./cmd/shell-mcp/...` passes
- [ ] Manifest schema change reflected in this file's tables
- [ ] Templating change covered by `shq` / `raw` / unresolved-var tests
- [ ] `--no-shell` mode exercised in tests (drops `shell:` entries; rejects `raw`)
- [ ] JSON-RPC conformance: `initialize`, `tools/list`, `tools/call`
      round-trip against a minimal in-process client
- [ ] Timeout / SIGKILL fallback test still passes
- [ ] [AGENTS-SECURITY.md](AGENTS-SECURITY.md) cross-references still
      describe the live behavior

---

_Last reviewed: 2026-05-19_

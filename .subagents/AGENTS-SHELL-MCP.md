# AGENTS-SHELL-MCP.md — leather shell-mcp companion binary

Subagent guide for **`cmd/shell-mcp`**: the stdio JSON-RPC MCP server
that exposes operator-defined shell commands as tools to any MCP
client (including `leather` itself).

Load this guide when:

- Editing `cmd/shell-mcp/main.go` or its tests
- Changing the `shell-tools.json` config format or templating rules
- Reviewing the shell-injection surface (with [AGENTS-SECURITY.md](AGENTS-SECURITY.md))
- Documenting `shell-mcp` for operators or agent authors

For neighbouring domains, consult the routing table in [AGENTS.md](../AGENTS.md).

---

## Purpose

`shell-mcp` is a **separately-shipped binary** that:

- Reads a JSON config file (`shell-tools.json`) describing callable
  commands.
- Speaks JSON-RPC 2.0 over stdin/stdout (newline-delimited JSON),
  conformant with the Model Context Protocol.
- Exposes each config entry as a tool.
- Executes the command on each `tools/call`, returning stdout as the
  tool result.

It is a thin, audited bridge between an operator's shell environment
and any MCP-aware agent. **`shell-mcp` is not loaded by `leather`
unless an entry in `mcp-servers.yaml` invokes it.** Zero third-party
dependencies (stdlib only).

---

## Scope

| In scope | Out of scope |
|---|---|
| `cmd/shell-mcp/main.go` and tests | The MCP **client** in `leather` (see [AGENTS-RUNTIME.md](AGENTS-RUNTIME.md)). |
| The `shell-tools.json` config schema | Other MCP servers (operator-supplied). |
| `{{key}}` templating, pattern validation | `leather`'s tool registry. |
| JSON-RPC conformance for tool listing / invocation | The full MCP spec surface beyond `initialize`, `tools/list`, and `tools/call`. |

---

## CLI surface

`shell-mcp` is invoked by an MCP client (operator's `mcp-servers.yaml`
entry); it is rarely run interactively except for testing.

```text
shell-mcp [/path/to/shell-tools.json]
SHELL_MCP_CONFIG=/path/to/shell-tools.json shell-mcp
```

There are **no flags**. Config path resolution order:

1. First positional argument.
2. `SHELL_MCP_CONFIG` environment variable.
3. `~/.leather/shell-tools.json` (must exist, else startup fails).

Stdout is reserved for JSON-RPC frames. **All logs go to stderr.**
Mixing logs into stdout corrupts the JSON-RPC stream.

---

## Config format — `shell-tools.json`

```json
{
  "output_cap_bytes": 4000,
  "tools": [
    {
      "name": "git_log",
      "description": "Recent git history for the given ref",
      "command": "git",
      "args": ["log", "--oneline", "-n", "20", "{{ref}}"],
      "required": ["ref"],
      "patterns": { "ref": "^[A-Za-z0-9._/-]+$" },
      "timeout_seconds": 5
    },
    {
      "name": "find_large",
      "description": "Find files larger than a threshold (MB)",
      "command": "bash",
      "args": ["-c", "find \"$1\" -type f -size +\"$2\"M -print", "--", "{{path}}", "{{size_mb}}"],
      "required": ["path"],
      "defaults": { "size_mb": "50" },
      "timeout_seconds": 30
    }
  ]
}
```

### Top-level

| Key | Type | Required | Description |
|---|---|---|---|
| `output_cap_bytes` | int | no | Server-wide output cap; default 4000 bytes. |
| `tools` | array | yes | One entry per exposed tool. |

### Tool entry

| Key | Type | Required | Description |
|---|---|---|---|
| `name` | string | yes | Tool name (snake_case). Becomes the MCP `tools/list` entry. |
| `description` | string | yes | One-line description shown to the LLM. |
| `command` | string | yes | Executable name, looked up via `PATH`. |
| `args` | []string | no | Argument list; `{{key}}` placeholders substituted at call time. |
| `required` | []string | no | Argument keys that must be present in every call; a missing key fails the call before execution. |
| `patterns` | map[string]string | no | RE2 regexps the substituted value must match. A missing argument validates as the empty string, so anchored patterns also reject absent values. Advertised in the tool's `inputSchema`. |
| `defaults` | map[string]string | no | Fallback values for optional argument keys (call args win). |
| `optional` | bool | no | When true, a `command` not on `PATH` returns a graceful "not installed" message instead of an error. |
| `timeout_seconds` | int | no | Per-call execution timeout; default 30 s. |
| `output_cap_bytes` | int | no | Per-tool output cap; overrides the server-wide cap. |

All argument values are strings — there is no type/enum system. The
advertised `inputSchema` declares every known key (from `required`,
`defaults`, and `patterns`) as `"type": "string"`, with `pattern`
attached where configured. `required` is always emitted as an array
(never `null`) so schema-to-grammar backends accept it.

---

## Templating & quoting

Templates use `{{key}}` syntax inside `args` elements. Substitution is
literal string replacement of the merged (defaults ∪ call args) values,
each landing inside a single argv element.

- **No shell is involved** unless the operator's `command` is itself a
  shell. Direct `command` + `args` execution needs no quoting.
- To run a pipeline or globbing, use the `bash -c` idiom and pass
  model-supplied values as positional parameters **after `--`**, never
  spliced into the script string:
  `"args": ["-c", "script using \"$1\"", "--", "{{value}}"]`.
- Constrain any argument that reaches a sensitive position with a
  `patterns` regexp; rejected calls fail before the command runs.

### Call validation order

For every `tools/call`:

1. Merge `defaults` under the call's arguments (call args win).
2. Enforce `required` keys — missing keys → tool error, no execution
   (otherwise the command would run with a literal, unsubstituted
   `{{placeholder}}`).
3. Validate `patterns` — mismatch → tool error, no execution.
4. Substitute `{{key}}` placeholders in each `args` element.
5. Execute with the configured timeout.

---

## Execution model

- `exec.CommandContext(command, args...)` after templating — the child
  is spawned directly, with the server's environment and working
  directory.
- Only **stdout** is captured as the result. On non-zero exit, stderr
  is folded into the error message (`exit <code>: <stderr>`).
- A failed execution returns an MCP result with `isError: true` and an
  `error: …` text block — **not** a JSON-RPC error — so the client can
  distinguish tool failure from protocol failure and stop retrying
  deterministic errors.
- `optional: true` tools whose `command` is missing from `PATH` return
  a friendly "not installed" message as a successful result.

### Output capping

Output is truncated at the effective cap (per-tool
`output_cap_bytes`, else server-wide, else 4000 bytes), trimmed to the
last complete line, with a trailing `[output capped]` sentinel so the
model can detect truncation.

### Timeouts

Default 30 s per call; override per tool with `timeout_seconds`. The
child process is killed via context cancellation when the deadline
passes.

---

## JSON-RPC conformance

`shell-mcp` implements the MCP subset:

| Method | Status |
|---|---|
| `initialize` | required; advertises protocol `2024-11-05`, `tools` capability. |
| `tools/list` | required; returns all config entries with generated `inputSchema`. |
| `tools/call` | required; per-call execution as above. |
| notifications (no `id`) | ignored (e.g. `notifications/initialized`). |
| Anything else | `-32601` method not found per JSON-RPC 2.0. |

### Framing

- **Newline-delimited JSON** on stdin/stdout: one JSON-RPC message per
  line (max 1 MiB). Not LSP `Content-Length` framing.
- Malformed frames are skipped silently.

### Errors

- JSON-RPC `-32600` invalid params (params that fail to unmarshal).
- JSON-RPC `-32601` method not found (any unrecognised method).
- JSON-RPC `-32602` unknown tool name on a `tools/call`.
- Execution failures (missing required args, pattern mismatch,
  non-zero exit, spawn failure) return a **result** with
  `isError: true`, not a JSON-RPC error.

---

## Trust model

See [AGENTS-SECURITY.md § shell-mcp injection surface](AGENTS-SECURITY.md#shell-mcp-injection-surface)
for the operator-facing summary. Implementation-side invariants:

- Every config entry is exposed as-is — there is no hardening mode
  that filters entries. **An untrusted `shell-tools.json` must be
  vetted before it is pointed at**; the config file is the trust
  boundary.
- Model-supplied values are substituted into single argv elements,
  never into a shell string, unless the operator's own entry routes
  them through a shell (`command: bash` + `-c`). Entries that do so
  must pass values as positional parameters after `--`.
- `patterns` are the per-argument guard; anchored patterns also reject
  absent values.
- The server never reads stdin for any purpose other than JSON-RPC
  frames; piping data through `shell-mcp` to a child is not supported.

---

## Operator integration with `leather`

Operator's `mcp-servers.yaml`:

```yaml
servers:
  - name: shell
    command: ["shell-mcp", "/home/me/.leather/shell-tools.json"]
```

Each config tool is then addressable in `leather` as
`shell/<tool_name>` (see [AGENTS-TOOLS-SKILLS-TOOLSETS.md](AGENTS-TOOLS-SKILLS-TOOLSETS.md)
for naming and collision rules).

---

## Common mistakes

| Mistake | Correct approach |
|---|---|
| Writing log lines to stdout for "easy debugging" | Stdout is JSON-RPC only. Use stderr. |
| Splicing `{{key}}` into a `bash -c` script string | Pass values as positional parameters after `--`: `["-c", "… \"$1\"", "--", "{{key}}"]`. |
| Adding a new config field without updating the schema audit | Update this file's tables and the doclint shell-tools check in lockstep. |
| Leaving a sensitive argument unconstrained | Add an anchored `patterns` regexp; it rejects both bad and absent values before execution. |
| Implementing `tools/list_changed: true` | Not supported; config is read once at startup. |
| Returning a JSON-RPC error for a failed execution | Execution failures are `isError: true` results; only protocol errors return JSON-RPC errors. |

---

## Verification checklist

Before opening a PR that affects `shell-mcp`:

- [ ] `go test ./cmd/shell-mcp/...` passes
- [ ] Config schema change reflected in this file's tables
- [ ] Templating change covered by substitution / missing-required /
      pattern-mismatch tests
- [ ] JSON-RPC conformance: `initialize`, `tools/list`, `tools/call`
      round-trip against a minimal in-process client
- [ ] `required` is emitted as an array for zero-argument tools
- [ ] Timeout and output-cap overrides exercised in tests
- [ ] [AGENTS-SECURITY.md](AGENTS-SECURITY.md) cross-references still
      describe the live behavior

---

_Last reviewed: 2026-07-29_

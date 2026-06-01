# secret

> Resolve credentials from operator-friendly sources without logging the value.

## Responsibility

`secret` provides a single `Ref` type and `Resolve` function used everywhere
leather needs a credential at runtime: notify backends (Telegram bot tokens,
Signal RPC endpoints), HTTP tools needing bearer auth, and webhook signing
secrets. Resolution is performed once at request time and the value is never
written to logs.

## Public API

| Symbol | Signature | Description |
|---|---|---|
| `Ref` | `type Ref struct { Value, Pass, Env string }` | A credential lookup with up to three sources tried in order. |
| `(Ref).IsZero` | `() bool` | True when no source is configured (caller treats as "no auth"). |
| `Resolve` | `(ctx context.Context, r Ref) (string, error)` | Returns the resolved value or an error. |

## Resolution order

1. **`Value`** — inline literal. Non-empty short-circuits resolution. Intended for tests and bring-up; never the recommended production form.
2. **`Pass`** — Unix pass-store path; runs `pass show <path>` (5-second timeout) and uses the first non-empty line of stdout.
3. **`Env`** — environment variable name read via `os.Getenv`.

`Pass` and `Env` form a fallback pair: if `Pass` is set but `pass show` fails
or returns empty, `Env` is consulted. This mirrors the existing `MCPEnvVar`
semantics in `internal/mcp/env.go`.

`Resolve` returns `("", nil)` when `r.IsZero()`. A non-nil error is returned
only when a configured source was tried and produced nothing usable — never
when the secret is intentionally absent.

## Internal Design

- **5-second timeout** on `pass show` to avoid hanging on a stuck `gpg-agent` prompt.
- **No logging** — the resolved string is returned to the caller and never passed to the logger. Errors include the `pass`/`env` *names* but never the *value*.
- **`exec.LookPath("pass")`** is called per resolution; a missing `pass` binary returns a clear error.

## Dependencies

Stdlib only — no intra-project imports.

## Test Surface

`internal/secret/secret_test.go`:

- `Value` short-circuit returns the literal.
- `IsZero` returns `("", nil)` from `Resolve`.
- `Pass` failure falls through to `Env`.
- All-sources-empty returns a wrapped error mentioning the configured names.

## Related Docs

- [docs/modules/notify.md](notify.md)
- [docs/modules/mcp.md](mcp.md)
- [.subagents/AGENTS-SECURITY.md](../../.subagents/AGENTS-SECURITY.md)

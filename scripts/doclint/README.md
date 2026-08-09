# doclint — deterministic documentation-consistency gate

`doclint` extracts every documented identifier that must have a code referent and
asserts it resolves against ground truth scanned from the repo. Documented tokens
with no referent are violations; any violation exits non-zero. Zero third-party
dependencies (stdlib Go only).

It exists because the docs drift from the code in one repeatable shape: a
documented string — a flag, an env var, an HTTP endpoint, a shell-tools field —
with nothing behind it. This gate catches that class mechanically so it can't
ship.

## What it checks

| Check | Documented token | Ground truth | Catches |
| ----- | ---------------- | ------------ | ------- |
| `flag` | `` `--flag` `` in inline code | `fs.*("flag", …)` registrations | flags that don't exist (`--no-shell`, `--metrics-public`, `--debug-api`) |
| `env` | `LEATHER_*` / `SHELL_MCP_*` tokens | `env*("NAME")` + `os.Getenv("NAME")` + `os.Setenv("NAME")` | phantom env (`LEATHER_LOG_DIR`, `SHELL_MCP_MANIFEST`) |
| `endpoint` | `` `/path` `` on an HTTP-verb line | `mux.Handle(Func)?("/…")` (prefix-aware) | routes not served (`/status/agents`, `/replay/runs`) |
| `shell-tools` | ```` ```json ```` tool blocks | shell-mcp `toolDef` shape | removed `exec.*` form; missing `command` |
| `export` | — | exported symbols in `internal/` | an export with no row in its `docs/modules/<pkg>.md` |
| `module-doc` | — | package directories under `internal/` | a package with no module doc at all |
| `baseline` | — | `undocumented.txt` | stale entries; ledger growth past `max` |

Prefix routes are honored: `/queues/{name}/requeue` is covered by a registered
`/queues/` handler. Endpoint lines tagged *Planned/future/n-a* are exempt.

The first four checks run **doc → code**: they catch phantom documentation. The
last three run **code → doc**, which catches the opposite failure — an export
added without a doc row. Only the forward direction sees silent rot, because a
missing doc row produces no token for the reverse checks to test.

## Export baseline

`undocumented.txt` is a **debt ledger**, distinct from `allow.txt`: every line is
a doc row someone still owes, not a deliberate exception. It is validated in both
directions so it can only shrink.

| Situation | Result |
| --------- | ------ |
| Export undocumented, not in ledger | `export` violation — document it, or add a line |
| Ledger entry now documented | `baseline` violation — **delete the line** |
| Ledger entry no longer exists | `baseline` violation — **delete the line** |
| Entry count exceeds `# max: N` | `baseline` violation |
| Package has no module doc | `module-doc` violation — **not silenceable by the ledger** |

Rows 2 and 3 are what force the shrink: paid debt cannot sit in the file looking
unpaid. Row 4 is the growth ratchet — an allowlist that only blocks additions
still grows, so the count is capped. Lowering `max` is a one-line commit made
while cleaning up; raising it is a conspicuous one-line diff a reviewer will
question. Row 5 keeps a missing *file* from being filed as N missing *symbols*.

Regenerate after documenting something:

```
go run ./scripts/doclint -write-baseline
```

It refuses to write when the count would exceed `max`, so it cannot be used to
launder new drift into the ledger.

Matching is whole-word on the symbol name, so a table row like
`` | `(*Bus).Publish` | … | `` documents `Publish`. Nested packages resolve to a
parent doc: `internal/devtools/bus` is covered by `docs/modules/devtools.md`.

## Usage

```
go run ./scripts/doclint                     # lint docs/, .subagents/, README.md, AGENTS.md
go run ./scripts/doclint -json               # machine-readable violations
go run ./scripts/doclint -write-baseline     # regenerate the export ledger
go run ./scripts/doclint -docs docs -src internal,cmd -allow scripts/doclint/allow.txt
```

Exit 0 = clean, 1 = violations, 2 = scan error.

## Allowlist

`allow.txt` holds tokens that are intentionally documented but have no leather
referent (external service endpoints, browser globals, UI design tokens, naming
placeholders). Keep it short — it is an exception list, not a place to hide drift.
Per-line inline exceptions are also supported: `<!-- doclint:allow /some/path -->`.

A whole file can opt out with `<!-- doclint:disable-file -->` near its top — for
documents that quote drift *by design* (audit reports, post-mortems). Never use
it to silence drift in a live doc.

## Known limitations (deliberately out of scope)

- **Semantic claims** it can't see: e.g. `/metrics` is documented as Prometheus
  text but the handler returns JSON. Existence passes; the content-type mismatch
  needs a typed API contract. Track separately.
- **Schema ↔ defs.go parity** (a field valid at runtime but missing from a JSON
  schema) is a code-vs-code check, not doc-vs-code — see the schema-drift issue.
- **CSS custom properties** documented as inline-code in a table
  (`` `--accent` ``) are syntactically identical to CLI flags; the shipped
  allowlist enumerates the UI token set.
- Flags are only checked in inline-code spans; a real flag shown only in an
  un-backticked prose sentence won't be validated (low-value, high-noise).

## Wiring

Add to CI (see `ci-doclint.yml` in the audit bundle) and/or `make doclint`. Run
`go test ./scripts/doclint` to verify the linter's own logic.

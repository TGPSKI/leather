# safepath

> Anchor-relative path validation for every persistent store.

## Responsibility

`safepath` rejects paths that would escape a configured root. Every leather
store (hide, artifact, queue, cache, run-history, devtools token, tool
`OutputFile`) routes file operations through `Anchor` so a malicious
`hide_id` like `../../etc/passwd` cannot read or write outside the
state directory.

## Public API

| Symbol | Signature | Description |
|---|---|---|
| `Anchor` | `(root, name string) (string, error)` | Resolve `name` underneath `root`, rejecting absolute paths, paths containing `..` segments after cleaning, and paths whose `filepath.Clean` result escapes `root`. Returns the absolute joined path on success. |

## Internal Design

`Anchor` performs four checks:

1. `name` must be non-empty.
2. `filepath.IsAbs(name)` must be false.
3. `filepath.Clean(name)` must not start with `..`.
4. The cleaned join `filepath.Join(root, name)` must remain within `root`
   (verified with a prefix check on `filepath.Clean(root)`).

A failure on any check returns an error wrapped as
`safepath/Anchor: <reason>`. Callers must treat any error as fatal for
the affected operation.

## Dependencies

Stdlib only — no intra-project imports.

## Test Surface

`internal/safepath/safepath_test.go` exercises:

- Absolute paths rejected.
- Traversal (`../foo`, `a/../../etc`) rejected.
- Empty name rejected.
- Valid relative names (`foo`, `a/b/c.json`) joined correctly.
- Paths with embedded `..` that resolve back inside root accepted only
  when `filepath.Clean` keeps them in-bounds.

## Related Docs

- [docs/modules/hide.md](hide.md)
- [docs/modules/artifact.md](artifact.md)
- [docs/modules/queue.md](queue.md)
- [docs/modules/cache.md](cache.md)
- [.subagents/AGENTS-SECURITY.md](../../.subagents/AGENTS-SECURITY.md)

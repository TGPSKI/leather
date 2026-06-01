# artifact

> Stabilized curing output with lineage and content-addressed IDs.

## Responsibility

`artifact` persists the final output of a curing as a tamper-evident,
content-addressed record. Each artifact captures the curing name, agent name,
correlation ID, parent hide IDs, output content, and timestamp, and is written
under a safepath-anchored directory. Artifacts are append-only and queryable
by curing name or ID.

## Public API

| Symbol | Signature | Description |
|---|---|---|
| `Store` | `type Store struct { ... }` | Filesystem-backed artifact store rooted at a single directory. |
| `NewStore` | `(dir string) *Store` | Construct a store rooted at `dir`. The directory is created on first `Write`. |
| `(*Store).Write` | `(a model.Artifact) error` | Persist an artifact. Atomic write via tmp-file + rename, mode `0600`. |
| `(*Store).List` | `() ([]model.Artifact, error)` | All artifacts, ordered by creation time descending. |
| `(*Store).ListByCuring` | `(curingName string) ([]model.Artifact, error)` | Artifacts produced by one curing. |
| `(*Store).Get` | `(id string) (model.Artifact, error)` | Look up by artifact ID. |
| `GenerateArtifactID` | `() string` | Generate a fresh content-addressable ID (`art-<8-hex>`). |

## Internal Design

- **Filename** = `<id>.json`. ID generation uses `crypto/rand` + hex encoding.
- **Path safety**: all reads and writes go through `safepath.Anchor` to prevent
  traversal out of the configured root.
- **Lineage**: `model.Artifact` carries `ParentHideIDs []string`,
  `CuringName string`, `CorrelationID string`. The DevTools causality engine
  links a `tannery.artifact_written` event back through the originating hide.
- **Permissions**: directory `0700`, files `0600`.

## Dependencies

| Package | Why |
|---|---|
| `internal/model` | `Artifact` struct. |
| `internal/safepath` | Anchor every file operation to the store root. |

## Test Surface

`internal/artifact/store_test.go`:

- Round-trip: write then `Get` returns identical fields.
- `ListByCuring` filters correctly across multiple curings.
- Path-traversal IDs are rejected.

## Related Docs

- [docs/modules/curing.md](curing.md)
- [docs/modules/hide.md](hide.md)
- [docs/modules/safepath.md](safepath.md)

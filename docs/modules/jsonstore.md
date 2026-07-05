# jsonstore

> Shared JSON-on-disk persistence built on atomic file writes.

## Responsibility

`jsonstore` is the common persistence helper for packages that want the
"marshal one value to JSON, write atomically, or load it back if present"
pattern without each store re-implementing it. It sits one level above
`fileutil`, turning ordinary Go values into stable on-disk JSON blobs.

## Public API

| Symbol | Signature | Description |
|---|---|---|
| `Save` | `func Save(path string, v any, perm os.FileMode) error` | Marshal one value to JSON and atomically write it to disk. |
| `Load` | `func Load(path string, v any) (found bool, err error)` | Read JSON from disk into `v`, distinguishing "missing file" from decode errors. |

## Internal Design

The important behaviour here is that `Load` returns `(false, nil)` when a file
is absent. Callers such as cache, artifact, hide, and scheduler state stores
can therefore treat missing state as empty state instead of a hard failure.

`Save` deliberately delegates atomicity to `fileutil.AtomicWriteFile`. That
keeps marshal/unmarshal concerns separate from the filesystem replacement
mechanics and gives every caller the same parent-directory creation and cleanup
semantics.

## Dependencies

| Package | Why |
|---|---|
| `internal/fileutil` | Provides the atomic write primitive. |

## Data Flow

```mermaid
flowchart LR
    VALUE[Go value] --> SAVE[Save]
    SAVE --> JSON[encoding/json]
    JSON --> FILEUTIL[fileutil.AtomicWriteFile]
    FILEUTIL --> DISK[JSON file]
    DISK --> LOAD[Load]
    LOAD --> VALUE2[Go value]
```

## Test Surface

`internal/jsonstore/jsonstore_test.go` covers atomic saves, successful loads,
missing-file behaviour, and JSON decode failures.

## Related Docs

- [docs/modules/fileutil.md](fileutil.md)
- [docs/modules/cache.md](cache.md)
- [docs/modules/artifact.md](artifact.md)
- [docs/modules/hide.md](hide.md)
- [docs/modules/scheduler.md](scheduler.md)
- [docs/ARCHITECTURE.md](../ARCHITECTURE.md)

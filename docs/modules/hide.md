# hide

> Persistent raw input store and in-memory paginated buffer for oversized tool output.

## Responsibility

`hide` is the tannery's input layer. It has two cooperating pieces:

1. **`Store`** — content-addressed on-disk store of raw inputs ("hides")
   ingested via `leather ingest`, webhooks (`/intake`), or the runner when a
   tool result exceeds the inline budget. Hides are flock'd, immutable once
   written, and addressed by `<source>-<8-hex>` IDs.
2. **`HideBuffer`** — in-process pagination layer. The runner holds one
   `HideBuffer` per turn; oversized tool results are stored once and exposed
   to the model as fixed-size cuts retrieved through the `hide_next`,
   `hide_jump`, and `hide_search` tools rather than dumping the full payload
   into the next prompt.

## Public API

### Types

| Symbol | Description |
|---|---|
| `Hide` | One stored input: `ID`, `Source`, `Content`, `CreatedAt`. |
| `Cut` | One page of a hide: `HideID`, `Page`, `TotalPages`, `Content`, plus optional reflection-hint preface. |
| `StoreEntry` | On-disk hide metadata: `ID`, `Kind`, `Source`, `SizeBytes`, `Meta`, `CreatedAt`. |
| `Store` | Persistent flock-protected directory store. |
| `HideBuffer` | In-memory paginator keyed by hide ID. |

### Functions — `HideBuffer`

| Symbol | Signature | Description |
|---|---|---|
| `NewHideBuffer` | `(pageSize int) *HideBuffer` | Construct a buffer with the given byte page size. |
| `(*HideBuffer).Store` | `(source, content string) *Hide` | Add a hide; returns the assigned ID. |
| `(*HideBuffer).Get` | `(id string) (*Hide, bool)` | Direct lookup. |
| `(*HideBuffer).Cut` | `(id string, page int) (Cut, error)` | Return one page of the hide. |
| `(*HideBuffer).Search` | `(id, query string) (Cut, bool, error)` | Return the page containing the first occurrence of `query`. |
| `(*HideBuffer).NeedsPaging` | `() bool` | True when the most-recently stored hide exceeds `pageSize`. |
| `(*HideBuffer).FirstCut` | `() (Cut, error)` | First page of the most-recently stored hide. |
| `ToolDefs` | `() []model.ToolDefinition` | Definitions for `hide_next`, `hide_jump`, `hide_search` exposed to agents. |

### Functions — `Store`

| Symbol | Signature | Description |
|---|---|---|
| `NewStore` | `(dir string) *Store` | Open or create the on-disk store. Directory mode `0700`. |
| `(*Store).Put` | `(kind, source string, content []byte, meta map[string]string) (StoreEntry, error)` | Persist one hide. flock-coordinated. |
| `(*Store).Get` | `(id string) (StoreEntry, []byte, error)` | Read metadata and bytes. |
| `(*Store).Cut` | `(id string, page, pageSizeBytes int) (Cut, error)` | Page bytes directly off disk (no full-buffer load). |
| `(*Store).List` | `() ([]StoreEntry, error)` | All persisted hides, newest first. |
| `(*Store).Delete` | `(id string) error` | Remove a hide and its sidecar metadata. |
| `(*Store).LoadIntoBuffer` | `(id string, pageSize int) (*HideBuffer, error)` | Construct an in-memory buffer pre-loaded with one persisted hide; used when replaying a webhook into a curing. |

## Internal Design

- **ID scheme**: `<sanitized-source>-<sha256[:8]>`. `sanitizeSource` lowercases and strips non-alphanumeric characters.
- **Page sizing**: `pageSize` is in **bytes**, not characters. UTF-8 boundary handling is the caller's concern; `Cut` returns whatever falls inside the byte window.
- **Reflection hints**: when the runner is configured with `ForceTextAfterHide`, the first cut is wrapped with a hint instructing the model to summarize before paging further.
- **flock**: `Store` acquires `flock(LOCK_EX)` on a sentinel file inside the dir for every mutation. Concurrent webhook intake from multiple processes is safe.
- **Permissions**: directory `0700`; files `0600`. Path operations go through `safepath.Anchor`.

## Dependencies

| Package | Why |
|---|---|
| `internal/model` | `ToolDefinition` for the `hide_*` tools. |
| `internal/safepath` | Anchored path validation for `Store`. |

## Test Surface

`internal/hide/hide_test.go` and `internal/hide/store_test.go`:

- Page boundaries: `Cut` produces `TotalPages` consistent with `len(content)`.
- `Search` returns the right page and `false` on miss.
- `Store.Put` then `Store.Get` round-trips bytes byte-for-byte.
- flock contention: parallel `Put` calls succeed without corruption.
- `LoadIntoBuffer` reproduces a stored hide.

## Related Docs

- [docs/modules/runner.md](runner.md) — `HideBuffer` integration and `hide_*` tool gating.
- [docs/modules/curing.md](curing.md) — pipeline that consumes hides.
- [docs/modules/safepath.md](safepath.md)

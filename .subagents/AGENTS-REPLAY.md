# AGENTS-REPLAY.md — leather run-replay subsystem

Subagent guide for the **replay** subsystem: the feature that captures
LLM and tool I/O during a run so operators can re-read, audit, or
selectively re-execute later.

Load this guide when:

- Touching replay capture code (in `internal/runner`, `internal/session`)
- Changing the on-disk replay storage format
- Adding or modifying `/replay/...` HTTP endpoints
- Updating the UI replay views
- Reviewing the redaction policy
- Adding a new "replay action" (e.g. re-run from turn N)

This guide is itself the canonical replay spec. For the security
boundary, see [AGENTS-SECURITY.md § Replay artifact
redaction](AGENTS-SECURITY.md#replay-artifact-redaction). For UI, see
[AGENTS-UI.md](AGENTS-UI.md). For the HTTP API surface generally, see
[AGENTS-SERVE.md](AGENTS-SERVE.md). For runner integration, see
[AGENTS-RUNTIME.md](AGENTS-RUNTIME.md).

---

## Status

Implementation is partial. Only per-run transcript capture and tool-call
I/O capture are wired today, both surfaced through `/runs/{id}` and the
`views/run-detail.js` view. All other replay features
(token-by-token capture, re-run, re-run-from-turn-N, diff, redaction,
export) are tracked in [ROADMAP.md](../ROADMAP.md). The sections below
describe the **target** format and API; mark any new code clearly when
it lands.

---

## Storage format

### Layout

```
~/.leather/.state/replay/
├── runs/
│   ├── 2026-05-19T09-00-00Z__daily-summary__<8-char-id>.jsonl   0600
│   └── …
└── index.json                                                   0600
```

`runs/` directory mode: `0700`. Files: `0600`. (Permission table in
[AGENTS-SECURITY.md § File permission expectations](AGENTS-SECURITY.md#file-permission-expectations).)

### Filename

```
<RFC3339 UTC, : replaced with ->__<agent-name>__<8-char-id>.jsonl
```

`<8-char-id>` is the first 8 hex chars of a SHA-256 over
`{start-time-ns}|{agent-name}|{random128}`. Collisions are virtually
impossible; if detected, suffix `-2`, `-3`, etc.

### File format

One JSON object per line (JSONL). The **first** line is always a
header record:

```json
{"type":"header","schema":1,"run_id":"…","agent":"…","model":"…",
 "started_at":"…","tokens_budget":{"max":…,"reserve":…,"threshold":…},
 "redacted":false}
```

Subsequent records are events. Event kinds:

| `type` | Payload fields |
|---|---|
| `system_prompt` | `content`, `tokens` |
| `user_turn` | `content`, `tokens`, `turn_index` |
| `model_request` | `messages_count`, `tokens_in`, `params` |
| `model_response` | `content`, `tokens_out`, `finish_reason`, `tool_calls` (array) |
| `tool_call` | `id`, `name`, `args`, `start_at` |
| `tool_result` | `id`, `name`, `content`, `truncated`, `error`, `duration_ms` |
| `summarize` | `before_tokens`, `after_tokens`, `summary_tokens` |
| `error` | `phase`, `message` |
| `footer` | `ended_at`, `status`, `tokens_total`, `bytes_written` |

The **last** line is always a `footer`. A missing footer means the
run was killed mid-execution; readers must tolerate truncation.

### Schema version

`schema` is monotonic. Bumping requires:

- A migration note in this guide and in
  [ROADMAP.md](../ROADMAP.md).
- Readers backward-compatible to N-1 (or a one-time migration utility
  shipped in the same release).
- Bump documented in [AGENTS-OPERATIONS.md § Upgrade & state-migration
  policy](AGENTS-OPERATIONS.md#upgrade--state-migration-policy).

---

## `/replay/control` action surface

All mutating replay operations go through the **action surface**.
Action endpoints are gated behind authn when API auth lands
([AGENTS-SECURITY.md](AGENTS-SECURITY.md)).

| Method | Path | Action | Notes |
|---|---|---|---|
| `GET` | `/replay/runs` | List run records (paginated). | Read-only. |
| `GET` | `/replay/runs/{id}` | Fetch one run as JSONL or JSON. | Honors `Accept:`. |
| `POST` | `/replay/run/{id}` | Re-execute a stored run end-to-end. | Body: `{ "redact": true }`. |
| `POST` | `/replay/run/{id}/from/{turn}` | Re-run starting at turn N. | Same body shape. |
| `GET` | `/replay/diff` | Diff two runs side-by-side. | Query: `a=`, `b=`. |
| `GET` | `/replay/export/{id}` | Stream a zip of a redacted run record. | **Always** `--redact`; refuses otherwise. |
| `DELETE` | `/replay/runs/{id}` | Delete a run record. | Hard delete; no undo. |

### Response shape conventions

- `200` on success.
- `404` for unknown run ID.
- `409` if the run is currently writing (cannot re-run live).
- `422` on shape/validation errors.
- Action endpoints return `{ "result": "...", "id": "..." }` on
  success; the new run id is included.

---

## Redaction policy

Operator-facing policy in [AGENTS-SECURITY.md § Replay artifact
redaction](AGENTS-SECURITY.md#replay-artifact-redaction).
Implementation-side invariants:

- Redaction is a **read-time and export-time** transform; on-disk
  records are kept verbatim so that operators can recover misclassified
  secrets without re-running.
- The redactor matches resolved `model.SecretRef` values byte-exactly
  in `content` fields of `tool_result`, `model_response`,
  `system_prompt`, and `user_turn`. Matches are replaced with
  `[REDACTED:<ref-name>]`.
- Export is **redaction-mandatory**. A request without `redact=true`
  returns `422`.
- The UI renders a persistent banner whenever `redacted: true` was
  applied to the read response.
- Future: structured field-level redaction (e.g. by JSON path) when
  use cases demand it.

---

## Runner integration

`internal/runner` emits replay events through a narrow interface so
the runner doesn't carry storage concerns:

```go
type ReplaySink interface {
    Header(model.RunRecord) error
    Event(event ReplayEvent) error
    Footer(model.RunRecord) error
    Close() error
}
```

Implementations:

| Type | Location | Use |
|---|---|---|
| `FileSink` | `internal/runner/replay_file.go` | Production; writes JSONL to `.state/replay/runs/`. |
| `NoopSink` | `internal/runner/replay_noop.go` | When `--replay=off`. |
| `MemorySink` | tests | Capture events for assertions. |

The runner never inspects events after emission. Capture failures are
**logged-and-continued**: a failed replay write does not abort the
run.

---

## UI surface

Owned jointly with [AGENTS-UI.md](AGENTS-UI.md). Replay-specific views:

| View | File | Reads | Writes |
|---|---|---|---|
| Run detail | `ui/views/run-detail.js` | `/replay/runs/{id}` | `/replay/run/{id}`, `/replay/run/{id}/from/{turn}`, `/replay/export/{id}`, `/replay/runs/{id}` (delete) |
| Run diff | `ui/views/run-diff.js` (planned) | `/replay/diff` | n/a |

UI-side rules:

- "Branch from here" requires confirmation.
- "Delete" requires double-confirmation (typed run ID).
- "Export" surfaces the mandatory `--redact` toggle as a fixed on
  switch with explanation; cannot be turned off.

---

## Playbook: add a new replay action

1. **Spec it** — add a row to the action-surface table above and the
   feature × matrix at the top of this file.
2. **Define event shape** — if it introduces a new event type, bump
   `schema` and document the migration in [ROADMAP.md](../ROADMAP.md).
3. **Implement on the runner side** — emit through `ReplaySink`.
4. **Implement the HTTP endpoint** — see [AGENTS-SERVE.md](AGENTS-SERVE.md)
   for endpoint conventions; gate behind auth if mutating.
5. **Wire the UI** — `api.js` wrapper, then view consumers
   ([AGENTS-UI.md](AGENTS-UI.md)).
6. **Test** — `MemorySink` for runner unit tests, file fixtures for
   reader tests, smoke test through the UI.
7. **Document** — this file + the AGENTS.md routing line if scope shifts.

---

## Common mistakes

| Mistake | Correct approach |
|---|---|
| Reading the redacted form for re-run | Re-run uses the **verbatim** on-disk record; redaction is only for read/export. |
| Bumping `schema` without a migration note | Schema bumps require a migration entry in this guide. |
| Letting a replay write failure abort the run | Log and continue; runs are first-class, replay is observability. |
| Exporting without `--redact` | Refuse with `422`; never let an un-redacted export leave the host. |
| Mixing storage code into the runner | Use the `ReplaySink` interface boundary. |
| Adding a new event type without updating the event table | Schema bump + table update + reader compat note in the same PR. |

---

## Verification checklist

Before opening a PR that affects replay:

- [ ] Feature × matrix at the top of this file updated to reflect new state
- [ ] If `schema` bumped: migration entry added to this guide; readers
      handle N-1
- [ ] `MemorySink` test exercises any new event type
- [ ] HTTP endpoint added to the action-surface table; auth review
      noted for mutating endpoints
- [ ] Redaction transform covers any new content-bearing event field
- [ ] UI confirmation rules respected for destructive actions
- [ ] No on-disk file written with mode broader than 0600

---

_Last reviewed: 2026-05-19_

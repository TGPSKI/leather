# AGENTS-UI.md — leather web UI

Subagent guide for the **`ui/` SPA**: file map, design tokens, API
contract layer, state policy, accessibility, live-update mechanism,
mobile + theme posture, smoke-test playbook, and the procedure for
adding a new view.

Load this guide when:

- Editing anything under `ui/`
- Changing an HTTP API endpoint the UI consumes (coordinate with
  [AGENTS-SERVE.md](AGENTS-SERVE.md))
- Adding a new view or panel
- Updating design tokens or theming
- Reviewing accessibility / mobile behavior

For the backend API surface, see [AGENTS-SERVE.md](AGENTS-SERVE.md).
For replay-specific views, see [AGENTS-REPLAY.md](AGENTS-REPLAY.md).
For the security model around the API the UI consumes, see
[AGENTS-SECURITY.md § HTTP API authn/authz model](AGENTS-SECURITY.md#http-api-authnauthz-model).

---

## Scope

| In scope | Out of scope |
|---|---|
| All files under `ui/` | API implementation in `internal/cli` (HTTP) — that lives in [AGENTS-SERVE.md](AGENTS-SERVE.md). |
| Vanilla-JS, zero-build SPA discipline | Any framework adoption (React, Vue, Svelte). leather UI is intentionally framework-free. |
| Design tokens & theme | A visual design system overhaul (would require its own RFC). |

leather's UI is loaded directly from `file://` against a live
`leather serve --api` endpoint. No build step. No bundler. No package
manager.

---

## File map

```
ui/
├── index.html             # single entry; loads all modules
├── styles.css             # design tokens + layout
├── app.js                 # bootstraps modules, owns the top-level router
├── api.js                 # the only file that talks to /jobs, /runs, etc.
├── state.js               # shared in-memory state container + subscribers
├── views/
│   ├── dashboard.js       # /jobs + /runs summary
│   ├── job-detail.js      # single-job drilldown
│   ├── run-detail.js      # single-run drilldown (lifts into replay; see AGENTS-REPLAY)
│   ├── config.js          # /config readout
│   └── settings.js        # client-side toggles (theme, refresh interval)
└── components/
    ├── status-pill.js     # shared JobStatus indicator
    ├── tokens-bar.js      # token-usage visualisation
    └── …
```

> If a file does not yet exist in the workspace, treat the table as
> the **target layout**. Adding a view that doesn't fit the table is a
> review-time discussion.

### Hard rules

- **All HTTP calls go through `api.js`.** Views call `state.js`; state
  calls `api.js`. No `fetch()` outside `api.js`.
- **No transpilation.** Modern ES modules + dynamic `import()` only.
  If a feature requires a transpile step, it's out of scope.
- **No npm dependencies.** Inline copy with attribution if a tiny
  helper is unavoidable.

---

## Design tokens

CSS custom properties in `:root` (light) and `[data-theme="dark"]`
(dark). Keep the surface small:

| Token | Light | Dark | Used for |
|---|---|---|---|
| `--bg` | `#fafafa` | `#0e1116` | Page background. |
| `--surface` | `#fff` | `#181c22` | Cards, panels. |
| `--text` | `#1a1a1a` | `#e6e8ea` | Primary text. |
| `--text-muted` | `#666` | `#9aa4b2` | Secondary text. |
| `--border` | `#e5e7eb` | `#262c34` | Dividers. |
| `--accent` | `#0a66c2` | `#4f9bff` | Links, focus rings. |
| `--ok` | `#1a7f37` | `#4ade80` | Success status. |
| `--warn` | `#9a6700` | `#facc15` | Warning status. |
| `--err` | `#cf222e` | `#fb7185` | Error status. |
| `--mono` | `ui-monospace, …` | (same) | Code / IDs. |
| `--radius` | `6px` | (same) | Cards, pills. |
| `--space-1` … `--space-4` | `4/8/16/24px` | (same) | Padding / margin scale. |

Touching a token affects every view — coordinate via a PR description
mentioning each impacted view.

---

## API contract layer (`api.js`)

`api.js` is the single boundary between the UI and the backend. Every
endpoint gets a typed-by-convention wrapper:

```js
// api.js
const BASE = window.LEATHER_API_BASE || "http://127.0.0.1:7749";

export async function listJobs() { return getJSON("/jobs"); }
export async function getJob(name) { return getJSON(`/jobs/${encodeURIComponent(name)}`); }
export async function listRuns(query = {}) { return getJSON("/runs", query); }
export async function getRun(id) { return getJSON(`/runs/${encodeURIComponent(id)}`); }
export async function getConfig() { return getJSON("/config"); }
export async function getMetrics() { return getText("/metrics"); }
// …
```

### Rules

- One exported function per endpoint. Function name matches the
  conceptual operation, not the HTTP verb.
- All responses validated for shape **at the boundary**. Bad shapes
  surface as a typed error, not a TypeError downstream.
- All errors normalised into `{ kind: "network" | "http" | "shape",
  status?, message, raw? }`.
- No retries here. Retries live in `state.js` policies if needed.

When [AGENTS-SERVE.md](AGENTS-SERVE.md) adds a new endpoint, this file
gets the wrapper in the same PR.

---

## Empty / error / loading state policy

Every async-driven view exposes **all four** states:

| State | Render |
|---|---|
| `loading` | Skeleton or spinner with `aria-busy="true"`. No layout shift on resolve. |
| `empty` | Friendly empty-state message with the next-action affordance ("Schedule an agent → `leather` CLI"). |
| `error` | Inline error card with the normalised message and a `Retry` button. Never a silent failure. |
| `ready` | The data view. |

No "we'll add error handling later" — every new view ships all four
states from day one.

---

## Accessibility

Baseline:

- All interactive elements reachable via keyboard (`Tab`, `Enter`,
  `Space`). Visible focus ring uses `--accent`.
- All icons paired with `aria-label` or visible text.
- Color **never** the sole carrier of meaning (status pills include a
  glyph + text, not just color).
- Contrast ratio ≥ 4.5:1 for text (verify with axe / Lighthouse on
  light + dark themes).
- Live regions (`aria-live="polite"`) for the auto-refresh notice.
- Tables use `<th scope="…">`; sortable headers announce sort state.

---

## Live-update mechanism

v1 uses **client-side polling**. SSE / WebSockets are explicitly out
of scope until the API gains an event surface.

### Polling rules

- Default interval: **5 s** per active view; configurable in
  `settings.js`.
- Pause polling when `document.visibilityState !== "visible"`.
- Back off on consecutive `error` results: 5 s → 10 s → 30 s → 60 s
  (cap). Reset on first success.
- Cancel pending fetches on view change via `AbortController`.

State updates flow through `state.js`; views subscribe via a
publish/subscribe surface and re-render on diff.

---

## Mobile posture

The UI is **responsive** but not mobile-first. Targets:

- Usable on a 360 px-wide phone in portrait.
- Tables collapse into stacked cards below `640 px`.
- Touch targets ≥ 44 × 44 px.
- No horizontal scroll on the dashboard or job-detail views.

The replay UI ([AGENTS-REPLAY.md](AGENTS-REPLAY.md)) is desktop-first;
mobile is "best effort".

---

## Theme

- Default theme follows `prefers-color-scheme`.
- `settings.js` exposes an explicit `light / dark / system` toggle
  persisted to `localStorage` under `leather.theme`.
- `<html data-theme="…">` switches the variable set; no per-element
  overrides.

---

## Smoke test

A minimum check before merging any UI PR:

```bash
# Terminal 1
leather serve --api

# Terminal 2 (browser)
xdg-open ui/index.html   # or `open ui/index.html` on macOS
```

Walk through:

1. Dashboard loads, shows jobs.
2. Click a job → job-detail loads, shows recent runs.
3. Click a run → run-detail loads, shows transcript + tool calls.
4. Toggle theme → no visual regression.
5. Stop `leather serve` → dashboard shows the `error` state with a
   `Retry` button; restart `leather serve` → `Retry` succeeds.

Document the smoke test in the PR description.

---

## Playbook: add a new view

1. **Confirm the API exists.** If not, land the endpoint in
   [AGENTS-SERVE.md](AGENTS-SERVE.md) first.
2. **Add the wrapper in `api.js`**, including shape validation.
3. **Add `views/<name>.js`** implementing `loading`/`empty`/`error`/`ready`.
4. **Add routing** in `app.js`.
5. **Add nav entry** with the matching design-token colors.
6. **Walk the smoke test** end-to-end.
7. **Verify accessibility** (keyboard + screen-reader rotor).
8. **Update this file's file-map table** in the same PR.

---

## Common mistakes

| Mistake | Correct approach |
|---|---|
| Calling `fetch()` directly from a view | Always go through `api.js`. |
| Adding a build step "just for this one library" | Inline the helper or skip the feature. |
| Silent failure on API error (logging only) | Render the `error` state with a `Retry`. |
| Encoding meaning in color only | Pair color with a glyph and text. |
| Polling continuously while the tab is hidden | Honour `visibilitychange`. |
| Hard-coding API base URL | Use `window.LEATHER_API_BASE` override; default to loopback. |
| Adding npm / package.json | Out of scope. |

---

## Verification checklist

Before opening a PR that affects `ui/`:

- [ ] Smoke test walked end-to-end against a live `leather serve --api`
- [ ] All four states (`loading`/`empty`/`error`/`ready`) implemented
      for any new async view
- [ ] Keyboard-only navigation reaches every interactive element
- [ ] Contrast verified on light and dark themes
- [ ] Mobile (360 px portrait) does not introduce horizontal scroll on
      dashboard or job-detail
- [ ] No new fetch outside `api.js`
- [ ] No new dependency, no build step
- [ ] [AGENTS-SERVE.md](AGENTS-SERVE.md) endpoint table consistent
      with any new wrapper added

---

_Last reviewed: 2026-05-19_

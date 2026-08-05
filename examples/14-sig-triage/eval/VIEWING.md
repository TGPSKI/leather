# Viewing results — the watcher and the browser

Two programs read the same archives under `eval/results/runs/`. Neither one
runs a model, writes an archive, or touches anything outside this repo.

| | you want | run |
|---|---|---|
| **watcher** | a live monitor while a battery runs | `cd eval && make watch` |
| **browser** | to interrogate finished results | `cd eval && make matrix` |

Everything that *spends* GPU-hours lives in a separate makefile and takes an
explicit flag (`make -f Makefile.run help`), so reaching for a viewer is
never one typo away from launching a battery.

---

## Quick start

```bash
cd examples/14-sig-triage/eval

make watch                              # live, all rigs, all campaigns
RIG=4b SCOPE=confirmatory make watch    # just the registered battery on the 4B
make matrix                             # interactive browser, opens on the picker
make matrix FILTER=4b-T2\*              # browser, pre-filtered
make table                              # one-shot snapshot, pipe-friendly
make confirm                            # registered contrasts under Holm
```

`NOCOLOR=1` strips ANSI from any of them for piping or pasting.

---

## Scoping: three composable knobs

All of `RIG`, `SCOPE` and `FILTER` work on the watcher and `make table`; the
browser adds an interactive picker that does the same job.

| knob | values | means |
|---|---|---|
| `RIG` | `4b`, `35b` | one rig; also drops the other rig's whole section |
| `SCOPE` | `confirmatory`, `exploratory`, `all` | registered `-cN` draws vs the earlier atlas |
| `FILTER` | a pattern | see the grammar below |

**Filter grammar** — deliberately forgiving:

```
4b            bare prefix
4b-G          longer prefix
4b-*-c1       glob
T2c           substring, case-insensitive
4b-G,4b-E2    comma = OR
!35b          leading ! negates the whole expression
```

Scoping matters more than it sounds: `results/runs/` now holds four
campaigns (exploratory atlas, noise repeats, the registered confirmatory
battery, and the boundary bump). Watching all of them at once is what made
the screen unreadable in the first place.

---

## The watcher (`watch-matrix.sh`)

A live monitor. Ranked, not in run order — the question this screen answers
is "which arm is winning", and run order buries that.

Layout, top to bottom: the running battery and the in-flight cell's progress
(the only thing that changes second to second, so it goes where the eye
lands first), a blank line, the results table, then reference material and
the totals.

### Keys

| key | does |
|---|---|
| `a` `t` `K` `d` `n` `c` | sort by accuracy / tools / ktok / duration / no-out / cell |
| `r` | reverse the current sort |
| `?` | toggle the column glossary |
| `j` `k` / `↑` `↓` | scroll a line |
| `PgUp` `PgDn` | scroll a page |
| `g` `G` | top / bottom |
| `q` | quit |

The sorted column is marked in the header with a direction arrow (`dur▼`).
A scrollbar appears in the last column only when content overflows, with an
explicit `1-18/49` readout — "there is more" is stated, not implied.

Sorting and scrolling are also available as env vars (`SORT=dur`,
`SORT_REV=1`, `HELP=1`) if you want a one-shot table in a particular order.

### Colors

- **accuracy** — red `<74`, yellow `74–84`, green `≥84`. Fixed thresholds.
- **duration** — blue → cyan → magenta, **ranked within the current view**,
  not against absolute cutoffs: what counts as slow depends entirely on
  which arms are on screen (P2 runs 9 minutes, T2cr runs 54). Deliberately
  a different palette from accuracy so a slow cell never reads as a bad one.

---

## The browser (`matrix-tui.py`)

Interactive. `[tab]` cycles four views; the header always names the active
view and says what it does.

### `cells`
Every archived draw, ranked. Adds the analysis columns the watcher
deliberately drops: the arm's mean over its draws, this draw's deviation
from that mean, and the arm's spread. `[space]` opens a **detail card** for
the selected row — what was under test and *why* (the arm's variable and
full description from `arms.json`), stages/turns/catalog/rules, the result
with its mechanism flags, every draw of that arm, the complete
run-manifest provenance, and the archive's files. `[space]` or `[esc]` closes.

### `rankings`
Arm families ranked by mean accuracy as a bar chart, plus a list with each
arm's spread and per-draw accuracies. The chart is **baselined** — a
zero-based axis draws 60% and 78% as identical full columns — and the
baseline is stated in the title.

### `pairs`
McNemar exact on discordant issues for every contrast declared in
`arms.json`, each draw plus the pooled test, with RESOLVED / in-band flags.
It imports `paired-verdicts.py`'s implementation rather than reimplementing
it, so the browser cannot drift from the registered analysis path.

### `cost`
Accuracy against tokens, with the **Pareto frontier** marked (`★` = nothing
else is both cheaper and more accurate) and the frontier listed underneath.
This is where the campaign's most practical finding is visible: cost and
accuracy are near-orthogonal here, because the expensive arms buy turns and
stages — exactly what hurts this model.

### Keys

| key | does |
|---|---|
| `tab` | next view |
| `p` | open the picker |
| `f` / `F` | set / clear the text filter |
| `s` | cycle sort (accuracy, tag, spread) |
| `space` | detail card (in `cells`) |
| `r` | reload from disk |
| `↑↓` `PgUp` `PgDn` `g` | move / scroll |
| `q` | quit |

The browser reloads itself when a new cell lands, so it can be left open
during a battery.

### The picker

Opens on startup when you don't pass a pattern, and on `[p]` any time. It
lists every value of **rig**, **battery**, **arm** and **draw** with cell
counts. `[space]` ticks, `[enter]` applies, `[a]` ticks a whole group, `[n]`
clears one group, `[N]` clears everything.

Values **within** a group OR together; groups AND with each other; and a
group with nothing ticked is unrestricted. So the default state shows
everything and ticking only ever narrows.

---

## Reading the numbers

The in-app glossary (`[?]` in the watcher) defines every column. Four are
worth calling out because they can mislead:

- **`no-out`** — rows that produced no usable answer. For most arms this is
  noise. For `S1` it *is* the mechanism: a fresh-session stage boundary
  makes ~11% of rows unanswerable, reproducibly (25 / 26 / 28 / 30 / 34
  across five draws). Its accuracy loss and its row loss are one phenomenon
  measured two ways.
- **`arm mean`** (browser only) — grouped over *every* archived draw of an
  arm. On the 4B that can blend pre-registration draws with the registered
  `-cN` ones, giving a number that is not any registered quantity. Mixed
  families are marked `*`; the registered figures come from `make confirm`.
- **`battery`** — read from the manifest. Archives written before that field
  existed show an inferred value prefixed `~`, so a guess is never mistaken
  for a record.
- **`ended` / `dur`** — the end time comes from the last timestamp *inside*
  `run-evidence.log.gz`, not the file mtime: any git checkout or stash
  rewrites mtimes, which once had every archive reporting the same end time
  and 10-hour durations. Wall-clock is also rig-shared at concurrency 4, so
  `ktok` and `calls/iss` are the defensible cost axes; duration is the
  readable proxy.

---

## How it fits together

```
matrixdata.py      loads archives, scores via sigeval, one filter grammar
  ├── table.py       rows for the watcher and `make table`
  ├── matrix-tui.py  the four browser views
  └── tui/           vendored curses primitives (stdlib only, from
                     https://github.com/TGPSKI/pane — re-vendor, don't edit)
watch-matrix.sh    the live frame: progress, keys, scrolling
```

Both surfaces go through `matrixdata.py`, so they cannot disagree about a
number. That matters more than it sounds: two unreconciled views of the same
cell are exactly what hid a data-destroying bug for three hours on
2026-07-30 — the watcher said 63.2 and the archive said 60.4, and only
comparing them surfaced it.

`sigeval` is the only scorer. Nothing here derives correctness itself; the
loader regenerates `sigeval-rows.jsonl` when it is stale and fails closed
rather than falling back to a second opinion.

## Porting to another eval

Four things are specific to sig-triage, listed at the top of
`matrixdata.py`: the sigeval bridge, the `<rig>-<arm>-<draw>` tag grammar,
`arms.json` for contrast metadata, and the archive shape (already
leather-standard, so it travels as-is). Everything else — facets, families,
contrasts, Pareto, durations, filtering, the whole TUI — is generic over
"cells with an accuracy and a cost".

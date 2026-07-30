# 14 — sig-triage

Assign a Kubernetes SIG to issues that have none, with a small local model.

This example exists to answer the question leather itself was built for: **can a
runtime make a model that fits on a 6 GB laptop GPU worth using for real work?**

On this task, the answer is measured, not asserted. The same frozen 4B model
(Qwen3-4B-Instruct, AWQ) scores anywhere from **59.6% to 81.6%** on a 250-issue
gold corpus depending only on what the runtime puts around it — the weights
never change, the hardware never changes. Twenty-two points of accuracy live
in the runtime design.

And it is not one lucky sweep: six contrasts were **pre-registered at a commit
hash before any confirmatory cell ran**, replicated three times each, and all
six survive Holm–Bonferroni at α=0.05. Two of them shrank by roughly half under
replication — that correction is left visible, because it is the reason the
protocol exists.

The eval under `eval/` is the instrument — 94 archived cells across two model
scales, paired per-issue verdicts, a measured noise floor, and two quarantined
wrecks with post-mortems. The numbers are one click each: the
[registration](eval/ablation/preregistration.md), the
[confirmatory verdicts](eval/results/CONFIRMATORY.md), the arm-by-arm
[results matrix](eval/results/MATRIX.md), and the exploratory
[verdicts](eval/results/VERDICTS.md). Start at
[eval/README.md](eval/README.md); to browse the archives interactively, see
[eval/VIEWING.md](eval/VIEWING.md).

The arc, in one paragraph: this example started as a demo pipeline, grew an
eval to find out whether the demo was any good, and the eval then earned its
keep twice over. First it caught its **own** contamination (a shared state dir
leaking artifacts between runs) — the affected figures were frozen, the harness
fixed, and the full 22-arm × 2-rig matrix re-run under manifests and
verification. Then the re-baselined campaign produced the findings above — and
two of them were runtime defects that shipped as fixes in leather v0.5.0
(per-turn `clear: true`, recoverable out-of-scope tool refusals). The eval is
not decoration on the example; it is where most of what this example teaches
came from.

## What the small model actually needs

Every claim below is a paired comparison on identical issues, not a leaderboard
delta. Effects of 9+ points resolve decisively; the ~6-point delivery effects
are credible but provisional (many comparisons, one corpus); details and
p-values in `eval/ablation/arms.json` and `eval/scripts/paired-verdicts.py`.

What helped the 4B:

Figures below are the **replicated** ones — three registered draws per side,
pooled, Holm-adjusted. Where a single exploratory draw said something larger,
the replicated number is the one quoted.

- **Explicit domain rules** in the prompt — **+12.8**, the largest single lever.
- **Task before reference** — issue first, catalog after, **+6.5**. Let the
  model understand the question before the payload lands.
- **Rich reference payloads** — full catalog entries beat narrowed bare labels
  by **+3.0**. Shrinking the candidate list is not enough; the model needs the
  prose that explains *why* a label fits. This is the weakest confirmed lever
  and the one that triggered the registered 5× replication rule — the first
  measurement said +6.4.

What hurt it:

- **An extra accumulating turn** — a three-turn flow lost **5.2** to the
  two-turn flow. Context grows monotonically across turns; more calls bought
  noise, not reasoning. (The single exploratory draw said 9.2. Replication
  halved it.)
- **Aggressive context removal** — a fresh-session queue hop lost **16.3**;
  clearing the conversation and carrying only a distilled shortlist lost
  **11.6**. Both mechanisms replaced rich evidence with a lossy summary, and
  the control arm since separated the two costs: clearing itself is worth
  about 5 points, and replacing the rich carrier with a distilled shortlist
  costs another ~7. The carrier is the bigger half.
- **The floor is a bad harness, not the bare model.** The fresh-session scheme
  lands at 61.3% (5 draws) — statistically level with the bare model's 61.9%
  (4 draws) — after paying for an extra stage and 250 tool calls it got
  nothing for.
- **Code-enforced candidate pruning** — no detectable accuracy gain at either
  scale. Enforcement guarantees behaviour; it did not improve this classifier.

The shape of the result: the 4B is harmed both by accumulating too much state
and by carrying too little. The engineering problem is **minimum sufficient
state** — drop irrelevant history, keep the semantic evidence, keep the path
short — not maximal boundedness.

The 35B rig is the reference instrument, not the point: the same workflows at
the larger scale show which failures are 4B-specific (order sensitivity,
forced-tool fragility) and which transfer (turn accumulation hurts both).

One finding was a runtime defect, found because the tool-happy 4B probed a
boundary the 35B never touched: on a turn declared `tools: []` the model kept
calling a tool it remembered from the system prompt. The executor correctly
refused every call — but the refusal was run-fatal, so one recoverable model
mistake dead-lettered the whole work item, 214 times out of 250. Fixed: the
runner now refuses out-of-scope calls with a tool-result error the model can
recover from, still never executing them.
(`eval/results/quarantine/4b-T2c-scope-leak-wreck/QUARANTINE.md`.)

## The pipeline

Three agents, one job each; the write stage is dry by default.

```
analyze-in → [analyze] → match-in → [match] → label-in → [label]
```

- **analyze** — SIG-agnostic. Extracts components/symptoms/keywords from the
  issue. No tools.
- **match** — loads `sigs.reference.yaml` via `get_sig_reference` and picks one
  SIG (or `unknown`). The only stage that knows the taxonomy.
- **label** — assigns the SIG via `apply_sig`. The only stage with side effects.

## Dry vs live (mirrors examples 09–12)

`apply_sig` is gated on `LEATHER_DEMO_MODE` (default `dry`): it prints the action
it would take. Set `LEATHER_DEMO_MODE=live` to actually call `gh`.

`SIG_ACTION` selects the last-step action:

| SIG_ACTION        | Effect (live)                                             |
| ----------------- | --------------------------------------------------------- |
| `comment` (default) | `gh issue comment` posting `/sig <name>` (upstream-safe) |
| `label`           | `gh issue edit --add-label sig/<name>` (needs triage rights) |
| `both`            | comment then label                                        |

## Run the demo (dry)

```
make 14                 # from examples/, dry mode
make 14-live            # LEATHER_DEMO_MODE=live (needs gh auth + a repo you own)
SIG_ACTION=label make 14
```

Working inside this directory instead? `make help` lists the local targets —
`demo`, `live`, `eval`, `preflight`, `battery RIG=<35b|4b>`, `table`,
`verdicts`, `watch` — same surface, no `cd ..` required.

Or directly:

```
cat sample/issue.json | ../../leather workflow run \
  --config config.yaml --tannery tannery.yaml \
  --curing analyze --queue analyze-in --kind github.issues --source cli
```

Stage artifacts land in `.state/artifacts/{analyze,match,label}/`.

## Batch over real unsigged issues

```
./scripts/fetch-unsigged.sh                 # REPO= LABEL= LIMIT= to tune
leather serve --config config.yaml --tannery tannery.yaml --run-duration 300s
```

## Live mode notes

Direct `--add-label` needs triage rights on the target repo — fine on a repo you
own or a mirror. On upstream kubernetes/kubernetes use `SIG_ACTION=comment`; the
`/sig <name>` prow command is the sanctioned path. Update the SIG names from
`sigs.yaml` with `scripts/gen-taxonomy.sh`; the `features:` lists in
`sigs.reference.yaml` are curated by hand.

## The eval

Everything in "what the small model actually needs" is reproducible from
[eval/](eval/): [`run-eval.sh`](eval/run-eval.sh) runs one cell,
[`scripts/run-battery.sh`](eval/scripts/run-battery.sh) runs the remaining
matrix, [`scripts/preflight.sh`](eval/scripts/preflight.sh) proves the harness
works on 5 issues before you spend GPU-hours on it, and
[`scripts/paired-verdicts.py`](eval/scripts/paired-verdicts.py) prints every
declared comparison with its verdict — RESOLVED, unresolved, or CONFOUND — from
archives and manifests, never from runner logs. The arm registry
([`eval/ablation/arms.json`](eval/ablation/arms.json)) documents every variant
and the one variable it isolates. Start with [eval/README.md](eval/README.md).

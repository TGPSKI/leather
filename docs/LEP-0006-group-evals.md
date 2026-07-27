# LEP-0006 — Group Evals

- **Status:** Proposed
- **Target:** leather v0.6.0
- **Supersedes:** ad-hoc per-example eval scripts (e.g. `examples/14-sig-triage/eval/`)
- **Anchors:** the 100-issue SIG eval harness (blind/gold split, scrubbing, accept-sets, deterministic gate) and the example-11 "measured at scale" profile (orchestration ~free, GPU-bound, 6 GB card)

---

## 0. TL;DR

Group evals make evaluation a first-class leather subsystem. A declarative
**eval group** runs one or more **suites** — each a (pipeline × labeled corpus ×
scorer × gate) — on your local model, deterministically, cheaply, and it **fails
closed**. It generalizes the SIG-triage harness and inherits example-11's cost
shape: the orchestration costs a few percent of one host's CPU, the run is
GPU-bound end to end, and it fits a 6 GB laptop card. The point is not a
benchmark leaderboard; it is to make quality **continuously demonstrable and
gate-able** so that bounded-context, small-model pipelines can update themselves,
run unattended, and be trusted on a number instead of a vibe.

---

## 1. Motivation

**The measurement gap.** leather's design says: don't trust a model to *know* a
mutable external fact set — make it *read* one (the SIG catalog, a schema, a
policy) and *match*. That relocates the risk from opaque weights to legible
infrastructure, but it introduces a new obligation: you must *measure* whether
the read-and-match is right, per class, including where it should abstain.
Today that measurement is bespoke — a one-off script per example, no shared
vocabulary, no group semantics, no regression tracking, no standard gate.

**Why now.** Three things in the stack now depend on eval being a primitive:

1. **Self-updating catalogs.** The catalog-currency cron regenerates a catalog on
   upstream drift; a new entry can only merge if an eval gate clears. Without a
   first-class gate, the loop can't be autonomous.
2. **Deterministic operation.** leather already gates docs (doclint) and config
   (schema validation) by diffing against ground truth and failing closed. Eval
   is the same primitive with a tolerance band instead of equality — the missing
   member of that family.
3. **The portability thesis.** Bounded-context, small-model pipelines are only
   credible if their quality is *shown*, cheaply, on the hardware the audience
   actually has. A per-class precision table produced on a 6 GB card is the
   receipt. Eval is how leather manufactures receipts.

**Non-motivation.** This is not an ML experiment tracker, a training loop, or a
hosted leaderboard. It is a gate.

---

## 2. Design principles (the design language, restated)

Group evals are a direct expression of leather's design language. Each principle
below is load-bearing in the API that follows.

- **Constraints enable.** 6 GB of VRAM, a ~4B model, and no API budget are not
  limits worked around — they force the architecture (bounded per-stage contexts,
  one model call per stage, read-don't-know, cheap local eval) that makes the
  system portable and legible. Group evals assume this envelope and are cheap
  *because* the pipelines they measure are.
- **Orchestration is free; the model is the cost.** From example-11: wall time
  tracks model throughput, not leather. Eval inherits this exactly — a group eval
  is N inputs × a small number of prefill-heavy calls, and the harness around it
  rounds to zero. You scale eval by trimming prompts or feeding a bigger GPU, not
  by a bigger orchestrator.
- **Deterministic, fail-closed gates.** Every leather gate is a diff against
  ground truth: equality where the artifact is discrete (doclint tokens, catalog
  SHA), a **tolerance band** where it is statistical (eval precision/recall). A
  gate never green-lights on trust; it green-lights on a number, and it fails
  closed.
- **Read, don't know — then measure the reading.** Evals localize failure to the
  layer you own. Low precision on a class is usually a *catalog / features*
  problem you fix deterministically in a reference file, not a reason to reach
  for weights you can't afford. The report is designed to make that distinction
  visible (precision vs recall per class).
- **Abstention is a first-class answer.** The correct response to irreducible
  ambiguity is `unknown` / low confidence, not confabulation. The scorer rewards
  well-placed abstention instead of punishing it.
- **Orthogonal to context stuffing.** Group evals are how you *demonstrate* that
  bounded beats broad for a given task — and how you decide, per stage, whether
  widening a bound is *measured* to help. They turn "bounded vs long context"
  from a slogan into a per-suite number.

---

## 3. Concepts and vocabulary

Mirrors leather's existing nouns (hide, curing, tannery, agent, skill, artifact).

| Term | Definition | Analogue |
|---|---|---|
| **Eval group** | A declarative set of suites sharing config; the unit you run and gate. | tannery (a bundle of routes/queues) |
| **Suite** | One (pipeline / curing-chain) × (corpus) × (scorer) × (gate). Bounded, one job. | curing |
| **Corpus** | Blind inputs as hides (`corpus.jsonl`) + a separately-held answer key (`gold.jsonl`). | a queue of hides + labels |
| **Prediction** | The pipeline's terminal artifact per hide, parsed to `{predicted, confidence}`. | artifact |
| **Scorer** | Pure function `(gold, predictions) -> report`. Classification is built-in; pluggable. | — |
| **Gate** | Thresholds → pass/fail → exit code. Equality or tolerance band. | doclint / schema validate |
| **Eval run** | A reproducible execution: pinned model, seeded + content-hashed corpus, cached inputs; emits report + baseline. | workflow run |
| **Baseline** | A stored prior report for regression diffing. | — |

Group evals **reuse** the runtime wholesale: hides, curings (in eval-mode:
terminal at the stage under test, side-effect stages disabled), queues,
`workflow run` / `serve`, artifacts, `persist_runs`, and the profile harness.
Nothing new is invented where an existing primitive fits.

---

## 4. Architecture

### 4.1 Evals are leather flows in eval-mode

A suite compiles to a normal leather flow with three constraints:

1. **Blind ingest.** Only `corpus.jsonl` (inputs, no labels) is ingested as
   hides. The answer key never enters the model's context.
2. **Terminal at the scored stage.** The curing chain runs up to and including
   the stage under test (for SIG-triage: `analyze → match`, terminating at
   `match`). Downstream side-effect stages (`label`) are structurally absent from
   the eval tannery — not merely dry-run.
3. **Hard side-effect lock.** Eval-mode sets an irrevocable guard equivalent to
   `LEATHER_DEMO_MODE=dry` that *cannot* be lifted for the duration of the run.
   An eval never has live tools.

```
corpus.jsonl (blind) ─▶ ingest ─▶ [analyze] ─▶ q ─▶ [match] ─(terminal)─▶ artifacts/
                                                                              │
gold.jsonl (held) ──────────────────────────────────────────┐               │
                                                             ▼               ▼
                                                        scorer ◀── predictions.jsonl
                                                             │
                                                             ▼
                                                        report + gate (exit 0/1)
```

### 4.2 The runner

`leather eval run <group>` orchestrates the whole loop:

- builds or loads corpora (cached, content-hashed);
- fans out over hides using the tannery's queues and concurrency (sharding is
  just queue concurrency — the same mechanism example-11 stress-tested);
- drains to quiescence; isolates per-hide state for clean attribution;
- parses terminal artifacts into predictions;
- scores and gates.

Determinism is a first-class requirement (§7). Ordering is stable; the run is
reproducible from `{corpus hash, model id, seed}`.

### 4.3 New artifacts

- `report.json` — machine-readable metrics (per-class P/R/F1/support/abstain,
  macro + weighted, confusions, gate verdict).
- `report.txt` — the human classification report.
- `baseline.json` — the accepted prior, for regression diffs.

---

## 5. Corpus subsystem

Generalizes the SIG fetcher into a reusable, hygienic corpus builder.

### 5.1 Builders (pluggable sources)

- `local` — a hand-authored or curated JSONL.
- `github-issues` — class-balanced fetch via the GitHub search API: one query per
  class, dedup by id, cache raw responses, split blind/gold. Multi-label items
  become accept-sets.
- `query` — an arbitrary command/HTTP source emitting `{id, input, labels[]}`.

Balance is explicit: the builder fetches per class so the head doesn't drown the
long tail. Support counts are reported so the reader can weight the metrics.

### 5.2 Leakage guard (first-class, deterministic)

Real inputs leak their own answer — in the SIG corpus, ~46% of raw issues carried
a `/sig <name>` prow command or a `sig/<name>` mention. A blind corpus that
contains the label is not blind. So the builder **must** declare
label-revealing patterns, and:

- redact them from blind inputs (`[label-redacted]`), and
- `leather eval lint` **fails closed** if any gold token remains recoverable from
  its blind input.

This is the doclint pattern applied to eval hygiene: a deterministic diff (is the
answer present in the question?) that gates the corpus before it is ever used.
Leakage is a build-time error, not a silently-inflated score.

### 5.3 Accept-sets and abstention

- **Multi-label → accept-set.** An input with several valid labels accepts any of
  them; optionally `unknown`. This encodes real ambiguity (an EndpointSlice bug
  that is genuinely network-or-node) rather than punishing the model for it.
- **Abstention is scored.** `unknown` / low confidence is a first-class
  prediction. Where the gold permits abstention (genuinely ambiguous rows), a
  well-placed `unknown` scores as correct; where a concrete answer is expected,
  it is tracked as an abstention (neither correct nor a hard confusion). A model
  that knows when *not* to answer must be able to score well.

### 5.4 Provenance and caching

Raw sources are cached and hashed. A corpus is content-addressed: an eval run
pins `{corpus_hash, builder_version}` so results are reproducible and a corpus
change is visible in the diff. Re-running a build is free unless `REFRESH=1`.

---

## 6. Configuration schema

Mirrors leather's YAML conventions (`additionalProperties: false`,
schema-validated by `leather validate`; a new `schemas/evalgroup-1.schema.yaml`
and `schemas/evalsuite-1.schema.yaml` are added and held to defs.go parity).

### 6.1 Group

```yaml
# evals/sig-triage.group.yaml
name: sig-triage
description: SIG classification quality gate for the analyze->match pipeline.
model: "{{env:LEATHER_MODEL}}"          # pinned per run; recorded in the report
endpoint: "{{env:LEATHER_LLM_ENDPOINT}}"
seed: 42                                 # determinism for any synthetic step
suites:
  - sig-triage-current
  # matrix sweeps expand here (see 7.3): flows x models x catalogs
report_dir: evals/.reports
```

### 6.2 Suite

```yaml
# evals/suites/sig-triage-current.suite.yaml
name: sig-triage-current
config: config.yaml                      # reuse the pipeline's own config
tannery: eval/tannery.yaml               # analyze -> match (terminal); no side effects
entry:
  curing: analyze
  queue: analyze-in
  kind: github.issues
scored_stage: match                      # artifact parsed for the prediction
parse:                                    # how to extract {predicted, confidence}
  predicted: '^SIG:\s*([a-z-]+)'
  confidence: '^CONFIDENCE:\s*([a-z]+)'
corpus:
  builder: github-issues
  repo: kubernetes/kubernetes
  classes: [network, node, storage, scheduling, apps, api-machinery, auth, cli, autoscaling, instrumentation]
  per_class: 15
  target: 100
  body_cap: 4000
  leakage_patterns:                       # redacted from blind inputs; linted
    - '/(remove-)?sig\s+[a-z-]+'
    - '\bsig[-/][a-z-]+'
    - '\bSIG\s+(network|node|storage|...)\b'
scorer: classification                    # built-in; accept-set + abstention aware
gate:
  min_accuracy: 0.80
  max_abstain: 0.20
  core: [network, node, storage, scheduling, apps, api-machinery]
  min_core_recall: 0.80
  max_core_regression: 0.05               # vs baseline
```

### 6.3 Gate semantics

- Discrete conditions (coverage: no missing predictions) → equality.
- Statistical conditions (accuracy, per-class recall, abstention) → tolerance
  band, expressed as floors/ceilings.
- Regression conditions → bounded delta against `baseline.json`.
- Any failed condition → non-zero exit. Fail closed.

---

## 7. Execution and determinism

### 7.1 Commands

| Command | Purpose |
|---|---|
| `leather eval run <group>` | build/load corpora, run pipelines, score, gate |
| `leather eval lint <group>` | schema + **leakage** check; no model needed; fails closed |
| `leather eval report <group>` | render the last report (human/JSON) |
| `leather eval baseline <group> --accept` | promote the current report to baseline |

### 7.2 Determinism

- **Pinned model + decoding.** `temperature: 0`, `thinking: false`; model id
  recorded in the report. Runs are comparable only against the same model id.
- **Content-hashed corpus.** The corpus is pinned by hash; a corpus change shows
  up as a report input change.
- **Seeded synthetic paths.** Any synthetic step (e.g. a stand-in for CI
  plumbing) is seeded.
- **Fixture / replay mode.** Building on the offline-LLM-fixture work: a suite may
  run against a recorded response fixture so **CI can exercise the eval *plumbing*
  with no model**, while real-model runs produce the accuracy receipts. This
  separates "does the harness run" (cheap, every commit) from "is the model
  accurate" (scheduled, on real hardware).

### 7.3 Group / matrix semantics

A group can sweep a matrix and tabulate:

- **flows** (pipeline variants — e.g. with/without catalog pre-filtering),
- **models** (4B vs 7B; awq vs nvfp4),
- **catalogs** (candidate `sigs.reference.yaml` vs current),
- **corpora** (by date window, by difficulty).

Each cell is an independent suite run sharing the corpus cache; results form a
matrix report. Sharding across the corpus is queue concurrency — the exact
mechanism example-11 profiled at 100-webhook burst. Evals are just another
high-fan-out leather workload.

---

## 8. Metrics and reporting

### 8.1 Built-in classification scorer

Accept-set and abstention aware. Emits:

- overall accuracy, accuracy on *answered* (excl. abstentions), abstention rate;
- per-class **precision / recall / F1 / support / abstain**;
- **macro** and **weighted-F1** averages;
- top confusions (gold → predicted).

**Reading guidance (baked into the report footer):** precision and recall are
read together. High precision + low recall on a class = the model is *cautious*
there (good; pair with abstention). Low precision = the model *over-assigns* that
class — a catalog/features overlap you fix deterministically in the reference
file, not by swapping weights.

### 8.2 Pluggable scorers

Classification is the first scorer; the interface admits others without touching
the runner:

- `extraction` (token/field F1 against gold spans),
- `ranking` (top-k / MRR),
- `exact-match` / `regex-match`,
- `judged` (LLM-as-scorer; see open questions — determinism caveats apply).

### 8.3 Regression

`report.json` diffs against `baseline.json`. The gate can require no core-class
regression beyond a delta. This is how a catalog PR is judged: not "is it good in
the abstract" but "is it *not worse* than the accepted baseline, and does it clear
the floors."

---

## 9. Gating and CI

- **PR gate.** Any change to a catalog (`sigs.reference.yaml`), a pipeline agent,
  or a suite config triggers `leather eval run` as a required check. Fail closed.
- **Currency cron → gate.** `check-taxonomy-currency.sh` detects upstream drift →
  regenerate names + author features for new classes → open PR → the PR gate runs
  the eval → merge only on PASS. The human touches it once, at merge, which is the
  correct trust boundary for a new label.
- **Plumbing vs accuracy split.** CI runs `eval lint` (leakage/schema, no model)
  and fixture-mode `eval run` (plumbing) on every commit; real-model `eval run`
  runs on a schedule on the eval box and posts the receipt.

This completes the deterministic-gate family:

| Gate | Ground truth | Match |
|---|---|---|
| doclint | registered flags/env/routes | equality |
| schema validate | defs.go field set | equality |
| catalog currency | upstream SHA | equality |
| **group eval** | **labeled corpus** | **tolerance band** |

---

## 10. Measured at scale (expected v0.6.0 profile)

Mirrors example-11's "measured at scale" section. These are **projections
anchored to the measured 100-webhook profile**, not yet-measured eval numbers;
they state the shape v0.6.0 is expected to hold to on the reference rig.

**Reference rig:** single RTX PRO 4500 (32 GB) *or* RTX PRO 500 (6 GB laptop),
local vLLM serving Qwen3.6-4B-Instruct (AWQ/NVFP4), `thinking: false`.

A group eval of the SIG suite is `N` inputs × 2 model-touching stages
(`analyze` + `match`, the latter one tool round), prefill-heavy, terminal-artifact
-light — the same class of workload as the 500-LLM-call webhook burst that cost
6.5% avg host CPU and 0% PSI.

| Metric | Expected shape |
|---|---|
| Inputs | 100 blind issues |
| Model calls | ~200–300 (analyze + match, match with 1 tool round) |
| Host CPU (leather + queueing) | low single-digit % — orchestration adds ~nothing |
| Host pressure (PSI) | ~0% — no stalls; artifact writes negligible |
| Bound | **GPU-bound**; wall time ≈ N × per-issue model latency |
| Fit | resident on 6 GB (weights ~2.5–4 GB @ 4-bit, tiny KV at <4 K contexts) |

**Conclusion (same as example-11):** eval wall time scales with model throughput,
not with leather. To run larger suites, trim prompts (this workload is
classification — short in, terse out) or feed a bigger GPU — never a bigger
orchestrator. The harness is free; that is precisely what makes running a full
precision gate on **every** catalog change, on a laptop, unattended, viable.
Against a metered API this economics inverts and the autonomy is lost.

---

## 11. Non-goals and open questions

**Non-goals.** Not an experiment tracker; not a training/fine-tuning loop; not a
hosted leaderboard; not a statistical-significance engine for large corpora.

**Open questions.**

- **Judged scoring determinism.** An LLM-as-scorer reintroduces the
  non-determinism eval exists to remove. Constrain to rubric + fixed decoding, or
  keep judged suites out of the merge gate.
- **Gold curation & human-in-the-loop.** New-class features and disputed labels
  need a human. Where does that sit relative to the automated gate? (Proposal:
  human at PR merge only.)
- **Small-corpus variance.** At N≈100 per class counts are small; per-class
  recall is noisy. Report confidence intervals? Require minimum support before a
  per-class gate applies?
- **Prompt-set drift.** If the corpus is rebuilt from a moving upstream, the gate
  compares across shifting inputs. Pin corpora by hash and re-baseline explicitly
  on rebuild.

---

## 12. Rollout (v0.6.0)

- **Phase 1 — core.** Eval group/suite config + schema (+ defs.go parity),
  classification scorer, gate, `leather eval run` / `eval lint`. Migrate the
  existing SIG harness onto it as the reference (`examples/14-sig-triage/eval`
  becomes the first first-class group).
- **Phase 2 — corpora.** `github-issues` builder, the leakage guard as a
  fail-closed lint, caching/provenance, accept-sets/abstention.
- **Phase 3 — group & regression.** Matrix sweeps, baselines/regression diffs,
  fixture/replay CI mode, pluggable scorers.

**Compatibility.** Additive. Existing examples keep working; the SIG eval scripts
are re-expressed as a group and kept as a thin wrapper for one release.

---

## Appendix A — worked reference (the SIG group)

The v0.5-era harness (`examples/14-sig-triage/eval/`) already implements the
primitives this LEP promotes to first-class: `fetch-eval-corpus.sh`
(class-balanced GitHub fetch, cache, blind/gold split, leakage scrub),
`run-eval.sh` (blind corpus → analyze→match → predictions), `sigeval.go`
(accept-set/abstention-aware classification report + tolerance gate), a gate
self-test, and the currency-cron trigger. v0.6.0 replaces the shell glue with
`leather eval run` and the config schema above; the semantics are unchanged.

## Appendix B — glossary delta

`eval group`, `suite`, `corpus (blind/gold)`, `leakage guard`, `accept-set`,
`abstention`, `scorer`, `gate (tolerance band)`, `baseline`, `fixture/replay` —
added to `docs/GLOSSARY.md`. Each is defined against an existing runtime noun to
keep the vocabulary small.

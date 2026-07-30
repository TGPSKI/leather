# Eval-Driven Iteration (methodology companion to LEP-0006)

- **Status:** Methodology document — formerly LEP-0007, retired as a numbered
  proposal 2026-07-28. Nothing here is an API: it is the procedure for reading a
  red gate, proven by hand twice (the 35B validation loop below, and the
  2026-07-28 ablation campaign at full scale). Section numbers are preserved
  because code and audit notes cite them (e.g. sigeval.go §5.4/§4.6).
- **Companion to:** [LEP-0006 — Group Evals](LEP-0006-group-evals.md) (the gate primitive)
- **Anchors:** the 14-sig-triage 35B validation loop (64.5% → gated) — a real
  session where a failing gate was walked to a shippable one *without touching
  model weights*, using only scorer, gold-hygiene, catalog, and prompt fixes —
  and, since 2026-07-28, the full ablation campaign, which is this LEP's loop
  run by hand at scale: gold adjudication (accept-set overrides, one suspect
  deliberately left standing), a comparator error caught and re-registered, a
  runtime defect attributed and fixed, and a nearly-20-point range (62.4→81.6%)
  on one frozen 4B demonstrating that the layers this LEP exhausts before
  touching weights are where nearly all of the value lives.

---

## 0. TL;DR

LEP-0006 makes quality **measurable and gate-able** — it answers *is the gate
green?* and fails closed when it is not. It does not tell you **why** it is red or
**what to change**. LEP-0007 adds the missing half: a deterministic,
regression-guarded **improvement loop** that (1) **attributes** every failure to
the layer that owns it, (2) applies the **cheapest correct fix at that layer** —
exhausting free/deterministic layers before ever reaching for weights — and (3)
**re-measures on a cheap hard slice, guards against per-item regression, and
confirms on the full corpus with the dev/held-out split reported side by side**.
It turns a red gate from a verdict into a
prioritized, non-overfitting worklist. Same cost shape as 0006: the loop is
harness work; the model is the only real cost, and most of the highest-value
fixes touch no model at all.

---

## 1. Motivation

**A gate is a verdict, not a plan.** LEP-0006 gives you `GATE FAILED — overall
accuracy 64.5% (threshold ≥80%)` and a confusion table. That is necessary and not
sufficient: a red gate is only useful if there is a *repeatable, cheap, honest*
procedure for turning it green. Today that procedure is tribal knowledge — the
person who wrote the harness knows to read the confusion table, notice that a
third of the "errors" are a notation mismatch, spot the junk gold rows, and fix
the catalog before blaming the model. None of that is encoded, so every example
re-derives it and most stop at "the model isn't good enough."

**The failure we want to prevent is the wrong fix.** Faced with a red gate the
tempting move is to reach for a bigger model or a longer context — the two levers
leather's thesis says you *cannot afford* and *should not need*. In the anchor
session, of the 35.5-point accuracy gap:

- **~15 points were the harness mis-measuring** (the model emitted the label form
  `sig/network`; the catalog name is `sig-network`) — a scorer bug, not a model
  error;
- **~5 points were junk gold** ("Created by mistake" issues the model correctly
  abstained on) — a corpus-hygiene bug;
- the remainder were **genuine confusions** fixable in the **catalog and prompt**
  (storage-vs-node, auth-surfacing-through-kubelet) — the layer you own.

Not one point required a different model. This method is the discipline that makes
that the *default* path, and makes the cheap deterministic fixes come *first*.

**Why now.** LEP-0006 promotes eval to a first-class gate wired into CI and the
catalog-currency cron. A gate that can only say "no" strands the operator. For the
self-updating-catalog loop to be autonomous, the *response* to a red gate must be
as legible and bounded as the gate itself.

**Non-motivation.** Not an autotuner, not a prompt-search / RL loop, not a
"make-it-pass" script. It is a **decision procedure for humans and agents** that
keeps fixes at the cheapest honest layer and refuses to overfit the eval.

---

## 2. Design principles (extending LEP-0006 §2)

Inherits all of LEP-0006's principles. Adds five that are load-bearing here:

- **Fix the layer you own.** A failing gate is a worklist over layers *you
  control*: scorer parsing → gold hygiene → catalog features → prompt/curing
  boundaries → decoding (`thinking`, `temperature`) → and only then model size.
  You descend that ladder and stop at the first layer that explains the failure.
  Weights are the last resort, not the first reflex.
- **Deterministic before probabilistic.** A large share of "model failures" are
  the harness mis-measuring: notation/parse artifacts, unclassifiable gold. These
  are fixed in *code* (scorer, gold lint) — free, permanent, and they never
  regress. Exhaust them before you spend a single model call on tuning.
- **Guard every step against regression.** A prompt edit that lifts class A
  routinely, silently, breaks class B. Every iteration is scored **per item**
  against the prior run — *N fixed, M regressed* — not just on the aggregate,
  which can rise while quietly rotting a core class.
- **Iterate cheap, accept honest.** Tune against a **hard slice** (failures +
  regression guards) so a round costs seconds, but **never accept on the slice** —
  the slice is where you overfit. Acceptance is always a full-corpus confirmation
  that reports the **dev slice and the held-out remainder separately**, and the
  number of tuning rounds is **capped** and logged. The rails are what make the
  number honest, not the word "held": a slice drawn from the same corpus is a
  *tuning* set, and calling it held-out when it is not is the failure mode this
  bullet exists to prevent.
- **Gold is code.** The answer key is a maintained artifact with its own lints
  (leakage *and* sanity), not ground truth handed down. Correcting an obviously
  unclassifiable label is a *fix*, not cheating — but it must be a **general,
  documented rule**, never a hand-picked list of ids that happen to be wrong.

---

## 3. Concepts and vocabulary (delta over LEP-0006 §3)

| Term | Definition |
|---|---|
| **Failure class** | The bucket a miss falls into: `parse-artifact`, `gold-noise`, `over-assign` (low precision), `confusion-pair`, `abstention-gap`, `genuine`. Drives which layer fixes it. |
| **Attribution** | The scorer step that tags each incorrect prediction with a failure class + a suggested fix layer. |
| **Fix ladder** | The ordered layers a fix may live in, cheapest/most-deterministic first (§5). |
| **Hard slice** | A small, failure-weighted subset of the corpus (last run's misses + a stratified set of correct "regression guards"), used for fast iteration. |
| **Flip diff** | Per-item comparison of two runs: `fixed` (was wrong, now right), `regressed` (was right, now wrong), `unchanged`. The unit of iteration feedback. |
| **Split manifest** | A committed `{number, tier}` file naming which rows tuning was allowed to see (`dev`) and which it was not (`holdout`). Membership is stable and auditable across re-fetches. |
| **Anti-overfitting rails** | The three things that actually keep an iterated number honest: **principled-rules-only**, a **round cap**, and **per-item regression guards** — plus the dev/held-out split reported side by side. Named for what they are, not for a holdout the loop cannot enforce on a small corpus. |
| **Gold-sanity guard** | A fail-closed corpus lint: an input with no recoverable signal must have gold `unknown` (or accept `unknown`), not a concrete label. |

---

## 4. The loop

```
        ┌─────────────────────────────────────────────────────────────┐
        │                                                             │
   full run ──▶ attribute ──▶ per-class worklist ──▶ pick lowest ladder rung
   (gate)         (§5)                                   that explains it
        ▲                                                     │
        │                                                     ▼
   confirm on full  ◀── regression-guard ◀── re-run HARD SLICE ◀── apply fix
   (+ dev/held-out      (flip diff)          (seconds, not full)     at that layer
    split reported)
        │
        ▼
   accept → promote baseline (LEP-0006 §8.3)
```

One turn of the loop:

1. **Measure.** `leather eval run` → report + gate (LEP-0006). If green, stop.
2. **Attribute.** `leather eval attribute` tags each miss with a failure class and
   the ladder rung that owns it, and rolls them up per class (§5, §6).
3. **Pick the cheapest rung.** For each dominant failure class, take the
   lowest/most-deterministic rung on the fix ladder that explains it (§5).
4. **Fix at that layer.** Scorer normalization, a gold-lint rule, catalog
   features, a prompt/curing boundary, or a decoding flag — in that order of
   preference.
5. **Re-run the hard slice.** `leather eval run --slice` over failures + guards.
   Seconds, because the slice is ~½ the size and `thinking: false`.
6. **Regression-guard.** `leather eval diff` shows *fixed / regressed* per item.
   A net gain that regresses a core class is **rejected**, not shipped.
7. **Confirm on full.** When the slice looks good, run the full corpus and read
   the **3-way split** — full / dev-slice / held-out. Held-out tracking full is
   the evidence the fix generalized; dev far above held-out means it memorized,
   and the change is rejected. Cap: ≤ K tuning rounds before a mandatory full
   confirmation (default K=3).
8. **Accept.** Promote the report to baseline (LEP-0006 §8.3).

The loop is *stateless across examples*: it reads only the report, the gold, and
the artifacts LEP-0006 already produces.

---

## 5. Failure attribution and the fix ladder

The heart of this LEP. Attribution answers *which layer owns this miss*; the
ladder says *fix it there, and prefer the cheapest rung*.

### 5.1 Failure classes → fix ladder rung

| Failure class | Signature in the report | Rung (fix here) | Determinism |
|---|---|---|---|
| **parse-artifact** | predicted value is a valid answer in the *wrong notation/format* (`sig/x` vs `sig-x`, casing, whitespace, extra prose) | **Scorer** — normalize on load; or tighten the `parse:` regex | deterministic, free |
| **gold-noise** | input has no recoverable signal but gold demands a concrete label; model abstains and is marked wrong | **Gold lint** — sanity guard relabels/accepts `unknown` by a general rule (§5.3) | deterministic, free |
| **over-assign** | one class has **low precision** — it is predicted for inputs that belong elsewhere | **Catalog** — the class's `features` are too broad; narrow them in the reference file | deterministic, free |
| **confusion-pair** | a stable A→B off-diagonal (e.g. storage→node) | **Prompt / catalog boundary** — a principled ownership rule ("volumes are storage even via kubelet") | cheap model runs |
| **abstention-gap** | high `unknown` where a concrete answer exists | **Accept-sets / confidence** — or a prompt nudge to commit when a class clearly fits | cheap model runs |
| **genuine** | none of the above; the model is simply wrong on a well-posed input | **Decoding → model** — `thinking`, `temperature`, then (last) a bigger model | expensive |

The ladder is strict: **you may not attribute a miss to `genuine` (and reach for
weights) until the deterministic rungs above it have been ruled out.** In the
anchor session, doing this in order converted a "the 35B isn't good enough"
conclusion into four cheap fixes.

### 5.2 Deterministic-first: the two free rungs

`parse-artifact` and `gold-noise` are *harness* bugs. They are fixed once, in
code, and never regress. They must be swept **before** any model-touching tuning,
because otherwise every prompt experiment is measured through a noisy scorer and
you tune against ghosts. (Anchor: normalization alone was +15 points; it would
have been invisible under a broken scorer.)

### 5.3 Gold-sanity guard (fold into LEP-0006 §5, sibling of the leakage guard)

LEP-0006 §5.2 already lints for *answer leakage* (is the answer in the question?).
Its mirror image is *answer absence* (does the question contain enough to answer
at all?). Add a fail-closed **gold-sanity** lint:

- An input the builder judges **content-free** (a length floor, or a
  builder-declared junk pattern) **must** have gold `sig: unknown` (or an
  `accept` that includes `unknown`). Otherwise a well-calibrated abstention is
  scored as a miss, and the junk drags a core class's recall denominator below
  what perfect classification could reach.
- The rule is a **declared, general predicate** (e.g. `min_body_chars: 60`), not a
  hand-maintained id list — reproducible, auditable, and stable as the corpus is
  rebuilt. Anchor: junk clustered at ≤19 chars; the next real issue was 427, so a
  60-char floor isolated exactly the five "Created by mistake" rows with no false
  positives.
- `leather eval gold-lint` reports every relabel and **fails closed** if a
  content-free input still carries a concrete gold label. Gold hygiene is a
  build-time gate, exactly like leakage.

### 5.4 Anti-overfitting rails

Because iteration edits prompts/catalogs against observed failures, it can overfit
the eval. The rails below are the whole defence — deliberately **not** called
"holdout discipline," because on a small corpus a genuine holdout is too thin to
carry a threshold and pretending otherwise is the dishonesty this section guards
against. Rails:

- **Principled fixes only.** A fix must be a general rule ("volumes → storage
  wherever they surface"), never "issue #139535 → storage." Reviewers reject
  id-specific catalog/prompt edits.
- **Hard slice ≠ acceptance set.** Tune on the slice; accept on the full corpus.
  The slice carries **regression guards** (known-correct rows) so a fix that
  trades them away is caught immediately.
- **Report the split, always.** Acceptance prints full / dev-slice / held-out
  from a **committed split manifest** (§6). The manifest is the auditable record
  of what tuning was allowed to see; the held-out column is the generalization
  check. This is a *reported* rail, not an enforced one: at N≈93 a held-out set
  of ~49 rows has a ±5-point standard error on accuracy, enough to detect gross
  memorization and not enough to gate on. Growing the corpus is what upgrades it
  from evidence to gate.
- **Round cap.** ≤ K prompt/catalog rounds per full confirmation (default 3),
  logged in the report. Chasing the last few points on a small corpus is a
  variance-mining smell (LEP-0006 §11 small-corpus variance).
- **Baseline regression bound.** LEP-0006 §8.3's `max_core_regression` is the
  final backstop: no accepted change may regress a core class beyond the delta.
- **Measure the null before trusting any verdict.** Before a comparison can
  reject anything, run the *unchanged* configuration twice and diff it against
  itself. Whatever that moves is the floor: any smaller effect is unresolvable,
  whatever the report says. Skipping this is how a loop starts optimising noise.
- **Do not gate on per-class PASS/FAIL at small support.** This was tried and it
  is unsound. Two natural formulations both fail:
  - *"Reject any change that regresses a core class"* rejects the **best** class
    of fix. An over-predicting class's false positives ARE other classes' false
    negatives, so correcting its boundary hands rows back and its own recall
    necessarily dips while several others rise.
  - *"Trade down failing classes, never passing ones"* survives that objection
    and still fails, because a class crossing its floor is not a reliable event
    at this support. Measured on 14-sig-triage: re-running an **unchanged**
    prompt moved net −6 rows and pushed `sig-node` 92% → 83%, across the 90%
    floor — the identical signal that had just been used to reject a real change.
    At n=24 one row is 4 points; two rows cross a floor. **A reject rule that
    fires on identical inputs is not a rule.**

  So: **judge on the aggregate + macro-recall against the measured null**, and
  keep per-class strictly as a *diagnostic* for reading the mechanism. The
  per-class story is what makes an accepted change explicable — it is evidence,
  not a verdict.

  Worked instance (14-sig-triage, 250 rows): tightening `sig-api-machinery`'s
  ownership boundary moved its precision 58% → 74%, with `sig-node` 75% → 92%
  and `sig-apps`/`sig-auth` 84% → 92%, and 8 of 11 fixes were api-machinery
  handing a row back to its true owner. Net was only +4 rows — *inside* the ±6
  null band — so the aggregate alone does not carry it. The change is defensible
  because the precision shift is large and the per-row mechanism is the
  hypothesised one, and because replication (below) put the mean above baseline.

- **Replicate what you publish, not what you iterate.** Single runs are fine for
  deciding whether to keep exploring a direction; they are not fine for a number
  that ships. Any figure quoted as a result — a headline accuracy, a comparison
  table, an ablation — is run **3×** and reported as mean ± spread. Reporting the
  best of several draws as "the" result is the same error as letting a singleton
  class inflate a macro average, and it is easy to commit accidentally when the
  runs happen days apart.

---

## 6. Reporting additions (fold into LEP-0006 §8)

`report.json` gains, per incorrect prediction and rolled up per class:

- `failure_class` ∈ {parse-artifact, gold-noise, over-assign, confusion-pair,
  abstention-gap, genuine};
- `suggested_rung` (the ladder layer to fix it at);
- `normalized` (whether notation folding changed the compared value — surfaces
  parse-artifact volume at a glance).

`report.txt` gains a **worklist footer**: failure classes ranked by count, each
with its rung and the affected classes — the operator's to-do list, cheapest rung
first. This is the natural extension of LEP-0006 §8.1's precision/recall reading
guidance from *interpretation* to *action*.

### 6.1 The 3-way split (required)

Every acceptance report **must** carry the split, read from a committed
`splits.jsonl` manifest of `{number, tier}` (`dev` = tuning was allowed to see
it; anything else is held out):

```
split (dev = tuned on; held-out = never tuned on):
  full:       87.1% (81/93)
  dev-slice:  86.4% (38/44)
  held-out:   87.8% (43/49)   <- generalization check
```

Reading it: **held-out ≈ full** means the rules generalized. **dev ≫ held-out**
means they memorized the slice — reject the change regardless of what the
aggregate did. The manifest is committed rather than derived at runtime so
membership is stable across corpus re-fetches and a reviewer can check what was
tuned on, and gold relabels ride in a sibling `gold.overrides.jsonl` overlay so
the raw answer key stays re-fetchable (§9).

---

## 7. Commands (extending LEP-0006 §7.1)

| Command | Purpose |
|---|---|
| `leather eval attribute <group>` | tag the last run's misses with failure class + rung; print the ranked worklist |
| `leather eval slice <group> [--from-last] [--guards N]` | materialize a hard slice from the last run's failures + N stratified regression guards |
| `leather eval run <group> --slice` | run only the current hard slice (fast iteration) |
| `leather eval diff <group> <runA> <runB>` | per-item flip diff: fixed / regressed / unchanged |
| `leather eval gold-lint <group>` | leakage **and** sanity checks; emits the relabels to `gold.overrides.jsonl` by rule (never edits raw gold); fails closed |
| `leather eval split <group>` | show/verify the committed `splits.jsonl` manifest and the per-tier row counts |

All are harness-only except `run --slice` (which is a small model job). `attribute`,
`slice`, `diff`, and `gold-lint` need **no model** — they operate on artifacts,
gold, and reports LEP-0006 already emits.

---

## 8. Worked reference — the 14-sig-triage 35B loop

The session this LEP generalizes, on a single box serving Qwen3-35B via vLLM,
`analyze → match` terminal, `thinking: false`:

| Step | Rung | Change | Effect (full corpus / hard slice) |
|---|---|---|---|
| 0. Baseline | — | harness fixed, gate run | **64.5%**, GATE FAILED |
| 1. Attribute | — | confusion table → ~⅓ of misses are `sig/x` vs `sig-x` | worklist: parse-artifact dominant |
| 2. Deterministic | Scorer | fold `sig/x → sig-x`, trim/case | **64.5 → 79.6%**, +15 pts, zero model calls |
| 3. Deterministic | Gold lint | relabel 5 content-free rows → `unknown` (body < 60), as a `gold.overrides.jsonl` overlay over pristine gold | api-machinery recall ceiling unblocked; correct abstention now scores correct |
| 4. Confusion-pair | Prompt/catalog | balanced ownership rules (storage-via-kubelet, auth-via-anywhere, HPA→autoscaling); `REASONING:` before `SIG:` under no-think | hard slice **25 → 33/44**; 11 fixed / 3 regressed |
| 5. Confirm | — | full corpus, 3-way split | **64.5% → 87.1%** (81/93); dev **86.4%**, held-out **87.8%** (rules generalize); macro-F1 **87%**; storage recall **29% → 100%**, auth **50% → 90%** |

The pattern: **two free deterministic rungs did the heavy lifting** (+15 from the
scorer, junk unblocked the ceiling); the model prompt closed the genuine
confusions (+~7); no weights changed. The gate initially failed only on the 90%
per-core-SIG recall floor (support 7–13; one miss = −8 to −14 pts) — a threshold
*calibration* question (LEP-0006 §11), **not** a rung on the fix ladder, and
explicitly out of scope for this loop (§9). It was resolved as a calibration
decision, not a rung: a **min-support guard** (no per-class recall floor below
support 20, where the standard error on recall is ~11 points) plus a
**macro-recall** gate as the primary per-class health check. Note the honest
consequence at N=93 — *every* core class falls under the guard, so macro-recall
alone carries per-class health until the corpus grows. That is the argument for
growing it, and it is why the guard reports each skipped class and its support
rather than hiding them. The `REASONING:`-before-
`SIG:` ordering is a reusable `thinking: false` note — with no hidden trace, put
the visible reasoning token *before* the committed answer field so the model
decides before it commits (worth surfacing in LEP-0006 §7.2).

---

## 9. Non-goals and open questions

**Non-goals.** Not automated prompt search / RL / autotuning; not a
statistical-significance engine; not a way to *lower thresholds* to pass (gate
thresholds are a separate, human calibration decision — moving them is not a rung
on the fix ladder).

**Open questions.**

- **Attribution accuracy.** `confusion-pair` vs `genuine` is itself a judgment;
  mis-attribution sends you to the wrong rung. Start rule-based (notation regex,
  off-diagonal stability, precision floor) and treat any `judged` attribution with
  LEP-0006 §11's determinism caveats.
- **Slice construction.** Failure-weighting risks a slice that is unrepresentative;
  guards mitigate but do not eliminate it. How many guards, stratified how?
- ~~**Gold-fix provenance.**~~ **Resolved: yes, an overlay.** Relabels live in a
  separate `gold.overrides.jsonl` (`{number, sig?, accept?, reason}`, applied at
  load, overrides win) so `gold.jsonl` stays byte-identical to the fetcher's
  output and a re-fetch can never clobber or silently drift from them. The
  overlay is *generated by the declared predicate* (§5.3), not hand-maintained,
  so it is reproducible as well as diffable. Mutating the raw answer key in place
  is now a review-rejectable defect.
- **Round-cap enforcement.** Advisory (logged) vs hard (refuse to accept past K)?

---

## 10. Rollout

- **Phase 1 — attribution + gold-sanity.** `eval attribute`, the failure-class
  tags in `report.json`, and the `gold-lint` sanity guard (the two deterministic
  rungs). Ships with or just after LEP-0006 Phase 2; pure harness, no new model
  path.
- **Phase 2 — slice + diff.** `eval slice`, `run --slice`, `eval diff` (flip
  tracking). The cheap iteration loop.
- **Phase 3 — worklist + rails.** Report worklist footer, round-cap logging,
  reviewer guidance on principled-fix-only. Documented as the standard response to
  a red gate.

**Compatibility.** Additive over LEP-0006; introduces no new runtime primitive.
The gold-sanity guard and per-error attribution are extensions to LEP-0006 §5 and
§8 respectively and may be merged there instead of kept here, at the editors'
discretion — this LEP owns the **loop**; those two owe their home to the sections
they extend.

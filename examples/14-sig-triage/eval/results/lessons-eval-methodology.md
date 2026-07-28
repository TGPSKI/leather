# Lessons: eval harness, testing, and methodology

> **Figures provisional.** Every number below predates the harness fixes of
> `d5d8d23`/`eaf377a`/`5eeb03e` and is being re-baselined — see the status note in
> [README.md](README.md). The findings and methodology stand; the specific
> values will be replaced from verified runs.


The part with the most carry-forward value, because almost none of it is specific
to SIG triage. Several entries are corrections to decisions made earlier in this
same project — those are marked, because the sequence is the lesson.

---

## Measure the null before trusting any verdict

The original accept/reject rule was: reject a change if any core class crossed its
recall floor. It rejected two changes on that basis.

Then a **confirmation run of an unchanged prompt** produced net −6 rows and fired
the same signal. The rule had been rejecting noise, and the only reason it was
caught is that the same config was run twice.

Two repeat runs of one config gave net 0 and net −6. That is the null band, and
every verdict is now stated against it: **net beyond ±6 rows is a result; anything
inside is UNRESOLVED.** Not "no change" — *unresolved*, because the experiment
lacked the power to say.

**Do this first, before any tuning.** Run one config twice. The spread is the
smallest effect the setup can detect, and without it every subsequent comparison
is uncalibrated.

## Do not gate on per-class PASS/FAIL at small support

With ~24 rows per class, a single row is 4 percentage points. A class moving
88% → 79% looks alarming and is two rows of noise. Per-class numbers stayed in the
report as a **labelled diagnostic**, annotated with how few rows they represent,
but they no longer accept or reject anything. The verdict is on the aggregate.

The failed intermediate formulation is worth recording: "trade down failing
classes, never passing ones" sounds principled and does not work either, because
floor-crossing is not a reliable event at n=24 — whether a class counts as
"passing" is itself noise.

## Replicate what you publish, not what you iterate

Single runs are fine for steering. They are not fine for a number that goes in a
table.

The headline for one stage was recorded as **88.4%**. Replicated four times, the
same config scored 84.8 / 86.0 / 87.6 / 88.4 — **mean 86.7, spread 3.6 points**.
The published number was the best draw of four, and nothing in the process had
flagged that, because it was never run twice.

Rule: iterate on single runs, **publish means with spread**, and say how many runs
the mean is over.

## Small deltas need the arms replicated, not the baseline

A corollary that bit later: comparing two configs whose difference is ~2 points
requires *both* arms replicated. A −0.1-point difference of means between two 3×
replicated configs (86.4 vs 86.5) is a genuine "no effect" result. The same
comparison from one draw each could have shown +2.8 or −3.6 and been believed.

## Abstention is a different metric with different variance

Accuracy moved 2.4 points and was inside the null. **Abstention moved 2.0% → 6.8%
in the same run**, against a 2.0–2.4% baseline across every prior run. Tight
metrics detect effects that noisy ones cannot. When the aggregate says
UNRESOLVED, check whether a *mechanism* metric moved — it may be the real finding.

## Judge on flip-diff, not just the aggregate

`fixed N / regressed M / unchanged K` with the per-item list is worth more than
the score. Two changes with identical aggregates can be completely different
events, and only the item list shows whether a fix did what it was designed to do
or accidentally cancelled out elsewhere. Stage 2's rule was accepted because the
flip-diff showed the *predicted causal chain* — api-machinery over-predictions
returning to their true SIGs — not because the number went up.

## The prompt is a contended budget

Adding four ownership rules to a prompt that already had one degraded classes the
new rules **never mentioned**: `sig-storage` recall 88% → 50%, and the previous
round's api-machinery precision gain reverted (74% → 54%). Overall 88.4% → 80.8%,
net −19 rows.

The rules were not wrong — one of them produced its predicted effect
reproducibly. The delivery mechanism was saturated. **This is a scaling limit, not
a tuning problem:** each new rule makes the existing ones less reliable, so the
taxonomy cannot grow by adding rules. That is an argument for retrieval, and it
came from the data rather than from the design doc.

## Instrument provenance, or you will measure the harness

Two counters that changed conclusions:

- **"rows with no usable artifact"** — separates "the model was wrong" from "the
  harness lost the answer." A run with a nonzero count is measuring the harness.
- **"tool offered on N/250, called M"** — reading the log for `executing tool`
  cannot distinguish "the model declined" from "the tool was never offered." The
  request body can.

**And a worked example of how a broken counter manufactures a false finding.**
That counter first reported the catalog tool as offered 250/250 and called 0 *on
both model scales*, which became a headline claim. It was wrong for the small
model. The fold kept only the **last** match round per issue — but a tool call
*forces* a second round, and that round carries no `tool_calls`, so the fold
deleted precisely the evidence it was counting. Re-measured after the fix, on the
same prompt and corpus: the 35B calls the catalog **zero** times; the 4B calls it
on essentially every issue.

Two lessons, and the second is the expensive one. First, a counter that can only
under-report still produces confident conclusions. Second — **a conclusion drawn
from a broken instrument does not correct itself when the instrument is fixed.**
The bug was found and patched, and the claim it had produced went on standing in
this document until something else contradicted it. Fixing an instrument means
re-deriving what it told you, not just trusting it from then on.

## Keep the answer key pristine; put fixes in an overlay

`gold.jsonl` had been hand-edited to relabel five bad rows. That destroys
provenance — a re-fetch silently clobbers or drifts from the corrections.

Fix: regenerate `gold.jsonl` byte-identical to the fetcher's output, and put
relabels in `gold.overrides.jsonl` (`{number, sig?, accept?, reason}`, applied at
load). Critically, the overlay is **generated by a declared predicate**
(`MIN_BODY_CHARS=60`), not hand-maintained — so it is reproducible as well as
diffable, and every row carries the reason that produced it.

## Tier the corpus, and gate on the tier you didn't tune on

Three tiers, stratified per class: **smoke** (tuned on, expect the best number),
**acceptance** (the gate of record), **holdout** (never tuned on, the
generalization check). Reporting all three makes overfitting visible as a
divergence rather than invisible as a single good number.

## Macro-averages are inflated by tiny classes

Macro-recall read 89% while the honest figure was 86%, because singleton classes
scored 100% or 0% and counted the same as a 25-row class. Averaging only over
classes with support ≥ 20 fixed it. **Any macro-average over a long-tailed label
set needs a support floor**, and the floor belongs in the report.

## Sample the corpus honestly

Growing 100 → 250 issues, the first attempt over-sampled the ambiguous tail — 52%
of rows from multi-SIG issues against a natural rate of 26%. That inflates
apparent difficulty and makes every subsequent number incomparable to the old
ones. Proportional interleaving fixed it. **Check the distribution of what you
sampled against the population you sampled from**, every time the corpus changes.

## Ablate to answer "how much of this is your code?"

The single most valuable measurement in the project was the cheapest: strip the
prompt to a bare label set and re-run. Nothing else answers the skeptic's
question. A pipeline reporting 86.7% where the bare model gets 70.4% is a
different claim from one where the bare model gets 85%.

The rule for constructing arms: **change exactly one thing, by deletion**.
Variants here were generated by *removing* the rule block from the committed
prompt, with assertions that the removal happened and nothing else did. A
hand-written "simplified" prompt would have measured a prompt rewrite instead.

## Guard the confound you would never see

The two-pass adjudicator presents two candidates. Always showing the first pass's
top pick as candidate 1 would let a tie-breaker score well by simply agreeing with
position 1 — and that failure is invisible in the results. Candidate order is set
by **issue-number parity**: deterministic across re-runs, balanced across
positions. Measured position bias came out 57%, so the guard was doing something.

## Fail-safe merges, with the declines counted

When a second stage can overrule a first, define what happens when it produces
nothing usable. Here: missing, self-inconsistent, off-ballot, and explicit
`neither` verdicts all leave the first answer standing. The second opinion can
improve the result or decline; it cannot destroy it. **And each outcome is counted
separately**, because a stage that "helps" while silently declining 40% of its
cases is not a result.

## Aggregate views dilute targeted interventions

The adjudicator moved the corpus number by −1 row: unresolved, uninteresting. The
same run restricted to the 50 rows it actually touched: pass 1 scored 76% there
against 87.6% corpus-wide (so the margin router concentrated errors ~2×), gold was
on the ballot 46/50 (a 92% ceiling), and the tie-breaker converted that into 74%.

**The router worked; the adjudicator did not** — a conclusion the aggregate could
not express. When an intervention targets a subset, report the subset.

## Cheap failure beats expensive silence

The proxy refuses to start if its port is already serving. That guard cost four
consecutive runs which failed in under a second each — and prevented a repeat of
the earlier incident where a stale proxy served an entire eval while recording to
the wrong file, leaving a silently empty column. **Fail-closed on setup, fail-open
during measurement.**

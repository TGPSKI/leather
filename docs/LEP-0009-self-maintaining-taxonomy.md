# LEP-0009 — The Self-Maintaining Taxonomy

- **Status:** Deferred — measured 2026-07-28; see the post-campaign verdict below
- **Target:** none (was v0.7.0); reactivation trigger defined below
- **Depends on:** LEP-0006 (the eval gate), LEP-0007 (attribution + the anti-overfitting rails)
- **Anchors:** the measured collapse of "read the catalog" as currently built —
  the catalog is offered on 250/250 match requests and called 0 times, and when
  pasted into the prompt outright it buys +2.2 points against the +16.3 the
  hand-written prompt rules buy.

---

## 0. TL;DR

The 14-sig-triage `match` agent is configured to read a versioned catalog rather
than lean on the model's priors — one agent's prompt-and-tool configuration
inside one example, not a statement about leather's architecture. That
configuration does not currently work, and measuring it showed why: the catalog
is prose, delivered whole, which is the same scaling problem as inline rules with
an extra hop. The knowledge that drives accuracy is *boundary* knowledge — where
one SIG stops and the next begins — and today it has nowhere to live but the
prompt.

This LEP closes the loop in three parts. A **queryable index** replaces
whole-file delivery, so a stage retrieves the two or three candidates the issue
names instead of all twenty-two. **`NOT_MATCH` associations** give boundary
knowledge a home in the file rather than the prompt. And an **eval-driven
maintenance loop** proposes index mutations from measured confusion pairs and
ships them only through the LEP-0006 gate — so the taxonomy improves from
evidence rather than from an author's memory.

The danger is stated up front: a loop that edits a lookup table using its own
eval failures is gradient descent on the answer key. §5 is the set of rails that
make it honest, and they are load-bearing, not advisory.

---

## Post-campaign verdict (2026-07-28) — why this LEP is Deferred

The ablation campaign built and measured this LEP's first two parts. The
results, paired per-issue on 250 items at two model scales:

- **The queryable index: vindicated, with a correction.** Narrowed retrieval
  works — but only when it returns the candidates' FULL entries, not bare
  labels (arm G vs E2: **+6.4 resolved** on the 4B). Label-only narrowing, this
  LEP's original sketch, was one of the worst delivery mechanisms measured. The
  index shipped (lookup_sig v2/v3) and earns its place through payload
  richness, not through compression.
- **The NOT_MATCH boundary store: null.** Advisory boundaries were offered on
  57% of issues and cited on 2% (uptake, not coverage, is the constraint).
  Enforcing them in code (v3 pruning) so a boundary *cannot* be ignored: **−0.8
  / +1.2 vs the unenforced lookup, −0.8 / −3.6 as rules+narrowed vs
  rules+bulk — unresolved nulls at both scales.** Boundary knowledge in the
  FILE adds no measurable accuracy while the same knowledge in the PROMPT
  (the hand-written rules) remains the largest single lever (+12.4).
- **Therefore the maintenance loop is unfunded.** A loop that mutates a store
  whose accuracy contribution is measured at zero cannot pay for its own
  complexity or its overfitting risk, however good the rails.

What survives is the motivation, not the mechanism: the inline-rules saturation
ceiling is real and separately measured (adding four rules degraded classes the
new rules never mentioned; ~doubling the instruction block hurt adherence
across the board). The prompt cannot grow forever.

**Reactivation trigger:** the day the rules block needs to grow past what
saturation allows — a new confusion class the current rules cannot absorb, or a
taxonomy meaningfully larger than 22 classes — re-open this LEP, and start from
the measured facts above: full-entry retrieval as the delivery mechanism,
boundaries as *content in the retrieved entries* rather than a parallel
exclusion table, and the mutation loop only after a single hand-authored
boundary change demonstrates a paired, resolved gain.

---

## 1. Motivation

### 1.1 What the measurement said

On kubernetes/kubernetes issues, 250 rows, 22 SIGs, one commit, one model:

| variant | prompt contains | catalog | accuracy |
|---|---|---|---|
| A | inline ownership rules | offered, never called | **86.7%** (4 runs) |
| B | label set only | none | **70.4%** (2 runs) |
| C | label set only | pasted whole into the prompt | **72.6%** (2 runs) |

A − B = **+16.3 points**: the ten hand-written ownership rules. C − B = **+2.2
points**: the entire catalog, delivered in full. Whatever is making this pipeline
work, it is not the catalog.

The gap is explained by what each artifact contains. The catalog lists *positive*
ownership — sig-storage owns volumes, sig-node owns the kubelet. The rules encode
*boundaries*: metrics inside the apiserver are still sig-instrumentation; a bug in
a named resource belongs to that resource's SIG and not to the machinery it
travels through. Boundaries are what a classifier needs at the point where two
plausible answers compete, and they are exactly what the catalog cannot express.

### 1.2 Why whole-file delivery cannot be the answer

Delivering the catalog whole has the same ceiling that made the inline rules fail
to scale. Adding rules for two classes measurably degraded classes the new rules
never mentioned (`sig-storage` recall 88% → 50%); the prompt is a finite,
contended budget. A 22-entry catalog pasted into that budget competes with the
task instructions for the same attention, and a 60-entry one would compete harder.

Retrieval breaks that coupling: prompt size becomes independent of taxonomy size,
so the 30th SIG costs nothing. This is the property that was always claimed for
read-the-catalog and never actually built.

### 1.3 Why the loop, and why now

`scripts/check-taxonomy-currency.sh` already hashes upstream `sigs.yaml` and exits
10 on drift, and its header describes "a wrapping leather cron/agent" that opens a
catalog PR and gates it with the eval before merge. That agent does not exist. The
detector has been shipping a signal into the void — which is also how a regex bug
that silently disabled the whole currency loop went unnoticed until this eval
tripped over it.

Currency is only half of it. Upstream tells you a SIG's *name*; nothing upstream
tells you that issues mentioning `ServiceAccount` inside the apiserver belong to
sig-auth. That knowledge exists in exactly one place: the eval's confusion pairs.

---

## 2. Design principles

- **The file is the knowledge.** If accuracy depends on it, it belongs in a
  versioned, diffable, regenerable artifact — not in a prompt and not in weights.
- **Retrieve, don't deliver.** A stage asks for what its input names.
- **Boundaries are first-class.** `NOT_MATCH` is not an afterthought; it is the
  half of the taxonomy that carries the accuracy.
- **The gate decides.** No mutation ships because an agent proposed it. It ships
  because the eval improved and the holdout held.
- **Generalize or reject.** A mutation keyed to one issue is memorization. Terms
  earn their place by appearing across independent issues.
- **Every mutation is reviewable.** One association per line, so a change is a
  one-line diff and a bad batch can be bisected.

---

## 3. The index

```
term <TAB> MATCH|NOT_MATCH <TAB> sig-name <TAB> provenance
```

```
kubelet                 MATCH       sig-node          catalog
eviction                MATCH       sig-node          catalog
metrics                 NOT_MATCH   sig-api-machinery learned:2026-07-27:n=4
serviceaccount          MATCH       sig-auth          learned:2026-07-27:n=3
```

TSV, not YAML: this file is written by a generator, read by `grep`, and edited by
an agent. One association per line makes every one of those cheap, and makes a
mutation a one-line diff.

**Provenance is mandatory.** `catalog` rows are regenerable from
`sigs.reference.yaml`; `learned:<date>:n=<support>` rows are not, and carry the
evidence count that justified them. `gen-sig-index.sh` regenerates MATCH rows and
**preserves** learned rows, because deleting them discards measured knowledge that
no upstream source can restore.

**Lookup** takes the terms a stage already has (in sig-triage, the analyze note's
`COMPONENTS` and `KEYWORDS`) and returns only associations for those terms, a
MATCH tally, and any NOT_MATCH exclusions. A `NOT_MATCH` hit is an instruction:
that SIG is wrong for this term however plausible it looks.

---

## 4. The maintenance loop

Two triggers, one gate.

**Currency** (exists): upstream drift → regenerate names → propose additions for
SIGs that appeared and retirements for those that vanished.

**Accuracy** (new): a completed eval run → confusion pairs → proposed
associations. A row predicted `sig-api-machinery` with gold `sig-auth`, on an
issue whose components include `ServiceAccount`, proposes `serviceaccount MATCH
sig-auth` and `serviceaccount NOT_MATCH sig-api-machinery`.

```
  eval run ──► confusion pairs ──► candidate mutations ──► §5 rails
                                                             │
                              ┌── rejected (logged, with reason)
                              └── survivors ──► apply to index ──► eval gate
                                                                     │
                                            ┌── holdout regressed ──► revert
                                            └── holdout held ──► PR with evidence
```

The agent never writes the index directly on a green run. It opens a change with
the confusion pairs, the support count, and the before/after gate output attached.
A human merges. Autonomy is in the *proposal*, not the commit.

---

## 5. Rails (load-bearing)

A loop that edits a lookup table using its own eval failures will, unchecked,
encode the corpus into the table and report a rising number the entire way. Each
rail below blocks a specific failure of that kind.

**5.1 Terms, never issues.** A mutation may key only on a term appearing in the
issue's extracted signals. An association keyed to an issue number, title, or any
identifier is memorization with extra steps, and is rejected at proposal time.
This is LEP-0007's "principled general rules only" applied to the index.

**5.2 Support threshold.** A term earns an association only after appearing in
**≥3 distinct issues** with the same verdict. One confusion pair is an anecdote.
The support count is recorded in the row's provenance so a reviewer can weigh it.

**5.3 Propose on acceptance, gate on holdout.** Mutations are derived only from
the acceptance tier. They ship only if the holdout tier — never tuned on, never
proposed from — holds. A mutation that improves acceptance and degrades holdout is
overfitting caught in the act, and is reverted with the pair recorded.

**5.4 Reject inside the null band.** A batch that moves the aggregate less than
the measured null (±6 rows on this corpus) is noise and is not evidence of
anything. This loop generates candidates faster than any human reviews them; with
no band it will accept noise indefinitely and drift.

**5.5 Batch, bisect, and cap.** Mutations apply as a reviewable batch, capped per
run. A regressing batch is bisected — one association per line exists for exactly
this — so one bad row does not discard a good batch.

**5.6 Learned rows are never silently regenerated.** `gen-sig-index.sh` preserves
`learned:` provenance. A regeneration that would drop learned rows fails loudly.

**5.7 The gate threshold is never lowered to accept a mutation.** Inherited
verbatim from LEP-0007 §9, restated because a self-modifying loop has a standing
incentive to relax its own gate.

---

## 6. What this does not do

Not online learning: nothing changes at inference time. Not fine-tuning — the
whole point is knowledge in a file rather than in weights, because weights cannot
be diffed, reviewed, or reverted. Not a general knowledge base: the index is a
term→label association table, and it should stay one. Not autonomous merging: the
loop proposes, a human merges.

**Open questions.**

- **Term extraction quality.** Associations are only as good as the terms the
  analyze stage emits. Garbage terms produce garbage associations at scale, and
  nothing here validates them.
- **Conflicting associations.** When one term carries MATCH for two SIGs, is the
  tally enough, or does the index need weights? Weights are harder to review,
  which is a real cost.
- **Retirement.** Associations that stop earning their keep should decay, but
  removing a learned row loses the evidence that produced it. Time-based decay,
  or re-derivation from a growing corpus?
- **Does retrieval actually recover the rules' 16 points?** Unmeasured. Arm E
  (index + lookup) exists; the prediction on record is that it beats B and still
  trails A until `NOT_MATCH` rows are populated, because the index as generated
  carries positive ownership only. If E closes the gap after a round of learned
  boundaries, the thesis is demonstrated rather than asserted.

---

## 7. Rollout

- **Phase 1 — the index and the lookup.** `gen-sig-index.sh`, `sigs.index.tsv`,
  a `lookup_sig` tool, and the agent variant that queries it. Measurable
  immediately as ablation arm E; no loop, no mutation.
- **Phase 2 — proposal.** Confusion pairs → candidate mutations with the §5 rails
  enforced, emitted as a report and nothing else. Run it for a few cycles and read
  what it wants to do before letting it do anything.
- **Phase 3 — the gated loop.** The currency cron gains an accuracy trigger,
  mutations apply as bisectable batches, PRs carry evidence. A human still merges.

**Compatibility.** Additive. `sigs.reference.yaml` stays the human-curated source
for MATCH rows and the currency check keeps working unchanged; the index is
derived from it, and the learned rows are the part that has no other home.

# eval — full SIG-triage evaluation harness

Fetches real sig-labeled `kubernetes/kubernetes` issues, caches them, **separates
the labels from the issues** (and scrubs label leakage), runs the `analyze`->`match`
pipeline over all of them on your local model, and emits a full classification
report gated on thresholds. Reuses the exact pipeline agents — no `label` stage,
no side effects. Runs entirely on your local endpoint; cheap enough to gate every
catalog change.

## Pipeline

```
scripts/fetch-eval-corpus.sh   GitHub search API -> cache/ (raw) -> split:
                             corpus.jsonl  {number,repo,title,body}   (BLIND, scrubbed)
                             gold.jsonl    {number,sig,accept[]}       (answer key, PRISTINE)
scripts/gold-sanity-lint.sh    corpus + gold -> gold.overrides.jsonl   (rule-based relabels)
scripts/make-splits.sh         corpus + gold -> splits.jsonl           (tier manifest)
run-eval.sh                blind corpus -> analyze->match on your model -> predictions.jsonl
sigeval.go                 gold + overrides + splits + predictions
                             -> classification report + tiered split + PASS/FAIL gate
```

Labels never touch the model: `run-eval.sh` reads only `corpus.jsonl`; scoring
joins `predictions.jsonl` to `gold.jsonl` afterward.

## The ablation campaign (2026-07-28)

Beyond the single gate run, this harness ran a 22-arm ablation matrix on two
model scales (46 archived cells). The moving parts:

- `ablation/arms.json` — every arm: its parameters, the ONE variable it
  isolates, the arm it is read against, and `allow_diff` for manifest keys the
  variable legitimately changes.
- `scripts/preflight.sh` — 26 checks on 5 issues before any GPU-hours.
- `scripts/run-battery.sh <rig>` / `noise-battery.sh` / `overnight-battery.sh`
  — cell runners: per-rig locks, skip-if-complete (>=225 answered rows, not
  just 250 archived), verify-after-archive.
- `scripts/paired-verdicts.py` — McNemar's exact test on the discordant issues
  for every declared pair; manifests diffed per pair, confounds flagged instead
  of narrated. The inference tool of record.
- `scripts/table.py` / `watch-matrix.sh` — archive-derived leaderboard and live
  battery status.
- `results/quarantine/` — wrecked runs kept with post-mortems; do not resurrect.

Headline: the same frozen 4B spans 62.4→81.6% across arms (floor: the S1
fresh-session stage split; ceiling: the F2 single-stage split with rules and
catalog held constant). Findings and their
verdicts are summarized in the example README; per-cell evidence lives in each
archive's manifest, sigeval report, and logprob record.

## 1. Build the corpus

```
PER_SIG=15 bash eval/scripts/fetch-eval-corpus.sh          # unauth: ~10 search reqs
GH_TOKEN=ghp_... PER_SIG=15 bash eval/scripts/fetch-eval-corpus.sh   # higher rate limit
```

The committed corpus is **250 issues**, 24-25 per core SIG.

One search request per SIG (balanced across 10 SIGs), deduped by issue number.
Cached in `cache/`; re-running is free unless `REFRESH=1`. Prints the gold
distribution and multi-SIG count.

**Growing the corpus.** Bump `SUFFIX` rather than `REFRESH=1` — every
`cache/search-sig-*.json` is merged, so a wider pull under a new suffix ADDS to
the corpus and previously-scored issues stay in:

```
GH_TOKEN=$(gh auth token) PER_SIG=60 TARGET=250 SUFFIX=-p2 \
  bash eval/scripts/fetch-eval-corpus.sh
bash eval/scripts/make-splits.sh        # rebuild the tier manifest
WRITE=1 bash eval/scripts/gold-sanity-lint.sh   # rebuild the gold overlay
```

Two sampling rules keep the corpus honest, both in the fetcher:

- **Round-robin over SIGs, not the first N issue numbers.** A plain `[:target]`
  cut keeps whichever SIGs own the lowest-numbered issues and can starve a class
  below the support its own recall gate needs.
- **The ambiguous tail is retained at its natural rate.** Multi-SIG issues are
  neither dropped nor preferred: each SIG interleaves its multi- and single-label
  strata in proportion to its own multi-label rate. Drawing them first doubled
  ambiguity from 26% to 52% and made the corpus harder than the population it
  claims to sample; dropping them would make it easier.

**Label hygiene (important):** real issues carry `/sig <name>` prow commands and
`sig/<name>` mentions in the body — ~46% of a raw pull leaks its own answer. The
fetcher **redacts** those (`/sig ...`, `sig/...`, `sig-...`, `SIG <Name>`) from the
blind corpus, so the model must classify from technical content, not the label.
Verify: `grep -c '\[sig-redacted\]' corpus.jsonl`.

**Multi-SIG issues** (an issue with >1 sig label) become an `accept` set in
`gold.jsonl`: any of its labels — or a low-confidence `unknown` if you allow it —
counts correct. That encodes the real ambiguity instead of punishing it.

## 2. Run + analyze against your model

```
LEATHER_MODEL=qwen3.6-4b-instruct-2507-awq \
LEATHER_LLM_ENDPOINT=http://127.0.0.1:8000/v1 \
bash eval/run-eval.sh
```

Runs each blind issue through analyze->match, writes `predictions.jsonl`, and
prints the classification report + gate. Tune the gate:

```
MIN_ACCURACY=0.80 MAX_ABSTAIN=0.20 MIN_CORE_RECALL=0.80 bash eval/run-eval.sh
```

## The report

- overall accuracy (accept-set aware), accuracy on *answered* (excl. abstentions),
  abstention rate
- the tiered split: full / smoke / acceptance / holdout
- per-SIG **precision / recall / f1 / support / abstain**, with macro and
  weighted-f1 averages
- top confusions (gold -> predicted)
- gate: overall accuracy >= `-min-accuracy`, abstention <= `-max-abstain`,
  **macro-recall >= `-min-macro-recall`**, and each core SIG's recall >=
  `-min-core-recall` *for classes with support >= `-min-class-support`*  ->  exit 0/1

**Why the support guard.** A per-class recall floor is only meaningful where
support can carry it: at n=10 and p≈0.85 the standard error on recall is ~11
points, so a 90% floor gates on sampling noise — one defensible confusion is −8
to −14 points and flips the verdict. Below `-min-class-support` (default 20) a
class is printed with its support and excluded from the gate, and **macro-recall
(default >= 85%) is the primary per-class health check** instead.

Macro-recall averages over **the same classes the per-class floor applies to**,
and the report says which. That is not cosmetic: a singleton class can only score
0% or 100%, so averaging over every gold class lets a few 1-row classes swing the
headline gate. On this corpus three singletons at 100% recall inflated
macro-recall from 86% to 89% — enough to mask a real 3-point per-class
regression.

At the 93-row corpus every core class sat under the support guard, so
macro-recall alone carried per-class health. At 250 rows each core SIG has
support 21–25, the per-class floors apply for real, and they currently **fail on
5 of 6** — which is the honest signal the small corpus could not produce.

Read precision and recall together: high precision + low recall on a SIG means
the model is *cautious* about it (good — pair with abstention); low precision
means it *over-assigns* that SIG (a catalog-features problem you fix in
`sigs.reference.yaml`, deterministically, not by swapping models).

## Scoring robustness

Two normalizations keep the score measuring *classification*, not *formatting*:

- **Notation folding.** The catalog name is `sig-foo`; the GitHub label is
  `sig/foo`. Smaller models sometimes emit the label form. `sigeval.go` folds
  `sig/foo` -> `sig-foo` (and trims/lowercases) on both sides, so the two denote
  the same SIG instead of scoring as a miss.
- **Content-free issues are `unknown`.** A handful of real issues are pure noise
  (e.g. body `Created by mistake`). A triage bot *should* abstain on those, so
  gold treats any issue with a `< 60`-char body as `sig: unknown` — correct
  abstention scores correct, and the junk no longer drags a core SIG's recall
  denominator below what perfect classification could reach. The rule is a body
  length threshold (junk clusters at <= 19 chars; the next real issue is 427), not
  a hardcoded issue list.

## Reproducibility: `temperature: 0` must be set TWICE

A gate has to be reproducible or a flip diff measures dice. Greedy decode is set
in **both** `eval/config.eval.yaml` and every `agents/*.agent.md`. That is not
belt-and-braces — **neither one works alone**, for two interacting reasons:

- `agent/parseFrontMatter` defaults an agent's temperature to **0.7**, not to a
  sentinel. `resolveAgent` (`internal/cli/cmd_serve.go`) only falls back to the
  config value when the agent's is exactly `0`. So for an agent that never
  mentions temperature, the 0.7 frontmatter default **always shadows
  `config.yaml`**, and the documented priority (*lifecycle > config.yaml >
  built-in default*) does not hold. Setting it only in the config does nothing.
- `temperature: 0` in an agent is indistinguishable from *unset*, because `0` is
  the zero-value sentinel `resolveAgent` tests against. So setting it only in the
  agent sends you to `cfg.Temperature`, which is `0.7` unless the config says
  otherwise.

Setting both is the only combination that reaches the wire. Verify rather than
assume — the failure is silent:

```
LEATHER_MODEL=... leather workflow run ... 2>&1 | grep 'agent config'
#  ... temperature=0 ...     <- what you want
```

This is a real drift in leather, not an eval quirk; the same trap applies to any
agent wanting greedy decode. Until it is fixed, do not remove either setting.

## The catalog is a shadow: the model never reads it

Measured from the request body (not from log counts) via `scripts/logprob-proxy.py`:
`get_sig_reference` is **offered on 250/250 match requests and called on 0**. The
tool is wired correctly; the model declines it every time, because it can already
answer from the inline rule block in `agents/match.agent.md` plus its pretrained
knowledge of the public Kubernetes SIG taxonomy.

So the accuracy number measures a **prompt-driven** classifier, not the
read-the-catalog design. Practical consequences, learned the hard way:

- **Accuracy work goes in the prompt.** Editing `sigs.reference.yaml` to fix a
  confusion measures nothing. `sig-etcd` was added to the catalog by the currency
  check and changed no prediction until the same guidance went into the prompt.
- **The catalog can silently rot**, because nothing consuming it fails when it is
  wrong. Its real claim is *maintainability* — it is regenerable from upstream
  `sigs.yaml` — and that claim is proven separately from accuracy.
- **Nothing constrains predictions to the catalog's vocabulary.** The model
  emitted `sig-device-plugins`, which is not a SIG (device plugins are sig-node) —
  a name invented from priors, which is exactly the failure mode a catalog the
  model actually read would prevent. The prompt now enforces a closed vocabulary;
  a real fix would validate the prediction against the catalog in the scorer.

Testing the actual fetch loop (rather than the catalog's information content)
needs `tool_choice: required`, which leather does not expose — `http_client.go`
hardcodes `"auto"`.

### The inline-rules approach hits a prompt-dilution wall

This is the strongest practical argument for read-the-catalog, and it arrived
from the data rather than from the design doc.

> **Era note.** The stage-2 figures in this subsection (88.4%, 80.8%, −19 rows,
> the 84.8–88.4 replication set) were measured during the tuning phase on the
> **pre-re-baseline harness** and were not re-run after the fixes — treat the
> direction as the finding and the magnitudes as illustrative, not citable.
> The post-fix instrument's replication behaviour is the A-family in
> `results/runs/` (7 draws, 86.8–89.2%, mean 87.7%), which re-confirms the
> ±6-row null band.

Because the model will not fetch the catalog, **every ownership rule has to live
in the one `match` prompt**, and they compete for a finite budget. Stage 2
measured the ceiling directly. Round 1 added a single boundary rule and improved
things. Round 2 added four more — and classes the new rules **never mention**
collapsed:

| | round 1 | round 2 (4 more rules) |
|---|---|---|
| `sig-storage` recall | 88% | **50%** — not mentioned by any new rule |
| `sig-api-machinery` precision | 74% | **54%** — round 1's gain, reverted |
| overall | 88.4% | 80.8% (net −19 rows) |

**On that 88.4%:** it is the best of four draws of the *same* config, not the
config's value. Replicated 4×, round 1 scores 84.8 / 86.0 / 87.6 / 88.4 —
**mean ~86.7, spread 3.6 points**. Quote the mean. Round 2's −19 rows is far
outside that spread and is the real finding here; the headline number is not.

A rule cannot break a class it does not mention, or undo a rule whose text it
does not touch. Roughly doubling the instruction block degraded adherence
*across the board*. The rules were not wrong — the instrumentation rule in the
same bundle produced its predicted effect (`sig-network` 75% → 83%,
reproducibly) — the delivery mechanism was saturated.

That is a **scaling limit on the inline-rules approach**, not a tuning problem:
each new rule makes the existing ones less reliable, so the taxonomy cannot grow.
A retrieved catalog has no such ceiling — the model pulls only what it needs, and
adding the 30th SIG does not degrade the other 29. The tool-based design solves
a problem the prompt-based one provably has.

(2026-07-28: the ablation has since happened — see the campaign section above.
Measured: fetched/retrieved delivery beats pasted rules-free delivery, full
entries beat narrowed labels (+6.4 on the 4B), and the *catalog on top of the
hand-written rules* is a null at both scales (H≈A) — the retrieval design's
scaling argument stands, but rules remain the largest single lever at today's
taxonomy size.)

## Uncertainty: do not route on the model's self-report

`CONFIDENCE: high|medium|low` is emitted but is **not a usable routing signal**,
and this was measured rather than assumed. Over 250 issues:

| signal | AUROC | error after escalating 10% / 20% / 30% |
|---|---|---|
| verbalized `CONFIDENCE` | **0.48–0.51** | 16.0% / 16.5% / 14.3% |
| `sig_margin` (logprob) | **0.66–0.71** | 12.0% / 11.0% / 9.1% |
| `commit_margin` (logprob) | **0.64–0.73** | 11.6% / 10.0% / 9.1% |

(ranges are two runs; baseline error 13–16%. 2026-07-28 update from six repeat
draws of one identical config: sig-margin AUROC wobbles 0.55–0.68, mean ≈0.62 —
budget routing decisions at 0.62 ± 0.05, not the upper tail. The margin-vs-
self-report gap survives; the margin's absolute strength was overstated by
single-draw estimates.)

AUROC 0.5 is a coin flip, so the self-report carries **no** signal — escalating on
it can make things *worse*. This held even after an explicit calibration protocol
(name a `RUNNER_UP`, justify `high` with a `WHY_NOT_RUNNER_UP` fact, hedge when the
top two are close): the model still answered `high` on 97% of rows, and the handful
of non-`high` rows were *more* accurate than the rest. That protocol cost ~2.4
points of accuracy and bought nothing, so it was removed. **Prompting harder does
not fix this**; the finding matches the cascade-routing literature, where verbalized
confidence consistently underperforms token-level uncertainty.

The logprob margin is read off the *same* forward pass at no extra cost, via
`scripts/logprob-proxy.py` (leather exposes no `logprobs` knob, so the proxy
injects it). Two margins are recorded, and the distinction matters:

- `commit_margin` — at the first token after `SIG:`. Every catalog name starts
  `sig-`, so this token is the shared prefix: it measures *commit vs abstain*.
- `sig_margin` — at the first token that actually discriminates between names
  (`-network` vs `-node`). This is label-level uncertainty.

Reading the first as label confidence would report near-total confidence on every
row. Reproduce with `LOGPROB=1 bash eval/run-eval.sh` then
`python3 eval/scripts/compare-uncertainty.py`.

**Runner-up recovery** is reported alongside: of the rows the top pick got wrong,
50–71% had gold as the model's own `RUNNER_UP`. That is the ceiling for a top-2
adjudication step, and the reason one is worth building.

One measured trade-off, both sides inside the noise floor: the `WHY_NOT_RUNNER_UP`
protocol *raised* runner-up recovery (71% vs 50%) while *lowering* base accuracy
(84.4% vs 86.8%). Perfect-adjudicator ceilings land at 94.0% vs 93.2% — a 0.8-point
gap that this corpus cannot resolve. Revisit only if an adjudicator turns out to be
starved of good runner-ups.

## Two-pass adjudication (experimental, harness-only)

**This is an experiment in the eval harness, not a leather feature, and not a
pattern to copy into a production tannery.** leather has no content-conditional
routing: a curing's `output.queue` is a static name that fires on every success,
and the tannery router matches on source/event type/hide kind — envelope
metadata, never artifact content. So the "send the uncertain ones to a
tie-breaker" decision is made *by the harness*, which reads pass-1 predictions
off disk and ingests the selected minority into `adjudicate-in` itself.

```
LOGPROB=1 bash eval/run-eval.sh                    # pass 1 (margins required)
COVERAGE=20 bash eval/scripts/adjudicate-pass.sh   # pass 2 over the lowest-margin 20%
```

Escalation is by `sig_margin`, **not** by `CONFIDENCE` — routing on the
self-report would be routing on AUROC 0.48. That constraint is the interesting
part: a conditional-routing feature inside leather could only see artifact
*text*, so it could only route on the signal measured above as worthless. Getting
this right requires the uncertainty connector as much as the router, which is why
[LEP-0008](../../../docs/LEP-0008-conditional-routing.md) carries both.

Two design details that exist to keep the measurement honest:

- **Candidate order is set by issue-number parity**, not by which SIG pass 1
  preferred. Always showing the top pick first would let the adjudicator score
  well by agreeing with position 1, and the result would look like reasoning.
  Parity is deterministic across re-runs and balanced across positions.
- **The merge is fail-safe.** A tie-break that is missing, self-inconsistent
  (`VERDICT` and `SIG` disagree), off-ballot, or explicitly `neither` leaves
  pass 1's answer standing. The second opinion can improve the score or decline;
  it cannot destroy it. Each outcome is counted, because a pass that "helps"
  while silently declining 40% of its cases is not a result.

The adjudicator runs with `thinking: true` — it is deliberately the expensive
arm, since the entire premise of escalation is spending more compute only where
it pays. Its cost is reported as a compute multiplier (coverage 20% ≈ 1.2×).

## Gold provenance: the overrides overlay

`gold.jsonl` is **byte-identical to the fetcher's output** and must stay that
way — hand-editing the answer key means a re-fetch silently clobbers or drifts
from your corrections. Relabels live in `gold.overrides.jsonl`
(`{number, sig?, accept?, reason}`, applied at load, overrides win):

```
make -C examples 14-eval-lint             # check: fails closed on a violation
WRITE=1 bash eval/scripts/gold-sanity-lint.sh    # regenerate the overlay
```

The overlay is **generated by the declared predicate** (`MIN_BODY_CHARS=60`),
not hand-maintained, so it is reproducible as well as diffable — every row
carries the `reason` that produced it. On the current corpus it isolates exactly
the five "Created by mistake" rows with no false positives. Each override keeps
the issue's real labels in `accept`, so only the demand for a *specific* concrete
answer is lifted; a model that guesses one of them is not punished either.

## The tiered split

`splits.jsonl` (`{number, tier}`) is the committed record of which rows tuning
was allowed to see. Three disjoint tiers, stratified so each core SIG splits
5/15/5 — a tier that starved a class would make its per-class numbers unreadable:

| tier | rows | role |
|---|---|---|
| `smoke` | 53 | the fast iteration slice — **the only tier tuning may look at** |
| `acceptance` | 151 | the rest of the gate of record; not tuned on |
| `holdout` | 46 | never tuned on, never gated on — the generalization check |

```
split by tier:
  full:         86.8% (217/250)
  smoke:        84.9% (45/53)     tuned on -- expect the best number here
  acceptance:   85.4% (129/151)   gate of record, not tuned on
  holdout:      93.5% (43/46)     never tuned on <- generalization check
```

holdout ≈ full means the catalog/prompt rules generalized. smoke ≫ holdout means
they memorized the slice — reject the change no matter what the aggregate did.
It is a **reported** rail, not a gate: at n=46 the holdout standard error is still
~±5 points, enough to catch gross memorization and not enough to threshold on.

Rebuild after a re-fetch with `bash eval/scripts/make-splits.sh`. Assignment is
deterministic (round-robin over each SIG's issues in number order), not a random
seed, so membership is stable and auditable. A corpus row missing from the
manifest is reported as `(untiered)` rather than silently folded into a tier.

The `analyze`/`match` agents run with `thinking: false` (Qwen3 no-think): the
`match` prompt reasons in a visible `REASONING:` line before committing to `SIG:`,
which is faster and avoids long hidden traces timing out mid-run.

## Wiring

`go test ./eval/` verifies the scorer's own math. Gate PRs that touch
`sigs.reference.yaml`; drive catalog refreshes from `scripts/check-taxonomy-currency.sh`.
See `Makefile-snippet.txt`.

## Taxonomy currency

`scripts/check-taxonomy-currency.sh` (cron) hashes the upstream SIG list and
signals drift; `scripts/gen-taxonomy.sh` parses the names out of
kubernetes/community `sigs.yaml`. Run the cross-check after any corpus re-fetch —
old issues can carry retired or renamed SIG labels, and old gold is wrong gold:

```
bash scripts/gen-taxonomy.sh     # current upstream SIG names (fails loudly if empty)
```

Compare against the labels in `gold.jsonl` and against `sigs.reference.yaml`.
A gold label that is no longer upstream should be relabelled **via the overrides
overlay**, never by editing raw gold. A gold label that is upstream but missing
from the catalog is unpredictable by construction — author `features` for it.
That check is what added `sig-etcd` to the catalog.

# Pre-registration — sig-triage confirmatory battery (T1)

**Status: REGISTERED.** Both open decisions signed by Tyler Pate 2026-07-29;
frozen by the commit that introduces this file on `main` — that commit hash
is the registration timestamp. Nothing in the "Registered contrasts" or
"Analysis plan" sections may change after that commit; any change requires a
dated amendment section appended below, never an edit. The registration
artifact is itself paper content (methods §: "contrasts and analysis were
pre-registered at commit `<sha>` before any confirmatory run").

## Purpose

The exploratory campaign (46 cells, 2026-07-28) generated the hypotheses.
This battery is **confirmatory**: a fixed, pre-declared set of contrasts,
replicated, analyzed under a pre-declared policy — the answer to "you ran 22
arms and reported the ones that hit."

## Registered contrasts (six, 4B rig, frozen model + corpus + scorer)

| # | contrast | isolates | exploratory result (single draws) |
|---|---|---|---|
| 1 | A0 vs B | hand-written rules (tool absent both sides) | +12.4 |
| 2 | G vs E2 | retrieval payload: full entries vs bare labels | +6.4 (RESOLVED) |
| 3 | P2 vs P1 | order: task before reference | +6.0 (RESOLVED) |
| 4 | T3 vs T2 | decomposition depth: 3 turns vs 2 | −9.2 (RESOLVED) |
| 5 | S1 vs T2 and T2c vs T2 | context bounding: stage split / per-turn clear | −14.8 / −11.2 (RESOLVED) |
| 6 | T2cr vs T2c (and vs T2) | carrier vs clearing — is T2c's loss the clear or the shortlist? | not yet run under fixed runner |

Frozen inputs, recorded by sha in every manifest: model
(Qwen3-4B-Instruct-2507-AWQ), corpus (250 rows), gold + overrides, splits,
scorer (`sigeval.go`), prompt files per arm. Runner: leather ≥ v0.5.0
(post-fix; out-of-scope refusals recoverable).

## Analysis plan (frozen at registration)

- Scorer of record: `sigeval.go`; verdicts: `paired-verdicts.py` (McNemar
  exact on discordant pairs), confounds flagged from manifest diffs.
- Primary metric: accept-set accuracy on the full 250. Holdout tier reported
  as a rail, not a gate.
- Effects inside the null band reported UNRESOLVED; no post-hoc metric
  swapping; abstention and tool-adherence reported as secondary mechanism
  metrics for every contrast.
- Temporal holdout (T1.4): fresh post-freeze issues scored with zero changes
  to any prompt/config; reported separately; no iteration permitted on it.

## DECISION 1 — replication count · SIGNED

**Registered: 3× per arm-side, with a pre-declared bump to 5× triggered only
if a registered contrast lands within 1 point of its decision boundary.**
(Signed: Tyler Pate, 2026-07-29.)

Options considered, with what each buys at the measured ±6-row (~2.4%)
single-run band:

- **3× per arm-side** (~36 runs total for six contrasts): detects ~3-point
  paired effects; leaves #2/#3 (~6-point claims) comfortably resolved if
  real; cheapest.
- **5× per arm-side** (~60 runs): tightens to ~2-point resolution; overnight
  batteries × several nights.
- Asymmetric (5× on the two structure contrasts, 3× elsewhere) — matches
  effect sizes to cost.

Suggested default: **3×, asymmetric bump to 5× only if a registered contrast
lands within 1 point of its decision boundary** (declare that trigger here so
it isn't post-hoc).

## DECISION 2 — multiplicity policy · SIGNED

**Registered: Holm–Bonferroni across all six primary contrasts at α=0.05.**
(Signed: Tyler Pate, 2026-07-29.)

Six registered contrasts ⇒ six primary tests. Options considered:

- **Holm–Bonferroni across the six primaries at α=0.05** — standard,
  conservative, easy to defend in review.
- Per-contrast α=0.05 with the six-fold multiplicity stated plainly and
  effect sizes carrying the argument — honest-disclosure style, weaker to a
  hostile reviewer.
- Hierarchical: structure contrasts (#4/#5/#6) as primary family with Holm,
  delivery (#2/#3) and rules (#1) as declared secondary.

Suggested default: **Holm across all six** — the exploratory effects are
large enough that Holm costs nothing if they're real, and surviving it is a
stronger sentence in the paper.

## What is deliberately NOT registered

Second-family (T2) runs — those get their own registration addendum once the
family passes preflight, because "same harness" needs its definition written
per family (prompt SHAs constant; tokenizer/template vary) before anything
runs.

---

# Amendment 1 — 2026-07-30 (appended; nothing above is edited)

**Context.** The confirmatory battery completed 2026-07-30T08:19 (33/33
registered cells: 11 arms × 3 replications, wave-ordered, plus the E2-c1
makeup). All six registered contrasts resolve under Holm at α=0.05. Two
items require registration-level decisions before any further confirmatory
cells run.

**Disclosure required in the paper.** DECISION 3 below specifies a rule that
the original registration did NOT fix, and it is being specified *after* the
3× results were seen. That is a genuine deviation from ideal
pre-registration and must be stated as such in the methods section — not
presented as if it had been registered in advance. Mitigation: both
combination readings are computed and reported
(`eval/scripts/confirmatory-verdicts.py`), so the alternative is auditable
rather than hidden. The rule is being fixed now, before the DECISION 4 cells
run, so it is at least pre-specified with respect to the remaining data.

## ⚠ DECISION 3 — how the three replications combine (Tyler signs)

The registration fixed the scorer (sigeval), the test (McNemar exact on
discordant pairs), the multiplicity policy (Holm across six) and the
replication count (3×) — but not how three paired replications combine into
one primary p-value.

This is load-bearing for exactly one contrast: **#2 (G vs E2) resolves only
when pooled.** Its three pairings are p=0.19 / 1.0 / 0.009 individually;
pooled p=0.010. Contrasts 1, 3, 5, 6 resolve under either reading;
contrast 4's c3 pairing (p=0.12) misses individually but the other two hold.

Options:

- **Pooled (recommended).** Concatenate per-issue verdicts across the three
  waves; McNemar exact on the pooled discordant pairs (n=750). Cannot be
  gamed by wave selection, is the conventional combination for replicated
  paired designs, and uses all the evidence. Per-wave tests reported as a
  secondary consistency check.
- **Per-wave majority.** A contrast resolves only if ≥2 of 3 wave-level
  tests resolve. More conservative; would leave contrast #2 unresolved.
- **Per-wave then combine (Fisher/Stouffer).** Statistically respectable,
  but adds a second combination rule to defend and lands between the two
  above.

SIGNED: Tyler Pate, 07/30/26

SELECTION: Pooled - Concatenate per-issue verdicts across the three
  waves; McNemar exact on the pooled discordant pairs (n=750). Cannot be
  gamed by wave selection, is the conventional combination for replicated
  paired designs, and uses all the evidence. Per-wave tests reported as a
  secondary consistency check.

## ⚠ DECISION 4 — scope of the signed 5× boundary trigger (Tyler signs)

DECISION 1 registered a bump to 5× "triggered only if a registered contrast
lands within 1 point of its decision boundary." Measured on the completed
battery (band = ±2.4 points):

| contrast | pooled effect | distance from band edge | triggered |
|---|---|---|---|
| 1 A0 vs B | +12.8 | 10.4 | no |
| 2 G vs E2 | +2.9 | **0.5** | **YES** |
| 3 P2 vs P1 | +6.5 | 4.1 | no |
| 4 T3 vs T2 | −5.2 | 2.8 | no |
| 5a S1 vs T2 | −14.5 | 12.1 | no |
| 5b T2c vs T2 | −11.6 | 9.2 | no |
| 6 T2cr vs T2c | +6.9 | 4.5 | no |

Options:

- **Contrast-scoped (recommended).** Bump only the triggered contrast: G and
  E2 to five draws each = **4 new cells** (G-c4/c5, E2-c4/c5), ~1 rig
  evening. Matches the registered wording ("if a registered contrast
  lands…") and spends replication where the resolution is actually in doubt.
- **Family-wide.** All six contrasts to 5×: 20+ additional cells, no
  inferential gain on contrasts already at p≈1e-11.
- **No bump.** Report contrast #2 as resolved-but-marginal with its
  per-wave spread stated. Cheapest, but declines a trigger that was signed
  in advance specifically to handle this case — hard to defend in review.

Any bumped cells are analyzed under the DECISION 3 rule, and the Holm family
remains the same six primaries (no new tests are added by the bump).

SIGNED: Tyler Pate, 07/30/26

SELECTION: Contrast scoped - Bump only the triggered contrast: G and
  E2 to five draws each = **4 new cells** (G-c4/c5, E2-c4/c5), ~1 rig
  evening. Matches the registered wording ("if a registered contrast
  lands…") and spends replication where the resolution is actually in doubt.

---

# Amendment 2 — 2026-07-30 (appended; nothing above is edited)

**Context.** Amendment 1 fixed the replication-combination rule as *pooled*
McNemar and disclosed that the rule was specified after the 3× results were
seen. External review then identified an error in that choice, and the error
is real.

**The error.** Pooling concatenates `cN:issue` across waves and runs McNemar
as if each row were an independent trial. It is not: the same 250 issues are
re-measured every wave, so a systematically hard issue contributes a
discordant pair in *every* wave. Repeats do not manufacture independent
issues. Amendment 1 called pooling "conservative" — true against
wave-**selection** gaming, false statistically. Two senses of the word were
conflated, and the consequence is that pooled p-values **overstate**
significance.

**The correction (DECISION 5).** The primary estimator becomes an
**issue-clustered sign-flip permutation test**: per issue, take each arm's
mean correctness over its repeats, form the paired difference, and permute by
flipping the sign of whole issues — the exchangeability the null actually
asserts. 20 000 permutations, fixed seed, two-sided. Holm across the six
primaries is unchanged. Pooled McNemar is retained in the output as a
descriptive continuity figure and explicitly labelled not-the-verdict;
per-wave McNemar remains the robustness table.

**Disclosure.** This is the second post-hoc change to the analysis rule, and
like the first it is recorded rather than folded in silently. It differs from
Amendment 1 in an important way: it can only *weaken* results, and it did.

**What it costs.** Under the clustered estimator, **five of six contrasts
resolve; contrast 2 (retrieval payload, G vs E2) does not** — Holm-adjusted
p = 0.067 against 0.0008 pooled. Effect sizes are unchanged; only the
uncertainty was wrong. Contrast 2 is now reported as **UNRESOLVED**: a
positive point estimate of about +3 points that five draws per side could not
separate from the null band. That is also the contrast the registered
boundary trigger flagged and the only one that never resolved at any single
wave — three independent signals agreeing.

**Not re-registered:** the contrasts, arms, corpus, scorer and replication
counts are untouched. Only the combination rule changes.

## ⚠ DECISION 5 — adopt the clustered estimator as primary (Tyler signs)

Options:

- **Adopt (recommended).** Primary = issue-clustered permutation; contrast 2
  becomes unresolved; the paper reports five resolved contrasts and one
  suggestive-but-unresolved lever. Defensible to any reviewer who checks.
- **Retain pooled.** Keeps six resolved contrasts, but the independence
  assumption is indefensible if a reviewer looks, and one of them looks.
- **Report both as co-primary.** Honest but muddy; a reader cannot tell which
  number to cite.

SIGNED: Tyler Pate, 07/30/26

SELECTION: Adopt - Primary = issue-clustered permutation; contrast 2
  becomes unresolved; the paper reports five resolved contrasts and one
  suggestive-but-unresolved lever

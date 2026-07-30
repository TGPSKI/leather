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

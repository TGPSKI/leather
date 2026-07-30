# Pilot run 3 — 4B re-run + ergonomics probe → anchor decision (2026-07-30)

14 cells on the 4B at the corrected ceiling (24576): the 10 PILOT-1 cells
re-run clean, plus the signed ergonomics probe (Be/V2e — `edit_file`
single find/replace primitive instead of full-file `write_file`) × both
families. Local vLLM serving the byte-identical frozen model; scored under
the signed semantic-consistent policy.

## Result: 0/14 pass, 0 action calls — anywhere

| observation | evidence |
|---|---|
| Narration floor is real, not a ceiling artifact | zero `finish_reason=length` anywhere; every cell a clean full-budget run |
| Ergonomics ruled out | Be/V2e: **zero `edit_file` calls** — the cheap primitive changed nothing; the bottleneck is the decision to act, not the cost of acting |
| PILOT-1's 4B action did not replicate | V2-pin/V-prt/V2-prt made 3/3/6 writes at the 8k ceiling; 0/0/0 here. Aggregate across all draws: B/R/E/Be **0 writes in 16 cells**; V/V2/V2e acted in 3 of 10 |
| More budget → more narration, not more action | pilot2 cells burned ~25k tokens each vs ~10k in PILOT-1 — 2.5× the generation, all of it prose |

The honest statement of the 4B action finding after replication:
verification-structured arms are the **only** condition under which this
4B has ever acted, and its action rate under them is a draw-level coin
flip. Action-rate is a noisy per-cell statistic that needs replication
counts exactly as accuracy did in ex-14 — single-cell action claims are
single-run optimism on a new axis.

## Anchor decision — RESOLVED (per the signed probe-first plan)

The probe was the decision procedure and it returned: **anchor the corpus
on the 35B; the 4B's contribution is the scale finding.** The 4B narrates
without acting (16/16 non-verification cells, two ceilings, two serving
configs); the 35B acts — sometimes compulsively without concluding. "The
same protocol detects that a model class cannot execute the task
contract" is the protocol-validation framing, now with the ergonomics
alternative measured and excluded rather than assumed.

## Signed-policy impact on the 35B grid (workdir re-scores; archived
pre-policy verdicts unchanged)

- V-prt: **flips to PASS** — its only failure was its own explanatory
  comment re-triggering a pattern rule; strict-clean stays False and is
  reported as the component score.
- V2-prt: still fails — the 38-write loop left a genuine code-level
  violation, not just prose. The policy does not rescue chaos.
- B-prt: still fails — the leftover head-ref checkout is real code; this
  is the minimal-vs-complete boundary, to be specified per-defect in
  task.json before freeze (open policy decision #2).

**35B under signed policy: 7/10.** Corpus dynamic range confirmed with
the oracle policy that will actually govern the corpus.

## Carried forward to corpus build

- Arms for the confirmatory family: keep the verification axis (it is
  the discriminating axis on both models, in opposite directions); the
  evidence axis stays as the cross-task contrast with sig-triage.
- Reasoning-budget control for the 35B (V2's truncation/rumination
  sensitivity) must be a declared harness parameter, not an accident.
- Per-defect expected end-state in task.json resolves the
  minimal-vs-complete boundary (B-prt is the motivating cell).
- 4B cells stay in the confirmatory matrix as the scale finding — cheap,
  and the 0-action floor replicating across the corpus is itself the
  cross-scale result.

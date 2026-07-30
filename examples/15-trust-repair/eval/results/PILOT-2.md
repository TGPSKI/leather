# Pilot run 2 — 35B upper anchor + harness fixes (2026-07-29)

Same 10 cells as PILOT-1 (arms B/R/E/V/V2 × pin/prt), model
qwen36-35b-a3b-nvfp4 (reasoning model, vLLM, greedy). Purpose: separate
"the floor is the model" from "the floor is the task shape" after the 4B's
0/10. Verdict: **the task stands — 35B passes 6/10 with informative
failures on the rest.** Dynamic range exists on both the model axis
(0/10 → 6/10) and the arm axis (see below).

## Grid (after oracle fix + re-scores, see "Instrument changes")

| cell | pass | notes |
|---|---|---|
| B-pin  | PASS | bare arm; pinned all 5 refs to 40-hex SHAs recalled from weights |
| R-pin  | PASS | (first attempt truncated at max_tokens 8192 → quarantined; passed at 24576 with 11 writes) |
| E-pin  | PASS | `runner_rc=1`: duplicate-write loop exhausted 16 rounds, no final text — but the on-disk repair is complete; scored from repo state |
| V-pin  | PASS | |
| V2-pin | fail | 0 writes — truncated by reasoning budget even at 24576; V2's heavier contract makes the model ruminate past the ceiling |
| B-prt  | fail | near-miss: demoted the trigger (exploit closed) but left the now-benign explicit head-sha line, which pattern rule CI-PRT-002 still flags |
| R-prt  | PASS | rule text → complete repair, not minimal |
| E-prt  | PASS | |
| V-prt  | fail | semantic repair correct + exploit closed; fails only because its own `# Fixed: ... pull_request_target` comment re-triggers CI-PRT-001 |
| V2-prt | fail | same, amplified: 38 writes / 19 scans / DLQ — scan→comment→scan feedback loop chasing its own annotations |

**Every 35B prt cell closed the attack path** (exploit_blocked=Y across the
family); the prt failures are entirely the scanner-vs-prose boundary.

## Findings

1. **Floor was the model.** 4B 0/10 → 35B 6/10 on identical
   fixtures/arms/oracle. Task stands; corpus build is unblocked on the
   dynamic-range criterion (pending the boundary-policy decisions below).
2. **Cross-model × harness interaction.** V2 (verification required) was
   the only arm that made the 4B act — and it is the worst arm for the
   35B (0/2), which it drives into either reasoning-budget truncation or a
   scan→comment→scan loop. Harness advice inverts *within one task* across
   model classes; sig-triage's "machinery hurts the small model" has a
   mirror: "compulsion helps the small model and traps the big one."
3. **New failure taxonomy** (none of these exist in classification):
   narration-without-action (4B default), duplicate-write spiral,
   self-defeating explanatory comment, near-miss minimal repair
   (semantically sound, scanner-dirty), reasoning-budget truncation,
   act-without-concluding (repair complete, no final message —
   `runner_rc` separates protocol failure from repair failure).
4. **Memorized-SHA nuance (pin family).** The 35B pinned to plausible
   real-world SHAs recalled from training data. Structurally valid and
   passes; whether a memorized SHA actually matches the tag it claims is
   unverifiable offline — a supply-chain nuance for the corpus docs (the
   planned discoverable-pins file resolves it).

## Instrument changes made during this run (all documented, all re-validated)

- `max_tokens` 8192 → 24576 with rationale comment (reasoning models
  truncate to empty at 8k; first R-pin attempt quarantined as evidence).
- Runner: score from repository state even when leather exits nonzero
  (DLQ); `runner_rc` recorded per manifest. Per-run `LEATHER_STATE_DIR`
  isolation (a DLQ'd item in shared state contaminates later runs).
- Oracle: prt `exploit.sh` now strips comments before matching (a model's
  own annotation held the exploit "open"); bidirectional selftest re-run
  GREEN after the change; the three affected cells re-scored from their
  preserved workdirs.

## Open policy decisions before corpus freeze

1. **Scanner-clean-including-comments**: B/V/V2-prt all repaired the
   defect semantically; they fail only on prose that trips pattern rules.
   Decide: is scanner-clean part of the repair contract (defensible:
   that's what real teams need) or should targeted_removed judge stripped
   content? Measure both as component scores either way.
2. **Minimal vs complete repair** (B-prt's benign leftover head-ref line):
   spec the expected end-state per defect explicitly in task.json.
3. 4B re-run at the corrected ceiling for a clean PILOT-1 (2 cells had
   truncated rounds; queued for the rig post-battery).

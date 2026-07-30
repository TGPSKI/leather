# Incident — the resume guard overwrote three finished S1 cells

**2026-07-30. Data destroyed; published figures corrected; conclusions
unchanged.** Kept here for the same reason the
[quarantine](quarantine/) directory is kept: an instrument you are invited to
argue with has to show its failures, and this one changed a number.

## What happened

`confirmatory-battery.sh` treated a cell as complete only if its archive had
250 rows **and ≥225 answered rows**. S1's mechanism *is* row loss — a
fresh-session stage boundary makes ~11% of rows unanswerable, reproducibly
(25 / 26 / 28 / 30 / 34 across five draws), i.e. ~220 answered. The guard
therefore judged all three *finished* S1 cells incomplete.

A later invocation — run to fill a single genuinely missing cell — re-ran
S1-c1, S1-c2 and S1-c3 and archived the new draws **over** the originals.
Only S1 crossed the threshold; every other arm's worst draw stayed well
inside it (T2c ≤4 unanswerable, T3 ≤6, G ≤7, E2 ≤6), and no other cell was
rewritten.

The confirmatory analysis then raced that invocation: it read S1's original
archives before they were replaced. So the first reported S1 figures describe
predictions that no longer exist on disk.

## How it was caught

Two independently-derived views of the same cell disagreed — the battery log
said S1-c1 scored 63.2%, the archive scored 60.4% — while building a results
browser that renders both. Nothing in the pipeline flagged it; only the
comparison did.

## Data state

| S1 draw | set A (original, archives destroyed) | set B (current archives) |
|---|---|---|
| c1 | 63.2 | 60.4 |
| c2 | 62.0 | 59.6 |
| c3 | 61.2 | 61.2 |
| mean | 62.1 | 60.4 |

Set A survives only as accuracies in the battery log; its predictions are
gone, so no paired test can ever be recomputed on it. Set B is what the
repository contains and what every published figure now uses.

## Effect on the registered contrasts

Only contrast 5a moves. **All six contrasts remain RESOLVED under Holm**, and
the Holm table is numerically unchanged, because contrast 5's family p-value
is taken from the less significant of its two arms (T2c), which was untouched.

| | reported first (set A) | current (set B) |
|---|---|---|
| 5a S1 vs T2 | −14.5, p=5.3e-15 | **−16.3, p=4.8e-18** |

**This correction is not conservative.** The surviving archives make the S1
effect *larger*, so choosing them is not a cautious choice and is not
presented as one. Both draw-sets agree in direction and both sit far outside
the ±2.4-point null band.

## Fix

Completeness no longer encodes a quality threshold. A cell is complete when
it has 250 rows and a run manifest — *did it run*, not *did it run well*.
Quality remains `verify-run.sh`'s job, reported per cell and never used to
decide whether to re-run.

The general lesson, which is the reason this file exists: **a resume guard
that a legitimate arm fails by design will silently destroy exactly the arm
whose mechanism is most interesting.** S1 is the campaign's largest effect and
the only arm the guard could delete.

## Follow-up

A clean S1 set was re-run under the fixed guard at draws c6–c8, paired with
fresh T2 draws so the registered comparison stays paired. Those cells are
archived alongside the others; all three draw-sets are disclosed rather than
one being chosen.

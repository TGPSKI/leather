# Wreck — 4b-T2c0-2, two runners against one state directory

**2026-07-30. Cause: operator error (mine). No registered data affected.**
Exploratory cell only; the registered battery was closed before this ran.

## What the archive shows

250 rows, **246 with no usable match artifact**, and `verify-run.sh` raised a
contradiction it is specifically built to catch:

```
[FAIL] 246 rows had no usable match artifact
[FAIL] CONTRADICTION: rounds/issue=1.75 but leather logged 0 tool executions
```

A cell cannot make 1.75 LLM rounds per issue and execute zero tools. The two
numbers come from different sources — the logprob proxy and leather's own
evidence log — and disagreeing is only possible if the state directory those
sources write to was destroyed mid-run.

## What happened

After a power outage I checked whether the exploratory runner had survived,
misread its state, and launched a **second** runner while the first was still
alive. Both invoke `eval/run-eval.sh`, which owns
`eval/.state-eval-4b/`. The second run cleared that directory as part of its
startup, deleting the first run's in-flight artifacts. The first run then
archived what remained: predictions with almost nothing behind them.

The second runner failed immediately afterwards on the logprob-proxy port
guard (`port 8021 is already serving; refusing to start a second proxy`) and
wrote no archives of its own — so exactly one cell was damaged.

## Why it is kept

This is the same failure class as the campaign's original contamination
incident: **two processes sharing one state directory**. That one forced a
full 22-arm re-baseline. This one cost a single exploratory cell, because the
claim ledger caught it in the cell that produced it rather than three days
later in a summary table.

It is also the second time in this campaign that a *guard* — not a person —
was the thing that limited the blast radius. The port guard stopped the
second runner before it could write anything, and `verify-run.sh` refused to
let the damaged cell pass as a result.

## Fix

- The cell was re-run cleanly and the replacement is in `runs/4b-T2c0-2`.
- Operational rule, same family as the branch-switch hazard already recorded
  in `confirmatory-battery.sh`: **never start a battery without confirming no
  other battery is live.** `pgrep -f run-eval.sh` before launching; the
  per-rig lock only covers `confirmatory-battery.sh`, not ad-hoc runners.
- The exploratory runner in `eval/.battery/` should acquire the same
  `.lock-<rig>` directory the confirmatory battery uses. Filed as follow-up;
  the lock is the structural fix, the rule above is the interim one.

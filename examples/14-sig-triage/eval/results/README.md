# SIG-triage eval — results and lessons

(Every figure here comes from the post-fix verified archives in `runs/` —
see the provenance note below for the history of how that came to matter.)

Everything measured while turning `examples/14-sig-triage` from a demo into a
gated eval: the ablation campaign, the run archives behind it, and the findings
worth carrying to the next example.

**What is being demonstrated.** leather's claim is bounded, single-purpose agents
with controlled context and tools, orchestrated over queues with fan-out/fan-in
and parallelism, so that **small local models produce consistent, gate-able output
with no frontier model and no API**. This eval is one measurement of that claim on
a real task: classify kubernetes/kubernetes issues into one of 22 SIGs, using a
two-stage pipeline (`analyze` → `match`), one model call per stage, bounded
context per stage.

## The setup

| | |
|---|---|
| task | 22-class SIG classification from issue title + body |
| corpus | 250 issues, tiered smoke / acceptance / holdout, stratified per class |
| ground truth | upstream `sig/*` labels, plus a rule-generated overrides overlay |
| scoring | accept-sets (multi-SIG issues), abstention-aware, macro-recall with a support floor |
| rigs | Qwen3-35B-A3B NVFP4 (MoE) and Qwen3-4B-Instruct-2507-AWQ, both local, vLLM |
| decoding | `temperature: 0`, `thinking: false` |
| matrix | 22 arms × 2 rigs = 44 verified cells + 2 quarantined wrecks = 46 archives |
| replication | 56 run directories in `runs/` (the 44 cells plus repeat draws: A×7, H×6, T2c×2) |
| noise floor | ±6 rows (~2.4%) — measured pre-campaign, re-confirmed by the post-fix A-family spread |

## Headline

The same frozen **4B model spans 62.4% → 81.6%** across arms — floor at the S1
fresh-session stage split, ceiling at the F2 single-stage split — with runtime
design as the only variable. The committed pipeline (arm A) scores **77.6%** on
the 4B and **86.8%** on the 35B (A replicated 7×: 86.8–89.2, **mean 87.7**).

The same models handed only the bare label set (arm B) score **62.8%** (4B) and
**68.4%** (35B) — so configuration is worth **~15 points on the 4B and ~18 on
the 35B**. The load-bearing part: the discipline transfers *down* — a 4B running
locally, correctly configured, beats the bare 35B by 9 points on this task.
(An earlier draft reported the delta as an identical ~16 at both scales; that
symmetry did not survive the re-baseline and is retired.)

## Contents

| file | what's in it |
|---|---|
| [`../ablation/arms.json`](../ablation/arms.json) | every arm: its parameters, the ONE variable it isolates, and the arm it is read against |
| [lessons-leather.md](lessons-leather.md) | framework findings: silent config no-ops, queue concurrency, tool-choice, routing limits |
| [lessons-vllm-models.md](lessons-vllm-models.md) | model and server findings: uncertainty signals, `tool_choice` hangs, thinking, the scale gap |
| [lessons-eval-methodology.md](lessons-eval-methodology.md) | how to run an eval that can be trusted — null bands, tiers, replication, provenance |
| [verification.md](verification.md) | the claim ledger: which artifact proves each load-bearing claim |
| `runs/` | per-cell archives: predictions, sigeval report, logprobs, evidence log, `run-manifest.json` |
| `quarantine/` | wrecked runs kept with post-mortems; do not resurrect |

The arm-by-arm leaderboard is generated from the archives, never hand-edited:
`python3 eval/scripts/table.py`. Paired comparisons with verdicts:
`python3 eval/scripts/paired-verdicts.py`.

## Reading these numbers

Three rules were adopted the hard way and apply to every number here.

**Every published figure is a mean with its spread, or a single draw said to be
one.** The baseline config replicated seven times scored 86.8 / 87.6 / 87.6 /
89.2 / 88.0 / 87.6 / 86.8. A single draw of it would have supported any claim
between "middling" and "89.2%". Where a cell ran once, the number is the draw,
not the config's value.

**Verdicts are against a measured null band, not against zero.** Repeat draws
of an unchanged config differ by up to ±6 rows. Anything inside ±6 is reported
UNRESOLVED — not "no change", but *the experiment could not tell*.

**The gate is failing, and that is stated rather than omitted.** `core-recall`
at ≥90% is missed by 5 of the 6 core SIGs (api-machinery, apps, network, node,
storage) in the baseline configuration. Accuracy, abstention and macro-recall
pass. No threshold was lowered to make anything green.

## Provenance

> **Provenance note (2026-07-28).** An earlier draft of this package carried
> figures from before a set of harness fixes (`d5d8d23`, `eaf377a`, `5eeb03e`,
> `6295cd7`) and was frozen behind a do-not-publish banner: leather's queues
> lived in a shared `state_dir` that was never cleared between runs, so a killed
> run could leak queue items into the *next* run's artifacts — contamination
> that could not be bounded after the fact. (Narrower defects hit specific arms:
> a tool schema emitting `required: null`, and a proxy misfiling match calls
> under the wrong stage.) The full matrix was then **re-run on the fixed
> harness**; every figure in this package comes from those verified archives — each
> carries a `run-manifest.json`, its evidence under `runs/<tag>/`, and a
> `verify-run.sh` pass. The lessons documents era-tag the few remaining
> tuning-phase numbers as non-citable. The methodology stood throughout — it is
> what caught the problem.

## Reproducing

```bash
cd examples/14-sig-triage
LEATHER_MODEL=... LEATHER_LLM_ENDPOINT=... LOGPROB=1 bash eval/run-eval.sh   # one cell
bash eval/scripts/run-battery.sh <35b|4b>                                    # the matrix
```

`LOGPROB=1` routes through `eval/scripts/logprob-proxy.py`, which records
token-level uncertainty margins and — the counter that mattered most here —
whether each request actually carried a `tools` array. Verify any cell before
quoting it: `bash eval/scripts/verify-run.sh`.

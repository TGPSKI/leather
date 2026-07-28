# SIG-triage eval — results and lessons

> ## ⚠ STATUS: figures in this package are being re-baselined
>
> Every number written here so far was produced **before** a set of harness fixes
> (`d5d8d23`, `eaf377a`, `5eeb03e`, `6295cd7`), and is therefore **provisional —
> do not publish, quote, or build an argument on it.**
>
> The disqualifying defect is that leather's queues lived in a shared `state_dir`
> that was never cleared between runs. Several runs that night were killed
> mid-flight, and a killed run leaves queue items behind for the *next* run's
> supervisor to drain — writing artifacts into that run's directory, attributed
> by issue number, invisibly. The amount of contamination cannot be bounded after
> the fact, so the numbers are treated as unusable rather than as probably-fine.
>
> Narrower defects affecting specific arms: a tool schema emitting
> `required: null` (broke every forced-tool arm, and put a malformed schema in
> front of arms that offered a tool at all), and a proxy that misfiled match
> calls under the wrong stage (voided tool telemetry, not accuracy).
>
> A full re-baseline is running on the fixed harness: every cell carries a
> `run-manifest.json`, archives its evidence under `runs/<tag>/`, and is checked
> by `verify-run.sh`. The **methodology** in these documents stands — it is what
> caught the problem. The **figures** will be replaced.

Everything measured while turning `examples/14-sig-triage` from a demo into a
gated eval: the ablation matrix, the run artifacts behind it, and the findings
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
| reproducibility | measured: ±6 rows (~2.4%) run-to-run on 250 |

## Headline

A **4B model running locally** reaches **78.0%** on this task. The 35B reaches
**86.7%** (mean of 4 runs). The same models given only the label set and no
pipeline configuration score 62.0% and 70.4% — so **configuration is worth ~16
points, identically at both scales**.

That last part is the load-bearing result: the discipline transfers *down* to the
small model rather than being something only a large model can exploit.

## Contents

| file | what's in it |
|---|---|
| [ablation-matrix.md](ablation-matrix.md) | the A/B/C/C′/D/E/E′ × {35B, 4B} table, with what each cell isolates |
| [lessons-leather.md](lessons-leather.md) | framework findings: silent config no-ops, queue concurrency, tool-choice, routing limits |
| [lessons-vllm-models.md](lessons-vllm-models.md) | model and server findings: uncertainty signals, `tool_choice` hangs, thinking, the scale gap |
| [lessons-eval-methodology.md](lessons-eval-methodology.md) | how to run an eval that can be trusted — null bands, tiers, replication, provenance |
| `runs/` | raw per-cell predictions and reports |

## Reading these numbers

Three rules were adopted the hard way and apply to every number here.

**Every published figure is a mean with its spread.** One config replicated four
times scored 84.8 / 86.0 / 87.6 / 88.4. A single run of it would have supported
any claim between "barely works" and "88.4%".

**Verdicts are against a measured null band, not against zero.** Two repeat runs
of an unchanged config differed by 6 rows. Anything inside ±6 is reported
UNRESOLVED — not "no change", but *the experiment could not tell*.

**The gate is failing, and that is stated rather than omitted.** `core-recall` at
≥90% is missed by sig-api-machinery, sig-network, sig-node and sig-storage in
every configuration measured. Accuracy, abstention and macro-recall pass. No
threshold was lowered to make anything green.

## Reproducing

```bash
cd examples/14-sig-triage
LEATHER_MODEL=... LEATHER_LLM_ENDPOINT=... LOGPROB=1 bash eval/run-eval.sh
```

`LOGPROB=1` routes through `eval/scripts/logprob-proxy.py`, which records
token-level uncertainty margins and — the counter that mattered most here —
whether each request actually carried a `tools` array.

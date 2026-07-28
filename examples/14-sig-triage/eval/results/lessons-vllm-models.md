# Lessons: vLLM, Qwen3-35B-A3B and Qwen3-4B-Instruct

> **Figures provisional.** Every number below predates the harness fixes of
> `d5d8d23`/`eaf377a`/`5eeb03e` and is being re-baselined — see the status note in
> [README.md](README.md). The findings and methodology stand; the specific
> values will be replaced from verified runs.


Model- and server-level findings. Scope is stated on each one, because most were
measured on a single rig and the failure mode of this whole exercise was
generalizing too early.

**Rigs.** 35B: `qwen36-35b-a3b-nvfp4` (NVFP4, MoE) on vLLM `0.23.1rc1`, flags
`--reasoning-parser qwen3 --tool-call-parser qwen3_xml --enable-auto-tool-choice
--enable-prefix-caching`, `max_model_len` 228000, `--max-num-seqs 8`. 4B:
`Qwen3-4B-Instruct-2507-AWQ`, `max_model_len` 12288, a separate host.

---

## Verbalized confidence is worthless; the logprob margin is not

Measured over 250 issues, the same forward pass, both signals side by side:

| signal | AUROC | error after escalating 10% / 20% / 30% |
|---|---|---|
| verbalized `CONFIDENCE: high\|medium\|low` | **0.48–0.51** | 16.0% / 16.5% / 14.3% |
| `sig_margin` (token logprob) | **0.66–0.71** | 12.0% / 11.0% / 9.1% |
| `commit_margin` (token logprob) | **0.64–0.73** | 11.6% / 10.0% / 9.1% |

AUROC 0.5 is a coin flip. The self-report carries **no** signal, and escalating on
it can make things worse. The model answered `high` on 97% of rows.

**Prompting harder does not fix it.** An explicit calibration protocol — name a
`RUNNER_UP`, justify `high` with a `WHY_NOT_RUNNER_UP` fact, hedge when the top
two are close — still produced `high` on 97% of rows, cost ~2.4 points of
accuracy, and the few non-`high` rows were *more* accurate than the rest. This
matches the cascade-routing literature. Both model sizes showed the same
degeneracy, so it is not a large-model artifact.

**Extracting the margin has a trap.** The token right after `SIG:` is the shared
prefix `sig`, so its margin measures *commit vs abstain*, not *which SIG*. Read as
label confidence it reports near-total confidence on every row. The discriminating
token is the first one where the top candidates stop sharing a prefix
(`-network` vs `-node`). Record both — they answer different questions and both
are useful — but route on the discriminating one.

## `tool_choice: required` can fail to terminate

Scope: the 35B rig above. Three conditions must coincide, and removing any one
fixes it:

1. the forced call is **unmotivated by the prompt** (a classification task where
   the model would rather just answer),
2. the tool declares **no parameters**,
3. **`temperature: 0`**.

Then the model opens the call and emits tab characters to `max_tokens`:

```
'<tool_call>\n<function=get_sig_reference>\n\t\t\t\t\t\t\t\t… (to the cap)'
```

The call itself is complete and parseable within ~17 tokens. At an 8192 budget
this is 41s per request producing nothing, which breaches `llm_timeout` under
concurrency and abstains an entire run (measured: 2% accuracy, 100% abstention).
Escapes: one required parameter → clean at 38 tokens; `temperature: 0.7` → clean;
an imperative prompt → clean; `auto` → never reproduces, because the model only
calls when it wants to.

Underneath it is an upstream structured-output defect — the grammar for an empty
argument object admits unbounded whitespace and greedy decoding never leaves it.
The practical guard is a token cap on forced rounds, which costs nothing because
the call is complete in the first ~17 tokens.

**Diagnostic lesson:** three successive explanations of this were wrong ("the
server is misconfigured", "reasoning models need thinking off", "`required` never
emits a stop token"), each from testing one prompt and generalizing. Dumping the
*raw tokens* — not `finish_reason`, not the parsed `tool_calls` — settled it in
one shot. **When a model does something inexplicable, look at what it actually
emitted before theorizing.**

## Thinking costs accuracy here — replicating example 11

This was already settled in `examples/11-high-volume-ci`, where disabling thinking
took a run from 323s to 70s and eliminated intermittent `max tool rounds` failures
and empty final answers. The SIG-triage A/B is a **replication on a different
task**, not a new finding, and it agrees:

`thinking: true` on the match stage: 87.6% -> 85.2%, net -6 rows (inside the +-6
null band, so not a significant accuracy change) at ~2.5-3x wall clock. The
mechanism is not noise, though: **abstention went 2.0% -> 6.8%**, and 11 of the 17
regressions were previously-correct answers becoming `unknown`. Deliberation talks
this model out of answers it had right.

Two independent tasks now point the same way for Qwen3 on this stack: leave
`thinking: false` on. Note `Qwen3-4B-Instruct-2507` has no thinking mode at all --
`enable_thinking` is accepted and silently ignored by its template -- so a two-rig
comparison is not matched on that axis.

## Reasoning models and `enable_thinking` interact with token budgets

With thinking on, the model spends its budget in the reasoning channel first. A
test capped at 400–600 tokens looks like a hard failure ("no tool call, empty
content") when it is simply still thinking. Any budget-sensitive probe on a
reasoning model needs a budget large enough to clear the thinking block, or the
result is an artifact of the cap.

## Prefix caching is worth checking

The match system prompt is byte-identical across all 250 requests (~950 tokens),
so it should be a cache hit on every request after the first. Observed hit rates
varied a lot between runs (63% steady-state, ~0.2% in one window). Worth
confirming `--enable-prefix-caching` is on and that per-request preamble isn't
varying, since prefill dominates this workload.

## The 4B is competent and format-compliant

Format compliance was the risk and it was a non-issue: 5/5 probes emitted the
required `SIG`/`RUNNER_UP`/`CONFIDENCE` lines with `finish_reason: stop` in
127–201 tokens. Prompt size 1340 tokens against a 12288 window. `top_logprobs` is
supported, so uncertainty measurement transfers.

An early truncation that looked like a format failure was our own `max_tokens:
300` cutting off a long `REASONING` line. **Check your own caps before blaming
the model.**

## Scale gap, measured

| | A (rules) | B (bare label set) | A − B |
|---|---|---|---|
| 35B | 86.7% | 70.4% | **+16.3** |
| 4B | 78.0% | 62.0% | **+16.0** |

Read this as the **configuration delta**: A is the bounded two-stage pipeline with
a controlled prompt and tools; B is the same model handed only the label set.
Configuration is worth ~16 points, and — the part worth noting — it is worth the
*same* ~16 points at both scales, so the discipline transfers down to the small
model rather than being something only a large model can exploit. That is the
thesis result: a 4B running locally reaches 78.0% on a 22-class task where the
bare model gets 62.0%.

The specific hypothesis that structure would help the *smaller* model more is
**not supported**: the 4B's deficit is a roughly constant ~8.7-point offset that
configuration does not close. B(4B) = 62% also puts a legible floor under the
whole exercise.

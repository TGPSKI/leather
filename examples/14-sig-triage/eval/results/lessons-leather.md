# Lessons: leather

> **Era note.** The mechanism and performance findings below are unaffected by
> the 2026-07-28 accuracy re-baseline (see the provenance note in
> [README.md](README.md)) — the contamination corrupted accuracy attribution,
> not wall-clock or protocol behaviour. Accuracy-bearing figures in this
> package now live in README.md and the run archives.


Carry-forward findings about the framework itself, from building and running the
SIG-triage eval. Each is something that cost time to discover and would cost the
same time again.

---

## Config values that parse, are accepted, and do nothing

Three separate keys share one failure mode, which makes it a pattern rather than
three bugs.

**`temperature` in agent frontmatter is ignored unless it is also in config.yaml.**
`parseFrontMatter` defaults to 0.7 and `resolveAgent` falls back to config only
when the agent's value is *exactly* 0. Setting `temperature: 0` in the agent alone
leaves you at 0.7 with no warning. This one is worse than a wasted afternoon: it
silently invalidates every measurement taken while you believed decoding was
greedy. A committed accuracy number in this example was produced under a
temperature the config claimed was zero. **Set it in both places, and assert it
somewhere the eval can see.** (Tracked as
[leather#56](https://github.com/TGPSKI/leather/issues/56); keep double-setting
until it closes.)

**`{{env:VAR}}` is not expanded everywhere it looks like it should be** — notably
config `model:`. The `LEATHER_MODEL` env override is the mechanism.

**`summarize_threshold` has three traps.** `0` means "summarize on *every*
message" (`used >= 0` is always true), not "off" — the intuitive disable value is
the worst possible setting. Out-of-range values pass silently: the runtime
validator only checks `TypeNumber`, so the `minimum`/`maximum` in the schema are
editor-only. And malformed values (`85%`, `0,85`) fall back to the default because
the `ParseFloat` error is swallowed.

The general lesson: **a config key that can silently do nothing is worse than one
that errors**, because it converts a configuration bug into a measurement bug. If
you add a knob, make the wrong value loud.

## Queue concurrency is real; per-item invocation makes it dead

The first harness ran one `leather workflow run` per issue and waited for it.
That made the tannery's `concurrency:` setting inert — the queue never held more
than one item, so the run was strictly serial no matter how much parallelism the
model server had.

Batch-ingest the whole corpus with `leather ingest`, then drain once with a single
`workflow run`: **~5× faster** (8.4s → 1.6s per issue at concurrency 8), and the
GPU actually saturates (`Running: 8 reqs, Waiting: 0`).

The second-order benefit was larger than the speedup. Per-item invocation had
forced an artifact race — clear the artifact directory, run one issue, poll for
the file that appears. Batch draining retires the race outright: every artifact is
kept and attributed by an `ISSUE:` line the agent copies verbatim. **Provenance
read from content beats provenance inferred from timing**, and the batch design
made the correct approach the only approach.

## Tool choice is `auto`, always

`internal/session/http_client.go` hardcodes `tool_choice: "auto"`. There is no
agent or config knob. To force a tool call for an experiment you must inject it
between leather and the server.

Two measured consequences, both counterintuitive:

- **`auto` batches, forcing serializes.** A directive prompt over five zero-arg
  tools returned *three parallel calls* in one 44-token turn under `auto`; the
  same request with `tool_choice: "required"` returned one. Forcing would make a
  `tool_rounds`-budgeted pipeline slower and more likely to run out of rounds.
- **Forcing a call the prompt does not motivate can fail to terminate.** See
  [lessons-vllm-models.md](lessons-vllm-models.md); it needs a token cap.

## An instrument that cannot change what it measures

leather has no `logprobs` knob (`ExtraBody` is populated internally only), so
uncertainty measurement went through a proxy sitting between leather and vLLM.
That turned out to be the right shape for reasons beyond necessity: the proxy also
records **whether each request actually carried a `tools` array**, which is the
only way to distinguish "the model declined to call the catalog" from "the tool
was never offered." Counting `executing tool` in the log cannot tell those apart,
and the difference is the entire shadow-catalog finding.

Rules that held up: fail open (any proxy-side error still forwards the upstream
response), and **fail closed on setup** — refuse to start if the port is already
serving. A stale proxy from an earlier run once answered the readiness probe and
served an entire eval while recording to *its* output file, leaving that run's
margins silently empty. The port check later cost four runs in a row that failed
instantly and correctly, which is exactly what it is for.

## Artifacts have no structure, and everyone re-derives it

`Artifact.Content` is an opaque string. Every consumer that wants a field writes
its own regex; this eval accumulated four across two scripts and they drifted at
least once. Anything that routes, scores, or reports on artifact content pays this
tax. Field extraction belongs in the curing, once. (Proposed as LEP-0008 S1.)

## leather can fan out but cannot choose

All four routing points decide on envelope metadata or a static queue name:
tannery routes match `source`/`event_type`/`hide_kind`; `queue_pattern` expands
only `{{hide_id}}`; `curing.output.queue` is a static name that fires on every
success; agent `outputs:` have no predicate field. Two apparent loopholes are
closed too — `hide_types` can't discriminate chained content (a chained artifact's
hide kind is stamped with the *upstream curing name*), and agents can't route
themselves (the builtin tools are `hide_next`/`hide_jump`/`hide_search` only).

`NewWorker` already takes a `Router` it ignores, commented "reserved for future
content-based output routing". Anything shaped like "send the uncertain ones for a
second opinion" has to be done outside leather today. (LEP-0008.)

## Detectors with nothing on the other end

`check-taxonomy-currency.sh` hashes upstream, exits 10 on drift, and drops a
marker file for "a wrapping leather cron/agent" that opens a catalog PR. That
agent was never built. The signal had been going nowhere — which is also why a
regex bug in `gen-taxonomy.sh` that silently disabled the entire currency loop
went unnoticed until this eval tripped over it.

**A half-built loop looks exactly like a working one from the outside.** If a
component emits a signal nobody consumes, either build the consumer or make the
absence loud.

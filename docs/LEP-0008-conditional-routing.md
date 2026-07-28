# LEP-0008 — Conditional Routing and the Uncertainty Connector

- **Status:** Proposed
- **Target:** leather v0.7.0
- **Depends on:** nothing (runtime-only; LEP-0006/0007 are the eval subsystem and
  are independent)
- **Anchors:** the 14-sig-triage two-pass adjudicator, which exists today only as
  harness code (`examples/14-sig-triage/eval/scripts/adjudicate-pass.sh`) because
  leather cannot express it; and the measured finding that the signal such a
  feature would naturally route on is worthless.

---

## 0. TL;DR

leather can fan out but it cannot choose. Every routing decision in the runtime
is made on envelope metadata (source, event type, hide kind) or is a static queue
name that fires unconditionally. There is no way to say *send the uncertain ones
to a second opinion and let the confident ones through*, which is the single most
useful shape in a bounded-context pipeline: it is how you spend more compute
where it pays and nowhere else.

This LEP adds that in two halves, and the order matters. **Part B** is the
routing: strict, declarative predicates over parsed artifact fields, plus an
agent-selected mode where the agent names a destination from a closed allow-list.
**Part A** is the connector: a real uncertainty signal (token-level logprob
margin) attached to the artifact as structured metadata.

Part B without Part A is an attractive nuisance. A router that can only see
artifact *text* can only route on what the model *says* about its own confidence,
and on the one corpus where this has been measured carefully, verbalized
confidence separates right from wrong at **AUROC 0.483** — indistinguishable from
a coin flip — while the logprob margin from the same forward pass scores
meaningfully better. (2026-07-28 correction, from six repeat draws of one
identical config: the margin's AUROC wobbles 0.55–0.68 across draws, mean
**≈0.62 ± 0.05** — the 0.71–0.73 in earlier drafts was a single-draw upper
tail. The margin-vs-self-report gap survives decisively; the absolute payoff
estimates in this LEP should be re-derived at 0.62 before implementation, and
the strongest new argument for the two-pass shape is independent of AUROC:
across cells, ~50–63% of misses carry gold as the artifact's RUNNER_UP — a
large recoverable pool reachable only by a second look.) Shipping the router
alone would still hand users a well-built mechanism aimed at a signal that does
not work.

---

## 1. Motivation

### 1.1 The routing gap

There are four places leather makes a routing decision. None can see content:

| Where | Decides on | Conditional? |
|---|---|---|
| `tannery.routes[].match` (`internal/curing/router.go:21`) | `source`, `event_type`, `hide_kind` | Envelope metadata only |
| `queue_pattern` (`internal/cli/api_tannery.go:481`) | expands `{{hide_id}}` | One queue per *event*, not per content |
| `curing.output.queue` (`internal/curing/worker.go:1075`) | — | Static name, fires on every success |
| agent frontmatter `outputs:` (`internal/runner/runner.go:1202`) | — | Static name; `OutputRoute` has no predicate field |

Two apparent loopholes are closed too. `hide_types` looks like a content filter,
but `dispatchQueue` stamps a chained artifact's hide kind as the *upstream curing
name*, so every artifact from one curing arrives with the same kind whatever it
says. And an agent cannot route itself: the builtin tool set is `hide_next`,
`hide_jump`, `hide_search`, with no enqueue.

The gap is already acknowledged in the code. `NewWorker` takes a `Router` it
never uses (`internal/curing/worker.go:157`):

```go
_ *Router, // reserved for future content-based output routing
```

### 1.2 The signal gap, and why it constrains the design

The 14-sig-triage pipeline asks its match agent to self-report `CONFIDENCE: high
| medium | low`. Measured over 250 issues, that field is `high` 97% of the time
and scores **AUROC 0.483** at separating correct from incorrect — worse than
chance, and the agent prompt now carries a standing warning not to route on it.
Prompting harder for calibration was tried and did not fix it.

The token-level margin at the deciding token, read off the *same* forward pass at
no extra inference cost, scores **AUROC ≈0.62 ± 0.05** on the same rows
(repeat-corrected 2026-07-28; single draws ranged to 0.73). That signal
does not currently exist anywhere in leather: it is captured today by a sidecar
HTTP proxy that sits between leather and vLLM injecting `logprobs: true`, because
leather has no knob for it and no place to put the answer.

So the honest conclusion is not "add conditional routing." It is: **add
conditional routing and give it something worth routing on**, or the feature will
be used to route on the self-report because that is the only thing in the
artifact.

### 1.3 Why now

The two-pass adjudicator is a concrete, measured need that the runtime cannot
express. On this corpus, when the pipeline is wrong the correct answer is the
runner-up 50–71% of the time — a large recoverable pool, reachable only by
sending the uncertain minority for a second, more expensive opinion. Implemented
in the harness it is ~200 lines of bash and Python that re-reads predictions off
disk and re-ingests a hand-picked subset. That code is a workaround for a missing
primitive, and it is not shippable as a pattern: production tanneries have no
harness to do it for them.

**Non-motivation.** This is not a workflow engine, a DAG language, or a rules
engine. It is one predicate, evaluated once, at the point where an artifact
already chooses a queue.

---

## 2. Design principles

- **Strict over expressive.** Comparisons are typed and enumerated (`eq`, `in`,
  `lt`, …). No expression language, no embedded scripting, no user-supplied code
  on the hot path. leather gates docs with a diff and config with a schema for
  the same reason: a predicate you can read is a predicate you can review.
- **Fail closed, to the default route.** A missing field, an absent signal, an
  unparseable artifact or an unknown destination never silently takes the
  interesting branch. It takes the default branch and increments a counter.
- **The model proposes, config disposes.** Agent-selected routing is real, but
  the set of reachable destinations is declared in config. A model can pick from
  the ballot; it cannot write the ballot.
- **Measure the signal before you route on it.** A routing feature is only as
  good as the number it compares. This LEP ships the connector precisely so that
  users are not forced to route on a self-report that has been measured at chance.
- **Additive and off by default.** Every curing that exists today keeps its
  current behaviour with no config change.

---

## 3. Concepts and vocabulary

- **Field** — a named string extracted from artifact content by a declared
  line-prefix rule. `SIG: sig-network` → field `sig` = `"sig-network"`.
- **Signal** — a named number attached to an artifact by the runtime rather than
  the model. The logprob margin is the first one.
- **Predicate** — an ordered, typed condition over fields and signals.
- **Route** — a predicate plus a destination. First match wins.
- **Ballot** — the closed allow-list of destinations an agent may name in
  agent-selected mode.
- **Escalation** — a route that sends a minority of items to a more expensive
  path. **Coverage** is the fraction escalated; **compute multiplier** is the
  resulting increase in model calls.
- **Hop** — one agent-selected transition. Bounded, because agent-selected
  routing is the first construct in leather that can make a cycle.

---

## 4. Part A — the uncertainty connector

### 4.1 What is captured

For providers exposing an OpenAI-compatible `logprobs` field, the runtime
requests `logprobs: true, top_logprobs: N` (default N=5) and derives per-response
signals from the returned token list.

### 4.2 The commit-vs-discriminating token problem

The naive implementation — "take the margin at the first token after the anchor"
— is wrong, and wrong in a way that looks fine. In the SIG case every valid
answer begins with the token `sig`, so the first token after `SIG:` measures
*whether the model committed to any SIG at all* versus abstaining. It says
nothing about *which* SIG, which is the decision being routed on. The first
implementation of this made exactly that mistake and produced a plausible,
useless column.

Both are worth having, and they answer different questions:

- **`commit_margin`** — the margin at the anchor token: confidence that an answer
  exists at all. Routes abstention.
- **`<field>_margin`** — the margin at the first token where the top alternatives
  stop sharing a prefix with the chosen token: confidence in *which* answer.
  Routes escalation.

The extraction rule is therefore: from the anchor, advance while the top-2
candidate tokens share a leading prefix; the first token at which they diverge is
the discriminating token.

### 4.3 Declaration

```yaml
# curing yaml
signals:
  commit_margin: {anchor: "SIG:"}
  sig_margin:    {anchor: "SIG:", discriminating: true}
```

### 4.4 Where the value lives

Signals attach to `model.Artifact` as structured metadata, not as text appended
to content:

```go
type Artifact struct {
    // ...
    Fields  map[string]string   // parsed from Content by curing `parse:` rules
    Signals map[string]float64  // attached by the runtime (logprob margins, …)
}
```

Keeping them off the content string matters: content is what downstream agents
read, and padding it with telemetry changes the thing being measured.

### 4.5 Degradation

Providers without logprob support (and any request where the field is absent)
yield no signal. A predicate referencing an absent signal is **false**, logged
once per curing per process, and falls through to the default route. It must
never evaluate as "uncertain" — a provider swap would silently escalate 100% of
traffic and multiply the bill.

### 4.6 Honesty about what is known

The AUROC figures above are **one model, one corpus, one task** (a 35B MoE on 250
Kubernetes issues, 24 classes). The claim this LEP rests on is the *ordering* —
margin beats self-report, decisively and reproducibly on that corpus — not the
absolute numbers, and certainly not a portable threshold. Threshold selection is
a per-deployment calibration, which is why §6.3 makes coverage observable rather
than shipping a default cutoff.

---

## 5. Part B — conditional routing

### 5.1 The missing primitive: artifacts have no structure

`Artifact.Content` is an opaque string. Every consumer that wants a field today
writes its own regex — the sig-triage eval has four such regexes across two
scripts, and they have drifted from each other at least once. Routing on content
requires this to be solved first, so it is stage one and it is useful alone:

```yaml
# curing yaml
parse:
  fields:
    sig:        {line: "SIG:"}
    runner_up:  {line: "RUNNER_UP:"}
    confidence: {line: "CONFIDENCE:", optional: true}
```

Semantics: `line:` matches at the start of a line, takes the remainder, trims it.
A declared non-optional field that is absent is a **parse failure** — the artifact
is still written (the success boundary does not move), the failure is counted, and
routing takes `on_parse_error:` if declared, otherwise the default route.

### 5.2 Predicate routes

```yaml
output:
  routes:
    - name: escalate
      when:
        all:
          - {signal: sig_margin, lt: 0.35}
          - {field: runner_up, not_in: [none, unknown]}
      queue: adjudicate-in
    - name: default          # no `when:` — the mandatory catch-all, must be last
      queue: label-in
  on_parse_error: dlq
```

Rules:

- **First match wins**, in file order. Exactly one route may omit `when:`, and it
  must be last. A route list with no catch-all is a load-time error, not a
  runtime surprise.
- **Typed comparisons only.** Fields (strings): `eq`, `ne`, `in`, `not_in`,
  `matches` (RE2, anchored, compiled at load). Signals (numbers): `lt`, `lte`,
  `gt`, `gte`. Combinators: `all`, `any`, `not`. A field compared with `lt`, or a
  signal with `in`, is a load-time type error.
- **No side effects.** Predicate evaluation cannot call a tool, read a file, or
  reach the network.
- **Backwards compatible.** `output: {queue: x}` keeps working and is exactly
  equivalent to a single default route.

### 5.3 Agent-selected routing

Some decisions genuinely belong to the model — "this needs a human," "this is a
duplicate, stop here." The agent names a destination; config constrains which
names are legal.

```yaml
output:
  agent_selectable:
    field: next          # artifact field holding the destination
    allow: [adjudicate-in, escalate-human, label-in]
    default: label-in    # used when absent, unparseable, or not in `allow`
  max_hops: 3
```

The safety properties are the whole point:

- The ballot is **closed**. A name outside `allow` is discarded and counted, not
  created. Prompt injection cannot invent a destination, only choose among ones
  an operator already wrote down.
- **Hops are bounded.** Agent-selected routing is the first construct in leather
  where A can send to B and B back to A. Each queue item carries a hop counter;
  exceeding `max_hops` routes to the DLQ with the chain recorded.
- Predicate routes and agent-selected routing **compose**: predicates are
  evaluated first, and `agent_selectable` applies only if no predicate route
  matched. Operator intent outranks model preference.

### 5.4 Where it lands

`Worker.process` step 10 (`internal/curing/worker.go:1067`), the "Output routing —
best-effort" block, and the `Router` parameter already threaded into `NewWorker`
and ignored. Routing stays strictly *after* the artifact-write success boundary:
a routing failure must never fail a run whose work is already done.

---

## 6. Observability

Escalation is a compute multiplier, and a multiplier you cannot see is a bill you
cannot predict. Per curing, per route:

- `routed_total{route}` — items taking each route.
- **Coverage** — the escalating routes' share of items.
- **Compute multiplier** — model calls on the decision path divided by items.
- `parse_failures_total{field}`, `signal_absent_total{signal}`,
  `ballot_rejected_total`, `hops_exceeded_total`.

The last four are the ones that catch a silently-broken deployment: a predicate
that never fires because the field renamed looks identical, from the outside, to
a predicate that correctly never fires.

---

## 7. Staging

Each stage is independently shippable, independently useful, and independently
testable.

| Stage | Content | Depends on |
|---|---|---|
| **S1** | `parse.fields` → `Artifact.Fields`; parse-failure counters | — |
| **S2** | logprob capture → `Artifact.Signals`; `commit`/discriminating margins; degradation rules | — |
| **S3** | `output.routes[].when`, typed predicates, mandatory catch-all, load-time validation | S1 (S2 for signal predicates) |
| **S4** | `agent_selectable` with closed ballot + `max_hops` | S1, S3 |
| **S5** | Route/coverage/multiplier metrics and `leather doctor` surfacing | S3 |

S1 and S2 are worth shipping even if S3 never does: S1 removes duplicated regexes
across every consumer of an artifact, and S2 makes uncertainty measurable for
anyone building an eval.

**The ordering rail:** S3 must not ship before S2. A router released on top of S1
alone can only compare text fields, which in practice means routing on a model's
verbalized confidence — the signal measured here at AUROC 0.483. Shipping the two
together is what makes the feature honest.

---

## 8. Worked example: the two-pass adjudicator

Today, in the harness (`eval/scripts/adjudicate-pass.sh`): read pass-1
predictions from disk, join them against per-issue margins recorded by a sidecar
proxy, sort by margin, take the lowest 20%, re-read the analyze artifacts,
compose a two-candidate ballot per issue, ingest each into `adjudicate-in`, drain,
attribute results back by an `ISSUE:` line, merge, re-score. Roughly 200 lines,
and every one of them is scaffolding around the missing primitive.

After this LEP, in `match.curing.yaml`:

```yaml
parse:
  fields:
    sig:       {line: "SIG:"}
    runner_up: {line: "RUNNER_UP:"}
signals:
  sig_margin: {anchor: "SIG:", discriminating: true}
output:
  routes:
    - name: escalate
      when:
        all:
          - {signal: sig_margin, lt: 0.35}
          - {field: runner_up, not_in: [none, unknown]}
      queue: adjudicate-in
    - name: default
      queue: label-in
```

Two details the harness version has to solve that the config version must not
lose:

1. **Candidate ordering.** Presenting the first pass's top choice as candidate 1
   every time lets a tie-breaker score well by simply agreeing with position 1,
   and you would never detect it. The harness orders by issue-number parity —
   deterministic across re-runs, balanced across positions. A config-driven
   version needs the equivalent, or the adjudicator's measured value is
   contaminated by position bias.
2. **Fail-safe merge.** A tie-break that is missing, self-inconsistent,
   off-ballot or explicitly `neither` leaves the first-pass answer standing. The
   second opinion can improve the result or decline; it cannot destroy it. Each
   outcome is counted separately, because a pass that "helps" while silently
   declining 40% of its cases is not a result.

---

## 9. Non-goals and open questions

**Non-goals.** Not a DAG/workflow language — this is one predicate at one
existing decision point. Not retry or error routing (`max_attempts` and the DLQ
own that). Not cross-tannery routing. Not model-based routing: no classifier
chooses the route, only declared comparisons over declared values.

**Open questions.**

- **Threshold portability.** The 0.35 above is illustrative. Whether a margin
  cutoff transfers across models, quantizations or task shapes is unmeasured; the
  4B-vs-35B transfer run on this corpus is the first available data point.
- **Cost of `top_logprobs`.** Assumed negligible because the values come from the
  same forward pass, but the serialization overhead at N=5 over a long completion
  has not been measured. It should be, before S2 defaults to on for any provider.
- **Structured output instead of line parsing.** If artifacts gain a JSON mode,
  `parse.fields` becomes redundant for those curings. Worth deciding before S1
  ossifies the line-prefix rule as the way to get structure out of an artifact.
- **Should abstention route separately?** `commit_margin` makes "the model
  declined" distinguishable from "the model guessed." Whether that deserves its
  own route or is just another predicate is unresolved.
- **Signal provenance in the artifact record.** If a margin influenced a routing
  decision, the artifact should probably record which threshold it was compared
  against, so a stored artifact explains its own path.

---

## 10. Rollout

- **Phase 1 — S1 + S2.** Structure and signal. No routing behaviour changes; both
  are immediately useful to eval harnesses, which is where they can be validated
  against known-good numbers before anything depends on them.
- **Phase 2 — S3.** Predicate routes, off by default, with the existing
  `output.queue` form preserved verbatim.
- **Phase 3 — S4 + S5.** Agent-selected routing and the metrics that make
  escalation cost visible.

**Compatibility.** Every change is additive. `output: {queue: x}` and
`output: {notify: y}` keep their exact current semantics; a curing with no
`parse:`, `signals:` or `routes:` block behaves identically to today.

**Validation.** The 14-sig-triage adjudicator is the acceptance test: the config
in §8 must reproduce the harness script's escalation set exactly, on the same
corpus, with the same recorded margins. If it does not, the primitive is wrong,
not the harness.

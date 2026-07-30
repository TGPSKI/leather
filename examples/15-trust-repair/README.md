# 15 — trust-repair: state-changing repair under a conjunction oracle

**Status: pre-pilot scaffold.** The graders are validated (bidirectional
selftest GREEN in skeptic), the plumbing is smoke-tested end to end with
scripted patchers, and no model has been run yet. The 24-task GH Actions
pilot (dynamic-range probe) is the next gate; the full corpus and any
confirmatory claims wait on it.

## What this is

The second demonstration workload for the harness-auditing protocol built in
[`14-sig-triage`](../14-sig-triage/): same method — frozen inputs, arms
varied one mechanism at a time, per-instance paired verdicts — pointed at a
**materially different task class**:

| axis | 14-sig-triage | 15-trust-repair |
|---|---|---|
| output | prediction | state-changing patch |
| space | 22-way classification | open-ended generation |
| input | fixed text | repository exploration |
| scoring | exact / accept-set | executable properties |
| side effects | none | modifies real artifacts |

One instance = a small synthetic repository containing exactly one known
trust-boundary defect (mutable action refs, privileged `pull_request_target`
checkout, …). The agent gets the repo and file tools; it must produce a
patch that removes the defect **and** keeps the repository doing its job.

## The oracle lives in skeptic, on purpose

Fixtures and graders are in the [skeptic](https://github.com/TGPSKI/skeptic)
repo under `testdata/repair/` — a separately maintained scanner with its own
CI, not artifacts built to pass this patcher. Every run manifest here pins
`skeptic_commit` + `fixture_tree_sha`, so fixture drift is corpus drift and
shows up in provenance.

A patch passes only on the full conjunction — scanner-clean is **never**
the sole oracle (deleting the workflow, adding a waiver, or gutting the
steps all fake it):

```
targeted findings removed          (scanner)
AND exploit path closed            (artifact property, scanner-independent)
AND intended behavior preserved    (held-out behavior test)
AND no new >= severity finding     (scanner, vs committed baseline)
AND no suppression/waiver added    (file diff; waivers are also inert)
```

The graders themselves are gated: `testdata/repair/tools/selftest.sh` must
stay GREEN — every scripted compliant patcher passes, every scripted
violator (suppressor, waiver-adder, workflow-deleter, trigger-remover,
behavior-breaker) fails, and each unpatched fixture fails. No model output
is scored by an instrument that hasn't passed that test.

## Arms (pilot: B, R, E, V, V2)

Each arm is its own committed file under `eval/arms/` — the runner copies it
to `agents/repair.agent.md` and sha-checks the copy.

| arm | treatment |
|---|---|
| B | task statement + repository only |
| R | + the explicit security invariant (rule titles/descriptions) |
| E | + full evidence: file, line, matched content, why it's unsafe |
| V | + verification tools (scanner, repo tests) available after editing |
| V2 | verification structurally required before completion |

Signs are **not** expected to match sig-triage — divergence is the result.
The verify tools exposed to V/V2 are the public instruments (scanner, `make
test`), never the held-out behavior/exploit tests.

## Running

```bash
# plumbing test, no model: apply a scripted patcher and score it
REPAIR_SCRIPTED=$SKEPTIC_ROOT/testdata/repair/gh-actions/mutable-action-pin/selftest/saint.sh \
  bash eval/run-instance.sh B gh-actions/mutable-action-pin/v1 smoke

# model run (pilot, once T1 GPU idle allows)
LEATHER_MODEL=... LEATHER_LLM_ENDPOINT=... \
  bash eval/run-instance.sh E gh-actions/unsafe-prt-checkout/v1
```

Each run archives to `eval/results/runs/<tag>/`: `verdict.json` (per-check
booleans + pass), `patch.diff`, `task-input.txt` (exactly what the arm saw),
and `run-manifest.json` (arm/agent/task shas, skeptic + fixture + leather
pins, model, endpoint).

## Design of record

Decisions and their reasons (why repair and not adherence-suite, why the
fixture/oracle split, the pilot's declared purpose as a dynamic-range
probe, registration discipline for the confirmatory family) are recorded in
the project planning notes; the operational summary: pilot families bracket
difficulty (mechanical pin vs semantic trust-flow), the pilot verdict is
go/no-go on dynamic range before any corpus investment, and the
confirmatory family gets its own pre-registration frozen by commit before
any confirmatory run — same protocol as 14's battery.

# Verification: no load-bearing claim from assumed behaviour

Every conclusion in this package depends on the run having actually done what its
configuration said. Twice in this project that assumption was false while the
accuracy numbers looked entirely normal:

- A proxy misfiled every `match` call as `analyze`, so an arm reported
  "tool offered on 0/250" for a run whose tool fired on every issue.
- Two rigs shared one queue store, so each supervisor dequeued items whose hides
  lived in the other's directory. One rig looked broken; the other looked
  healthy and was equally corrupt.

Neither was visible in the score. So the rule for this package is:

> **A claim about runtime behaviour is only as good as the artifact that proves
> it. Configuration is an intention, not evidence.**

`bash eval/scripts/verify-run.sh [suffix]` enforces it mechanically.

## The claim ledger

Each load-bearing claim, the assumption it would otherwise rest on, and the
artifact that settles it.

| claim | naive basis | artifact that proves it |
|---|---|---|
| the catalog tool was offered but never called | "the log has no `executing tool`" | **two sources, which must agree:** proxy `rounds/issue == 1.00` (a call forces a second round) *and* `executing tool` count of 0 in `run.log` |
| the forced lookup actually fired | "`FORCE_TOOL=1` was set" | `executing tool ... tool=lookup_sig` count in `run.log`, compared against the issue count |
| the *seeded* index was in use | "the runner exported `SIG_INDEX`" | `SIG_INDEX` in the live tool process (`/proc/<pid>/environ`), and post-run the index path + sha in `run-manifest.json` |
| the `NOT_MATCH` boundaries reached the model | "the index contained them" | the model **citing** them in its own artifact text ("the NOT_MATCH exclusion rules out sig-api-machinery") |
| decoding was greedy | "`temperature: 0` is in the agent and the config" | `temperature` recorded per request by the proxy, from the request body |
| the rows are the model's, not the harness's | "the run completed" | attribution count, `hide missing` count, stage-failure count |
| the two rigs did not interfere | "each had its own state dir" | `hide missing == 0` on **both** rigs, plus distinct `SIG_INDEX` per live tool process |

## Why two sources for the tool claim

The two available sources fail in opposite directions, so neither alone is
sufficient:

- **leather's `run.log` proves a positive.** `executing tool tool=lookup_sig` is
  unambiguous evidence a call ran. It cannot prove a negative: a zero count
  cannot distinguish "the model declined" from "the tool was never offered."
- **The proxy proves a negative.** A tool call forces a second round, so
  `1.00 rounds/issue` *is* the evidence no call happened — and the request body
  separately records whether `tools` was present at all. But its stage
  attribution has been wrong before, which is why it is never trusted alone.

`verify-run.sh` fails on **contradiction** between them, not on either value.
Agreement is the check; a disagreement means one instrument is broken and the
claim is void until it's known which.

## Reporting rules that follow from this

**An unverifiable claim is reported as unverifiable, not as a result.** The
verifier emits `SKIP` — never a silent pass — when the artifact is missing. An
absent artifact is not evidence of correctness.

**Cells whose evidence was overwritten get re-run, not rationalised.** Each cell
truncates `logprobs.jsonl` and `run.log` at start, so a cell's evidence is gone
once the next begins. Any cell whose behaviour was not verified while it was live
is re-run rather than argued for from a sibling cell's behaviour.

**A run that cannot be identified afterwards cannot support a published number.**
`run-manifest.json` records model, endpoint, agent prompt sha, index path and
sha, corpus sha and git commit. Without it, "which prompt produced 77.2%?" has no
answer three cells later.

## Running it

```bash
bash eval/scripts/verify-run.sh              # default single-run state
bash eval/scripts/verify-run.sh -35b         # a parallel, suffixed run
EXPECT_TOOL=lookup_sig EXPECT_CALLS=250 \
  bash eval/scripts/verify-run.sh -4b        # assert an arm's tool actually ran
```

Exit 0 only when every check passes. Run it before a number leaves this
directory.

# eval — full SIG-triage evaluation harness

Fetches real sig-labeled `kubernetes/kubernetes` issues, caches them, **separates
the labels from the issues** (and scrubs label leakage), runs the `analyze`->`match`
pipeline over all of them on your local model, and emits a full classification
report gated on thresholds. Reuses the exact pipeline agents — no `label` stage,
no side effects. Runs entirely on your local endpoint; cheap enough to gate every
catalog change.

## Pipeline

```
scripts/fetch-eval-corpus.sh   GitHub search API -> cache/ (raw) -> split:
                             corpus.jsonl  {number,repo,title,body}   (BLIND, scrubbed)
                             gold.jsonl    {number,sig,accept[]}       (answer key)
run-eval.sh                blind corpus -> analyze->match on your model -> predictions.jsonl
sigeval.go                 gold + predictions -> classification report + PASS/FAIL gate
```

Labels never touch the model: `run-eval.sh` reads only `corpus.jsonl`; scoring
joins `predictions.jsonl` to `gold.jsonl` afterward.

## 1. Build the corpus

```
PER_SIG=15 bash eval/scripts/fetch-eval-corpus.sh          # unauth: ~10 search reqs
GH_TOKEN=ghp_... PER_SIG=15 bash eval/scripts/fetch-eval-corpus.sh   # higher rate limit
```

One search request per SIG (balanced across 10 SIGs), deduped by issue number.
Cached in `cache/`; re-running is free unless `REFRESH=1`. Prints the gold
distribution and multi-SIG count.

**Label hygiene (important):** real issues carry `/sig <name>` prow commands and
`sig/<name>` mentions in the body — ~46% of a raw pull leaks its own answer. The
fetcher **redacts** those (`/sig ...`, `sig/...`, `sig-...`, `SIG <Name>`) from the
blind corpus, so the model must classify from technical content, not the label.
Verify: `grep -c '\[sig-redacted\]' corpus.jsonl`.

**Multi-SIG issues** (an issue with >1 sig label) become an `accept` set in
`gold.jsonl`: any of its labels — or a low-confidence `unknown` if you allow it —
counts correct. That encodes the real ambiguity instead of punishing it.

## 2. Run + analyze against your model

```
LEATHER_MODEL=qwen3.6-4b-instruct-2507-awq \
LEATHER_LLM_ENDPOINT=http://127.0.0.1:8000/v1 \
bash eval/run-eval.sh
```

Runs each blind issue through analyze->match, writes `predictions.jsonl`, and
prints the classification report + gate. Tune the gate:

```
MIN_ACCURACY=0.80 MAX_ABSTAIN=0.20 MIN_CORE_RECALL=0.80 bash eval/run-eval.sh
```

## The report

- overall accuracy (accept-set aware), accuracy on *answered* (excl. abstentions),
  abstention rate
- per-SIG **precision / recall / f1 / support / abstain**, with macro and
  weighted-f1 averages
- top confusions (gold -> predicted)
- gate: overall accuracy >= -min-accuracy, abstention <= -max-abstain, each
  -core SIG recall >= -min-core-recall  ->  exit 0/1

Read precision and recall together: high precision + low recall on a SIG means
the model is *cautious* about it (good — pair with abstention); low precision
means it *over-assigns* that SIG (a catalog-features problem you fix in
`sigs.reference.yaml`, deterministically, not by swapping models).

## Scoring robustness

Two normalizations keep the score measuring *classification*, not *formatting*:

- **Notation folding.** The catalog name is `sig-foo`; the GitHub label is
  `sig/foo`. Smaller models sometimes emit the label form. `sigeval.go` folds
  `sig/foo` -> `sig-foo` (and trims/lowercases) on both sides, so the two denote
  the same SIG instead of scoring as a miss.
- **Content-free issues are `unknown`.** A handful of real issues are pure noise
  (e.g. body `Created by mistake`). A triage bot *should* abstain on those, so
  gold treats any issue with a `< 60`-char body as `sig: unknown` — correct
  abstention scores correct, and the junk no longer drags a core SIG's recall
  denominator below what perfect classification could reach. The rule is a body
  length threshold (junk clusters at <= 19 chars; the next real issue is 427), not
  a hardcoded issue list.

The `analyze`/`match` agents run with `thinking: false` (Qwen3 no-think): the
`match` prompt reasons in a visible `REASONING:` line before committing to `SIG:`,
which is faster and avoids long hidden traces timing out mid-run.

## Wiring

`go test ./eval/` verifies the scorer's own math. Gate PRs that touch
`sigs.reference.yaml`; drive catalog refreshes from `scripts/check-taxonomy-currency.sh`.
See `Makefile-snippet.txt`.

## Note on "100"

A 10-SIG unauthenticated pull dedups to ~90 issues (multi-SIG issues collapse
across queries). `PER_SIG=15`, adding SIGs, or a `GH_TOKEN` gets a clean 100+.

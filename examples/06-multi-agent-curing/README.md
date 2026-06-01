# 06-multi-agent-curing

Two curings chained via `output.queue` to form a **pipeline**:

```
hide  --queue:triage-in-->  [triage agent]   --output.queue:summarize-in-->  [summarize agent]   --> artifact
```

The triage curing classifies the input and emits a structured note; the
summarize curing receives that note as a hide and produces the final artifact.

## Requirements

A local OpenAI-compatible endpoint at `$LEATHER_LLM_ENDPOINT`.

## Run

```bash
make 06
```

`make 06` ingests `sample/pr-thread.txt` into the `triage-in` queue and runs
serve for ~30 seconds so both curings can fire.

After a successful run, artifacts are written under `.state/artifacts/`:

- `.state/artifacts/triage/` contains the structured INTENT/RISK/AREAS/FLAGS
  note emitted by the first curing.
- `.state/artifacts/summarize/` contains the final reviewer paragraph emitted
  by the second curing.

For paging/context debugging, run with `LEATHER_SHOW_CONTEXT=1`. The triage
run should show page facts for each hide page, successful `hide_next` calls for
later pages, and a final-output context containing page facts rather than raw
hide bodies.

## What this shows

- Chaining curings: the triage curing's `output.queue` enqueues the agent
  response as a new hide on `summarize-in`.
- Hide paging: large inputs are delivered as cut pages, then compacted into
  page facts before the final triage output.
- Multiple `queues` with independent concurrency and depth limits.
- `--curing` and `--queue` flags on `leather ingest` for explicit routing.

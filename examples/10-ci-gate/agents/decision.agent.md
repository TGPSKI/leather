---
name: decision
---

You receive analysis blocks from three parallel agents, each delimited by
"--- ANALYSIS N (from: <agent>) ---". The system runs STT/TTS evals: 30-60 min, high cost.

FULL_EVAL if any CONCERN_PATHS touch evals/, models/, datasets/, inference/, or api/,
or any SIGNAL is: model-weights, decoder-params, eval-baseline, eval-script,
data-pipeline, inference-path, api-surface.
SKIP if all signals are: docs-only, ci-config, dependency-bump, formatting-only.
When in doubt: FULL_EVAL.

Copy PR_NUMBER, REPO, SHA verbatim from ANALYSIS 1 and write:

PR_NUMBER: <number>
REPO:      <full_name>
SHA:       <sha>
Decision:  FULL_EVAL | SKIP
Rationale: <2-3 sentences citing specific files or signals>
Files of concern:
  <filename>  +<add> -<del>  -- <why>
  (or "none")

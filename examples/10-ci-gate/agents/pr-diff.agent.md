---
name: pr-diff
skills: [github-read]
tool_rounds: 3
---

You receive a GitHub pull_request webhook payload. Extract PR_NUMBER and REPO, call get_pr_diff,
then write:

LOGIC_CHANGES:
  <one line per meaningful change — what shifted, not line counts>
SIGNALS: <comma-separated: model-weights, decoder-params, eval-baseline, eval-script,
          data-pipeline, inference-path, api-surface, docs-only,
          ci-config, dependency-bump, formatting-only>

The diff may be truncated; reason from what is visible. No extra text.

---
name: pr-comments
skills: [github-actions]
tool_rounds: 5
thinking: false
completion_reserve: 768
---

You receive a CI gate decision report. Extract PR_NUMBER, REPO, SHA, Decision, and Rationale.

If FULL_EVAL: call post_pr_comment(pr_number=<PR_NUMBER>, repo=<REPO>, body="/test-<SHA[0:8]>\n\n<Rationale>"),
               then add_pr_label(pr_number=<PR_NUMBER>, repo=<REPO>, label="eval-requested").
If SKIP:      call post_pr_comment(pr_number=<PR_NUMBER>, repo=<REPO>, body="CI gate: SKIP\n\n<Rationale>"),
               then add_pr_label(pr_number=<PR_NUMBER>, repo=<REPO>, label="eval-skip").

Do not call post_pr_comment more than once per PR.

If Decision is ERROR, or PR_NUMBER is blank or not a plain number (placeholder
text like <number> counts as blank), make no tool calls and write only:
DONE: skipped malformed decision report

After both calls write:
DONE: posted comment and label for PR #<PR_NUMBER>

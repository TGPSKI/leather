---
name: label
skills: [sig-apply]
tool_rounds: 2
---

You receive a match note (ISSUE, REPO, SIG, LABEL). Extract ISSUE, REPO, SIG.

If SIG is not `unknown`: call apply_sig(issue=<ISSUE>, repo=<REPO>, sig=<SIG>).
If SIG is `unknown`: do not call any tool.

After the call write exactly these lines:

ISSUE: <ISSUE>
ACTION: <first line of the tool output verbatim, or "skipped-unknown">

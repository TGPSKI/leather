---
name: issue-event
---

You receive a GitHub issue-event webhook payload.
Return exactly three bullets:

- Route: state this was routed as `issues` (or fallback if event type was unknown).
- Summary: one-sentence description of what happened.
- Next step: one concrete triage action.

Keep it concise and operational.

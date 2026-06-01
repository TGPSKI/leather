---
name: review-comment
---

You receive a GitHub pull request review-comment webhook payload.
Return exactly three bullets:

- Route: state this was routed as `pull_request_review_comment`.
- Signal: summarize the review feedback in one sentence.
- Action: one concrete next action for the PR author.

Keep it short and plain.

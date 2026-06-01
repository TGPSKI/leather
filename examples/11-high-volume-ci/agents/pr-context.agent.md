---
name: pr-context
---

You receive a GitHub pull_request webhook payload. Without calling any tools, write:

TITLE:   <pull_request.title>
AUTHOR:  <pull_request.user.login>
BASE:    <pull_request.base.ref>
LABELS:  <existing label names, comma-separated, or "none">
BODY_SIGNALS:
  <2-3 key phrases from the PR body signalling intent, or "none">
INTENT:  <feature|bugfix|refactor|docs|dependency|ci|unknown>
URGENCY: <high|normal|low — based on title, labels, body>

No extra text.

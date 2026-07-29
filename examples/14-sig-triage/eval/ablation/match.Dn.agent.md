---
name: match
tool_rounds: 2
thinking: false
temperature: 0
skills: [sig-catalog]
---

You receive an analysis note (ISSUE, REPO, COMPONENTS, SYMPTOMS, KEYWORDS,
SUMMARY). Pick the single SIG that OWNS the issue's concern.

Assign by the owning subsystem, not by a component that merely appears in a log
line or stack trace. The catalog SIGs are:
  sig-api-machinery
  sig-apps
  sig-architecture
  sig-auth
  sig-autoscaling
  sig-cli
  sig-cloud-provider
  sig-cluster-lifecycle
  sig-contributor-experience
  sig-docs
  sig-etcd
  sig-instrumentation
  sig-multicluster
  sig-network
  sig-node
  sig-release
  sig-scalability
  sig-scheduling
  sig-security
  sig-storage
  sig-testing
  sig-windows

Also name the SIG you considered second (RUNNER_UP). It is the input to the
top-2 adjudication step, and it is worth getting right: when this pipeline is
wrong, the correct answer is the runner-up ~70% of the time.

Copy ISSUE and REPO verbatim, then write exactly these lines (REASONING first so
you decide before committing to a SIG):

ISSUE: <number>
REPO: <owner/repo>
REASONING: <one sentence: which subsystem owns this and the signal that shows it>
SIG: <the catalog `name`, e.g. sig-network — always the dash form, never sig/...>
RUNNER_UP: <the second-most-likely catalog `name`, or `none` if nothing else fits>
CONFIDENCE: high | medium | low

Do not route on CONFIDENCE. It is retained for continuity only: measured over 250
issues it is 97% `high` and scores AUROC 0.483 at separating right from wrong —
literally no better than a coin flip. Uncertainty is taken from the token-level
logprob margin at the SIG decision instead (AUROC 0.71-0.73), which is read off
the same forward pass at no extra cost. Prompting harder for calibrated
self-reports does not fix this; it was already tried.

Use `SIG: unknown` only when the issue text is empty/content-free or nothing in
the catalog plausibly fits. Output only these lines, no extra text.

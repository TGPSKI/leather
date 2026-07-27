---
name: analyze
---

You receive the title and body of one Kubernetes GitHub issue. Extract the
technical signals that indicate which part of Kubernetes it concerns. Do not
name or guess a SIG — that is a later stage's job.

Copy ISSUE and REPO verbatim from the input, then write exactly these lines:

ISSUE: <number>
REPO: <owner/repo>
COMPONENTS: <comma-separated binaries/components named or implied: kube-proxy, kubelet, apiserver, scheduler, ...>
SYMPTOMS: <comma-separated short symptom phrases>
KEYWORDS: <comma-separated salient technical terms from the text>
SUMMARY: <one sentence, plain English>

Use `none` for any field with no signal. Work from the first cut alone; do not
ask for more input. No extra text.

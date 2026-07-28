---
name: s1-shortlist
tool_rounds: 2
thinking: false
temperature: 0
---

You receive an analysis note for one Kubernetes issue (ISSUE, REPO, COMPONENTS,
SYMPTOMS, KEYWORDS, SUMMARY). Your only job is to narrow the field. You do not
decide, and you do not see the catalog.

Name the subsystem whose behaviour is actually broken, as distinct from every
component that merely appears in a trace, then list the SIGs that could own it.

The catalog SIGs are:
  sig-api-machinery  sig-apps         sig-architecture  sig-auth
  sig-autoscaling    sig-cli          sig-cloud-provider sig-cluster-lifecycle
  sig-contributor-experience  sig-docs sig-etcd         sig-instrumentation
  sig-multicluster   sig-network      sig-node          sig-release
  sig-scalability    sig-scheduling   sig-security      sig-storage
  sig-testing        sig-windows

Copy ISSUE and REPO verbatim, then write exactly these lines and nothing else:

ISSUE: <number>
REPO: <owner/repo>
CONCERN: <one sentence: what subsystem's behaviour is wrong>
CANDIDATES: <2-3 catalog names, comma-separated, most likely first>

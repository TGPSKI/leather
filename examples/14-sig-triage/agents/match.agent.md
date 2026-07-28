---
name: match
skills: [sig-catalog]
tool_rounds: 2
thinking: false
temperature: 0
---

You receive an analysis note (ISSUE, REPO, COMPONENTS, SYMPTOMS, KEYWORDS,
SUMMARY). Call get_sig_reference once to load the SIG feature catalog, then pick
the single catalog SIG that OWNS the issue's concern.

Assign by the owning subsystem, not by a component that merely appears in a log
line or stack trace. Common mistakes to avoid:
  - HPA, VPA, cluster-autoscaler, metrics-driven scaling -> sig-autoscaling
    (these are controllers, but autoscaling owns them — not sig-apps).
  - Metrics, /metrics, Prometheus, klog/structured logging, tracing, Events API
    -> sig-instrumentation (not sig-api-machinery, even inside the apiserver).
  - kubectl, kustomize, kubeconfig, plugins/krew -> sig-cli.
  - Volumes, PV/PVC, CSI, StorageClass, mount/attach/detach, provisioning ->
    sig-storage, even when it surfaces through the kubelet volumemanager.
  - AuthN/AuthZ, RBAC, ServiceAccount, tokens, certificates/CSR, or leaked
    credentials/Authorization headers -> sig-auth, wherever they surface (kubelet,
    apiserver, admission Authorizer).
  - Workload controllers (Deployment, StatefulSet, DaemonSet, Job, CronJob) and
    rollouts -> sig-apps.
  - Apiserver core, CRDs/apiextensions, admission webhooks, watch/informers,
    versioning/conversion -> sig-api-machinery.
  - Scheduler placement/preemption/affinity/topology -> sig-scheduling; kubelet,
    CRI/runtime, cgroups, eviction, device plugins -> sig-node.

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

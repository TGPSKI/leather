---
name: match
skills: [sig-catalog]
tool_rounds: 2
thinking: false
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

Copy ISSUE and REPO verbatim, then write exactly these lines (REASONING first so
you decide before committing to a SIG):

ISSUE: <number>
REPO: <owner/repo>
REASONING: <one sentence: which subsystem owns this and the signal that shows it>
SIG: <the catalog `name`, e.g. sig-network — always the dash form, never sig/...>
CONFIDENCE: high | medium | low

Use `SIG: unknown` only when the issue text is empty/content-free or nothing in
the catalog plausibly fits. Output only these lines, no extra text.

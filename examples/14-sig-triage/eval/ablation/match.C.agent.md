---
name: match
tool_rounds: 2
thinking: false
temperature: 0
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

The SIG feature catalog follows. Match the issue's signals against it.

```yaml
# SIG feature catalog — signifying components and keywords per Kubernetes SIG.
# The `match` agent loads this via the sig-reference tool and matches issue
# signals against it. Names can be regenerated from kubernetes/community
# sigs.yaml with scripts/gen-taxonomy.sh; the `features` lists are curated here.
sigs:
  - name: sig-api-machinery
    label: sig/api-machinery
    features:
      - kube-apiserver core, aggregation layer, API server internals
      - admission controllers, validating/mutating webhooks
      - CRDs, CustomResourceDefinition, apiextensions
      - API discovery, OpenAPI, versioning, conversion
      - watch, informers, list/watch, resourceVersion
      - client-go, apimachinery, serialization (json/protobuf)
      - etcd storage layer, RESTStorage, server-side apply, field selectors

  - name: sig-apps
    label: sig/apps
    features:
      - Deployment, ReplicaSet, StatefulSet, DaemonSet
      - Job, CronJob controllers
      - rollout, rolling update, revisionHistory, controller reconciliation

  - name: sig-architecture
    label: sig/architecture
    features:
      - API conventions, deprecation policy, KEP governance
      - cross-cutting design, staging repos, code organization
      - conformance definition

  - name: sig-auth
    label: sig/auth
    features:
      - authentication (tokens, OIDC, client certs)
      - authorization, RBAC, ABAC, Node authorizer
      - ServiceAccount, bound/projected tokens
      - PodSecurity admission, SecurityContext policy
      - certificates, CSR API, kubelet cert rotation
      - audit logging, secrets encryption at rest

  - name: sig-autoscaling
    label: sig/autoscaling
    features:
      - HorizontalPodAutoscaler (HPA)
      - VerticalPodAutoscaler (VPA)
      - cluster-autoscaler
      - custom/external metrics scaling

  - name: sig-cli
    label: sig/cli
    features:
      - kubectl commands and output formatting
      - kustomize
      - kubeconfig UX, client-side apply
      - kubectl plugins, krew

  - name: sig-cloud-provider
    label: sig/cloud-provider
    features:
      - cloud-controller-manager
      - cloud provider interface (LoadBalancer, Routes, Instances, Zones)
      - out-of-tree provider migration, node lifecycle from cloud

  - name: sig-cluster-lifecycle
    label: sig/cluster-lifecycle
    features:
      - kubeadm init/join/upgrade
      - node bootstrap, TLS bootstrap, kubelet registration
      - cluster install/upgrade/downgrade, component config
      - kOps, cluster-api, kubeadm certs/etcd management

  - name: sig-contributor-experience
    label: sig/contributor-experience
    features:
      - Prow, test-infra, GitHub automation, k8s-ci-robot
      - OWNERS, membership, mentoring, community
      - devstats, contributor tooling

  - name: sig-docs
    label: sig/docs
    features:
      - kubernetes.io website content
      - reference/API docs generation
      - localization, Hugo doc builds

  - name: sig-etcd
    label: sig/etcd
    features:
      - etcd operation, upgrade, defragmentation, compaction
      - etcd client/clientv3 behaviour, leases, watch from the etcd side
      - etcd data corruption, backup/restore, cluster membership
      - etcd performance and reliability as a datastore
      # Boundary: the apiserver's storage layer ABOVE etcd (RESTStorage,
      # server-side apply, resourceVersion semantics) is sig-api-machinery.

  - name: sig-instrumentation
    label: sig/instrumentation
    features:
      - metrics (Prometheus format), /metrics endpoints, metrics-server
      - custom metrics API
      - logging (klog, structured logging), tracing (OpenTelemetry)
      - Events API, event aggregation

  - name: sig-multicluster
    label: sig/multicluster
    features:
      - Multi-Cluster Services (MCS) API, ClusterSet
      - cluster federation, KubeFed
      - cross-cluster networking and discovery

  - name: sig-network
    label: sig/network
    features:
      - kube-proxy (iptables, nftables, ipvs proxiers)
      - Service (ClusterIP, NodePort, LoadBalancer), EndpointSlice, Endpoints
      - Ingress, Gateway API
      - NetworkPolicy
      - DNS, CoreDNS, headless services
      - CNI, pod networking, dual-stack, IPv6

  - name: sig-node
    label: sig/node
    features:
      - kubelet
      - CRI, containerd/CRI-O runtime interface
      - cgroups, CPU/memory/topology manager, resource management
      - device plugins, GPUs, dynamic resource allocation (DRA)
      - pod lifecycle on node, probes, restarts, eviction
      - image pulling, node status, kubelet config

  - name: sig-release
    label: sig/release
    features:
      - release process, branch cut, semver versioning
      - build and packaging (debs/rpms, container images)
      - release-notes/changelog tooling, signing, SBOM, image promotion

  - name: sig-scalability
    label: sig/scalability
    features:
      - performance and scale tests, ClusterLoader
      - scalability thresholds, SLOs, large-cluster behavior, benchmarking

  - name: sig-scheduling
    label: sig/scheduling
    features:
      - kube-scheduler
      - scheduling framework, plugins, filter/score
      - preemption, priorities, PriorityClass
      - affinity/anti-affinity, taints/tolerations, topology spread

  - name: sig-security
    label: sig/security
    features:
      - security posture, threat modeling, hardening
      - CVE handling, security response, third-party audits
      - supply-chain security policy (posture, not release tooling)

  - name: sig-storage
    label: sig/storage
    features:
      - volumes, PersistentVolume/PersistentVolumeClaim
      - CSI drivers, in-tree to CSI migration
      - StorageClass, dynamic provisioning
      - attach/detach, mount, volume expansion, snapshots, ephemeral volumes

  - name: sig-testing
    label: sig/testing
    features:
      - e2e test framework, ginkgo/gomega mechanics
      - CI infra behavior, testgrid, flake triage
      - conformance test mechanics, kind for testing

  - name: sig-windows
    label: sig/windows
    features:
      - Windows nodes and Windows containers
      - HostProcess containers
      - Windows-specific kube-proxy/CNI/kubelet behavior
```

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

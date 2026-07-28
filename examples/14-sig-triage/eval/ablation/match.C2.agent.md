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

The SIG term index follows: one association per line, `term <TAB> MATCH|NOT_MATCH <TAB> sig`. A NOT_MATCH line rules that SIG out for that term. Match the issue's components and keywords against it.

```
abac	MATCH	sig-auth	catalog
admission controllers	MATCH	sig-api-machinery	catalog
affinity/anti-affinity	MATCH	sig-scheduling	catalog
aggregation layer	MATCH	sig-api-machinery	catalog
api conventions	MATCH	sig-architecture	catalog
api discovery	MATCH	sig-api-machinery	catalog
apiextensions	MATCH	sig-api-machinery	catalog
apimachinery	MATCH	sig-api-machinery	catalog
api server internals	MATCH	sig-api-machinery	catalog
attach/detach	MATCH	sig-storage	catalog
audit logging	MATCH	sig-auth	catalog
authentication (tokens	MATCH	sig-auth	catalog
authorization	MATCH	sig-auth	catalog
backup/restore	MATCH	sig-etcd	catalog
benchmarking	MATCH	sig-scalability	catalog
bound/projected tokens	MATCH	sig-auth	catalog
branch cut	MATCH	sig-release	catalog
build and packaging (debs/rpms	MATCH	sig-release	catalog
certificates	MATCH	sig-auth	catalog
cgroups	MATCH	sig-node	catalog
ci infra behavior	MATCH	sig-testing	catalog
client certs)	MATCH	sig-auth	catalog
client-go	MATCH	sig-api-machinery	catalog
client-side apply	MATCH	sig-cli	catalog
cloud-controller-manager	MATCH	sig-cloud-provider	catalog
cloud provider interface (loadbalancer	MATCH	sig-cloud-provider	catalog
cluster-api	MATCH	sig-cluster-lifecycle	catalog
cluster-autoscaler	MATCH	sig-autoscaling	catalog
cluster federation	MATCH	sig-multicluster	catalog
cluster install/upgrade/downgrade	MATCH	sig-cluster-lifecycle	catalog
clusterloader	MATCH	sig-scalability	catalog
cluster membership	MATCH	sig-etcd	catalog
clusterset	MATCH	sig-multicluster	catalog
cni	MATCH	sig-network	catalog
code organization	MATCH	sig-architecture	catalog
community	MATCH	sig-contributor-experience	catalog
compaction	MATCH	sig-etcd	catalog
component config	MATCH	sig-cluster-lifecycle	catalog
conformance definition	MATCH	sig-architecture	catalog
conformance test mechanics	MATCH	sig-testing	catalog
containerd/cri-o runtime interface	MATCH	sig-node	catalog
container images)	MATCH	sig-release	catalog
contributor tooling	MATCH	sig-contributor-experience	catalog
controller reconciliation	MATCH	sig-apps	catalog
conversion	MATCH	sig-api-machinery	catalog
coredns	MATCH	sig-network	catalog
cpu/memory/topology manager	MATCH	sig-node	catalog
crds	MATCH	sig-api-machinery	catalog
cri	MATCH	sig-node	catalog
cronjob controllers	MATCH	sig-apps	catalog
cross-cluster networking and discovery	MATCH	sig-multicluster	catalog
cross-cutting design	MATCH	sig-architecture	catalog
csi drivers	MATCH	sig-storage	catalog
csr api	MATCH	sig-auth	catalog
custom/external metrics scaling	MATCH	sig-autoscaling	catalog
custom metrics api	MATCH	sig-instrumentation	catalog
customresourcedefinition	MATCH	sig-api-machinery	catalog
cve handling	MATCH	sig-security	catalog
daemonset	MATCH	sig-apps	catalog
defragmentation	MATCH	sig-etcd	catalog
deployment	MATCH	sig-apps	catalog
deprecation policy	MATCH	sig-architecture	catalog
device plugins	MATCH	sig-node	catalog
devstats	MATCH	sig-contributor-experience	catalog
dns	MATCH	sig-network	catalog
dual-stack	MATCH	sig-network	catalog
dynamic provisioning	MATCH	sig-storage	catalog
dynamic resource allocation (dra)	MATCH	sig-node	catalog
e2e test framework	MATCH	sig-testing	catalog
endpointslice	MATCH	sig-network	catalog
endpoints	MATCH	sig-network	catalog
ephemeral volumes	MATCH	sig-storage	catalog
etcd client/clientv3 behaviour	MATCH	sig-etcd	catalog
etcd data corruption	MATCH	sig-etcd	catalog
etcd operation	MATCH	sig-etcd	catalog
etcd performance and reliability as a datastore	MATCH	sig-etcd	catalog
etcd storage layer	MATCH	sig-api-machinery	catalog
event aggregation	MATCH	sig-instrumentation	catalog
events api	MATCH	sig-instrumentation	catalog
eviction	MATCH	sig-node	catalog
field selectors	MATCH	sig-api-machinery	catalog
filter/score	MATCH	sig-scheduling	catalog
flake triage	MATCH	sig-testing	catalog
gateway api	MATCH	sig-network	catalog
ginkgo/gomega mechanics	MATCH	sig-testing	catalog
github automation	MATCH	sig-contributor-experience	catalog
gpus	MATCH	sig-node	catalog
hardening	MATCH	sig-security	catalog
headless services	MATCH	sig-network	catalog
horizontalpodautoscaler (hpa)	MATCH	sig-autoscaling	catalog
hostprocess containers	MATCH	sig-windows	catalog
hugo doc builds	MATCH	sig-docs	catalog
image promotion	MATCH	sig-release	catalog
image pulling	MATCH	sig-node	catalog
informers	MATCH	sig-api-machinery	catalog
ingress	MATCH	sig-network	catalog
instances	MATCH	sig-cloud-provider	catalog
in-tree to csi migration	MATCH	sig-storage	catalog
ipv6	MATCH	sig-network	catalog
ipvs proxiers)	MATCH	sig-network	catalog
job	MATCH	sig-apps	catalog
k8s-ci-robot	MATCH	sig-contributor-experience	catalog
kep governance	MATCH	sig-architecture	catalog
kind for testing	MATCH	sig-testing	catalog
kops	MATCH	sig-cluster-lifecycle	catalog
krew	MATCH	sig-cli	catalog
kubeadm certs/etcd management	MATCH	sig-cluster-lifecycle	catalog
kubeadm init/join/upgrade	MATCH	sig-cluster-lifecycle	catalog
kube-apiserver core	MATCH	sig-api-machinery	catalog
kubeconfig ux	MATCH	sig-cli	catalog
kubectl commands and output formatting	MATCH	sig-cli	catalog
kubectl plugins	MATCH	sig-cli	catalog
kubefed	MATCH	sig-multicluster	catalog
kubelet cert rotation	MATCH	sig-auth	catalog
kubelet config	MATCH	sig-node	catalog
kubelet	MATCH	sig-node	catalog
kubelet registration	MATCH	sig-cluster-lifecycle	catalog
kube-proxy (iptables	MATCH	sig-network	catalog
kubernetes.io website content	MATCH	sig-docs	catalog
kube-scheduler	MATCH	sig-scheduling	catalog
kustomize	MATCH	sig-cli	catalog
large-cluster behavior	MATCH	sig-scalability	catalog
leases	MATCH	sig-etcd	catalog
list/watch	MATCH	sig-api-machinery	catalog
loadbalancer)	MATCH	sig-network	catalog
localization	MATCH	sig-docs	catalog
logging (klog	MATCH	sig-instrumentation	catalog
membership	MATCH	sig-contributor-experience	catalog
mentoring	MATCH	sig-contributor-experience	catalog
/metrics endpoints	MATCH	sig-instrumentation	catalog
metrics (prometheus format)	MATCH	sig-instrumentation	catalog
metrics-server	MATCH	sig-instrumentation	catalog
mount	MATCH	sig-storage	catalog
multi-cluster services (mcs) api	MATCH	sig-multicluster	catalog
networkpolicy	MATCH	sig-network	catalog
nftables	MATCH	sig-network	catalog
node authorizer	MATCH	sig-auth	catalog
node bootstrap	MATCH	sig-cluster-lifecycle	catalog
node lifecycle from cloud	MATCH	sig-cloud-provider	catalog
nodeport	MATCH	sig-network	catalog
node status	MATCH	sig-node	catalog
not release tooling)	MATCH	sig-security	catalog
oidc	MATCH	sig-auth	catalog
openapi	MATCH	sig-api-machinery	catalog
out-of-tree provider migration	MATCH	sig-cloud-provider	catalog
owners	MATCH	sig-contributor-experience	catalog
performance and scale tests	MATCH	sig-scalability	catalog
persistentvolume/persistentvolumeclaim	MATCH	sig-storage	catalog
plugins	MATCH	sig-scheduling	catalog
pod lifecycle on node	MATCH	sig-node	catalog
pod networking	MATCH	sig-network	catalog
podsecurity admission	MATCH	sig-auth	catalog
preemption	MATCH	sig-scheduling	catalog
priorities	MATCH	sig-scheduling	catalog
priorityclass	MATCH	sig-scheduling	catalog
probes	MATCH	sig-node	catalog
prow	MATCH	sig-contributor-experience	catalog
rbac	MATCH	sig-auth	catalog
reference/api docs generation	MATCH	sig-docs	catalog
release-notes/changelog tooling	MATCH	sig-release	catalog
release process	MATCH	sig-release	catalog
replicaset	MATCH	sig-apps	catalog
resource management	MATCH	sig-node	catalog
resourceversion	MATCH	sig-api-machinery	catalog
restarts	MATCH	sig-node	catalog
reststorage	MATCH	sig-api-machinery	catalog
revisionhistory	MATCH	sig-apps	catalog
rolling update	MATCH	sig-apps	catalog
rollout	MATCH	sig-apps	catalog
routes	MATCH	sig-cloud-provider	catalog
sbom	MATCH	sig-release	catalog
scalability thresholds	MATCH	sig-scalability	catalog
scheduling framework	MATCH	sig-scheduling	catalog
secrets encryption at rest	MATCH	sig-auth	catalog
securitycontext policy	MATCH	sig-auth	catalog
security posture	MATCH	sig-security	catalog
security response	MATCH	sig-security	catalog
semver versioning	MATCH	sig-release	catalog
serialization (json/protobuf)	MATCH	sig-api-machinery	catalog
server-side apply	MATCH	sig-api-machinery	catalog
serviceaccount	MATCH	sig-auth	catalog
service (clusterip	MATCH	sig-network	catalog
signing	MATCH	sig-release	catalog
slos	MATCH	sig-scalability	catalog
snapshots	MATCH	sig-storage	catalog
staging repos	MATCH	sig-architecture	catalog
statefulset	MATCH	sig-apps	catalog
storageclass	MATCH	sig-storage	catalog
structured logging)	MATCH	sig-instrumentation	catalog
supply-chain security policy (posture	MATCH	sig-security	catalog
taints/tolerations	MATCH	sig-scheduling	catalog
testgrid	MATCH	sig-testing	catalog
test-infra	MATCH	sig-contributor-experience	catalog
third-party audits	MATCH	sig-security	catalog
threat modeling	MATCH	sig-security	catalog
tls bootstrap	MATCH	sig-cluster-lifecycle	catalog
topology spread	MATCH	sig-scheduling	catalog
tracing (opentelemetry)	MATCH	sig-instrumentation	catalog
upgrade	MATCH	sig-etcd	catalog
validating/mutating webhooks	MATCH	sig-api-machinery	catalog
versioning	MATCH	sig-api-machinery	catalog
verticalpodautoscaler (vpa)	MATCH	sig-autoscaling	catalog
volume expansion	MATCH	sig-storage	catalog
volumes	MATCH	sig-storage	catalog
watch from the etcd side	MATCH	sig-etcd	catalog
watch	MATCH	sig-api-machinery	catalog
windows nodes and windows containers	MATCH	sig-windows	catalog
windows-specific kube-proxy/cni/kubelet behavior	MATCH	sig-windows	catalog
zones)	MATCH	sig-cloud-provider	catalog
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

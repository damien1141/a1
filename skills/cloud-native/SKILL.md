---
name: cloud-native
description: Designs and ships cloud-native systems on Kubernetes 1.30+ across AWS, Azure, and GCP. Use for Deployments/StatefulSets/Jobs, HPA/KEDA autoscaling, NetworkPolicy, RBAC, CRDs, operators, Helm, kustomize, service mesh (Istio/Linkerd), multi-region DR, and cloud service selection. Produces validated manifests, Helm charts, and decision matrices with cost, RTO/RPO, and lock-in tradeoffs.
license: MIT
metadata:
  author: super-skills
  version: "2.0.0"
  domain: devops
  triggers: Kubernetes, K8s, kubectl, Helm, kustomize, KEDA, HPA, StatefulSet, NetworkPolicy, RBAC, CRD, operator, service mesh, Istio, Linkerd, ArgoCD, Flux, GitOps, EKS, AKS, GKE, multi-region, disaster recovery, cloud architecture, Well-Architected
  role: specialist
  scope: implementation
  output-format: code
  related-skills: infrastructure-as-code, sre-reliability, system-architecture, database-pro
---

# Cloud-Native

## When to Use

- Deploying containerized workloads to Kubernetes 1.30+ (Deployments, StatefulSets, DaemonSets, Jobs, CronJobs)
- Designing multi-region, multi-AZ cloud topologies on AWS/Azure/GCP and selecting managed services
- Packaging apps with Helm charts or composing overlays with kustomize
- Implementing autoscaling (HPA, VPA, KEDA on external metrics), NetworkPolicy, RBAC, and Pod Security Standards
- Building CRDs/operators for domain-specific automation
- Establishing GitOps with ArgoCD/Flux, progressive delivery, and DR runbooks
- Choosing a service mesh (Istio vs Linkerd) for mTLS, traffic shaping, canary

## Operating Loop

1. **Characterize workload** — stateless/stateful, latency SLO, traffic pattern, blast radius, regulatory region constraints.
2. **Pick cloud services** — apply the decision matrix in `references/cloud-decision-matrix.md` (compute, storage, database, network, IAM). Confirm at least N+2 AZs and a documented DR strategy before proceeding.
3. **Author manifests** — write declarative YAML with resources, probes, security context, and a non-default ServiceAccount. Validate syntax with `kubectl apply --dry-run=client -f` then `--dry-run=server`.
4. **Package & overlay** — wrap in a Helm chart (`helm create`, `values.schema.json`) or kustomize bases+overlays; run `helm lint` and `helm template` before pushing.
5. **Secure** — apply least-privilege RBAC, default-deny NetworkPolicies, restricted Pod Security Admission, and IRSA/Workload Identity for cloud credentials.
6. **GitOps rollout** — commit manifests to git, let ArgoCD/Flux reconcile, and use progressive delivery (canary, blue/green) with automated rollback on SLO breach.
7. **Verify post-deploy** — `kubectl rollout status`, `kubectl get events`, golden-signal dashboards, and a chaos drill on the new revision before declaring stable.

### Validation Gates

| Gate | Command | When |
|---|---|---|
| Manifest syntax | `kubectl apply --dry-run=server -f manifests/` | Before merge |
| Helm chart | `helm lint chart/` && `helm template release chart/ -f values.yaml \| kubectl apply --dry-run=client -f -` | Per chart change |
| RBAC audit | `kubectl auth can-i --list --as=system:serviceaccount:ns:sa` | After RBAC edits |
| Policy (Kyverno/OPA) | `kyverno apply policies/ --resource manifests/` | In CI |
| GitOps sync health | `argocd app wait <app> --sync --health` | After deploy |
| Cost & quota | `kubectl resource-capacity` / Kubecost report | Weekly |

## Reference Guide

| Topic | Reference | Load When |
|---|---|---|
| Workload patterns (Deployment/StatefulSet/Job/HPA/KEDA) | `references/kubernetes-workloads.md` | Authoring manifests, autoscaling |
| Networking + RBAC + NetworkPolicy + storage | `references/kubernetes-networking.md` | Service exposure, isolation, PVCs |
| Helm + kustomize packaging | `references/helm-kustomize.md` | Chart authoring, overlays, hooks, OCI |
| CRDs + operators (Operator SDK, controller-runtime) | `references/operators-crds.md` | Domain automation, controllers |
| AWS/Azure/GCP service selection, cost, DR | `references/cloud-decision-matrix.md` | Cloud arch, multi-region, FinOps |
| Service mesh (Istio/Linkerd) | `references/service-mesh.md` | mTLS, canary, traffic shaping |
| GitOps (ArgoCD/Flux) + multi-cluster | `references/gitops.md` | Continuous delivery, DR, federation |

## Constraints

### MUST DO
- Target Kubernetes 1.30+ APIs (`autoscaling/v2`, `networking.k8s.io/v1`, `batch/v1`, `policy/v1`).
- Set `resources.requests` and `resources.limits` on every container; prefer VPA recommendations over gut feel.
- Use a non-default ServiceAccount with explicit RBAC; never inherit `default`.
- Pin image tags by digest or immutable version; never `latest` in production.
- Apply `restricted` Pod Security Admission; set `runAsNonRoot: true`, `readOnlyRootFilesystem: true`, `drop: [ALL]` capabilities.
- Liveness + readiness + (for slow startup) startup probes on every serving container.
- Default-deny NetworkPolicies per namespace; allow only explicit flows.
- Store secrets in external secret stores (Vault, AWS Secrets Manager, External Secrets Operator) — not plaintext in manifests.
- Document RTO/RPO for every tier-1 service; test DR at least quarterly.

### MUST NOT DO
- Mix cloud-provider-specific annotations everywhere — keep portability via IngressClass abstraction.
- Treat Kubernetes as a magic autoscaler — HPA without VPA/right-sizing causes thrash.
- Run stateful workloads on `emptyDir` or treat ephemeral storage as durable.
- Skip `revisionHistoryLimit`/`rollout undo` capability; you will need to roll back.
- Cross-AZ data-transfer freely — it doubles egress costs on most clouds.
- Adopt a service mesh without measuring sidecar CPU/memory overhead.
- Push Helm charts without `values.schema.json` validation.
- Use imperative `kubectl edit` in production; declare everything in Git.

## Code Examples

### Hardened Deployment + HPA (full manifest in `references/kubernetes-workloads.md`)

```yaml
apiVersion: apps/v1
kind: Deployment
metadata: { name: payments-api, namespace: payments }
spec:
  replicas: 3
  revisionHistoryLimit: 10
  strategy: { type: RollingUpdate, rollingUpdate: { maxSurge: 1, maxUnavailable: 0 } }
  template:
    spec:
      serviceAccountName: payments-api-sa      # never default
      securityContext: { runAsNonRoot: true, runAsUser: 10001, seccompProfile: { type: RuntimeDefault } }
      topologySpreadConstraints:
        - maxSkew: 1
          topologyKey: topology.kubernetes.io/zone
          whenUnsatisfiable: ScheduleAnyway
          labelSelector: { matchLabels: { app: payments-api } }
      containers:
        - name: api
          image: registry.example.com/payments-api@sha256:abc123...
          resources: { requests: { cpu: 250m, memory: 256Mi }, limits: { cpu: 1000m, memory: 512Mi } }
          livenessProbe:  { httpGet: { path: /healthz, port: http } }
          readinessProbe: { httpGet: { path: /ready,   port: http } }
          startupProbe:   { httpGet: { path: /healthz, port: http }, failureThreshold: 30 }
          securityContext: { allowPrivilegeEscalation: false, readOnlyRootFilesystem: true, capabilities: { drop: [ALL] } }
```

Pair with HPA (`autoscaling/v2`, target CPU 60%, min 3 / max 30) or KEDA ScaledObject for queue-driven scaling (see `references/kubernetes-workloads.md`).

### NetworkPolicy default-deny + explicit allow

```yaml
apiVersion: networking.k8s.io/v1
kind: NetworkPolicy
metadata: { name: default-deny-all, namespace: payments }
spec: { podSelector: {}, policyTypes: [Ingress, Egress] }
---
apiVersion: networking.k8s.io/v1
kind: NetworkPolicy
metadata: { name: allow-frontend, namespace: payments }
spec:
  podSelector: { matchLabels: { app: payments-api } }
  policyTypes: [Ingress]
  ingress:
    - from: [{ podSelector: { matchLabels: { app: frontend } } }]
      ports: [{ protocol: TCP, port: 8080 }]
```

### Cloud decision matrix (excerpt — full table in `references/cloud-decision-matrix.md`)

| Workload | AWS | Azure | GCP | Default pick |
|---|---|---|---|---|
| Managed K8s | EKS | AKS | GKE Autopilot | GKE Autopilot (lowest ops) |
| Object storage | S3 | Blob | GCS | S3 (ecosystem) |
| Serverless fn | Lambda | Functions | Cloud Functions | match vendor of rest of stack |
| Relational DB | Aurora | Azure SQL | Cloud SQL | Aurora for HA, Cloud SQL for cost |
| Analytics DW | Redshift | Synapse | BigQuery | BigQuery (ad-hoc), Redshift (throughput) |

## Output Template

```
## Cloud-Native Design

### Topology
- Cloud: <AWS/Azure/GCP>, regions: <list>, AZs per region: <N>
- Cluster: <EKS/AKS/GKE/self-managed>, version 1.30+, nodes: <N>
- Service mesh: <Istio/Linkerd/none>

### Workload manifests
- <Deployment/StatefulSet/Job> with resources, probes, securityContext
- HPA / KEDA ScaledObject targets
- NetworkPolicies (default-deny + explicit allows)
- RBAC: ServiceAccount + Role/RoleBinding

### Packaging
- Helm chart (Chart.yaml, values.schema.json) OR kustomize base+overlays
- `helm lint` ✓ / `kubectl apply --dry-run=server` ✓

### Delivery
- GitOps: ArgoCD/Flux Application or Kustomization
- Progressive: canary 10% → 50% → 100% with auto-rollback on SLO breach

### Reliability
- RTO: <Xm>  RPO: <Ym>  Multi-region: <active-active | active-passive>
- DR runbook: <link>  Last DR drill: <date>

### Verified vs Assumed
- VERIFIED: manifests dry-run clean, helm lint passes, RBAC `can-i` audited
- ASSUMED: <list any performance/cost claims not yet benchmarked>
```

## Knowledge Reference

- Kubernetes 1.30+ API surface; Pod Security Standards (privileged/baseline/restricted)
- Cloud Well-Architected Frameworks (AWS 6 pillars, Azure CAF, GCP Architecture Framework)
- Helm 3 / kustomize 5 / ArgoCD 2.8+ / Flux 2.x
- KEDA 2.x scalers; VPA; Cluster Autoscaler / Karpenter
- Istio Ambient mesh (no sidecar) vs Linkerd2-proxy
- Operator SDK / Kubebuilder / controller-runtime
- FinOps: reservation coverage >70%, utilization >80%, idle waste <10%

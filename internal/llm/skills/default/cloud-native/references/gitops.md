# GitOps — ArgoCD / Flux + Multi-Cluster

## Why GitOps

GitOps principles:
1. **Declarative** — system state described in Git
2. **Versioned and immutable** — Git history is the source of truth
3. **Pulled automatically** — agents in cluster pull changes (not pushed)
4. **Continuously reconciled** — drift is corrected automatically

Benefits: audit log, easy rollback (`git revert`), PR review for prod, disaster recovery (rebuild cluster from Git).

## ArgoCD vs Flux

| Feature | ArgoCD | Flux |
|---|---|---|
| UI | Rich web UI + CLI | CLI-first (Weave GitOps Adds UI) |
| Sync model | Manual + auto | Auto-continuous |
| App definition | `Application` CR | `Kustomization` / `HelmRelease` CR |
| Multi-source | Yes (1.0+) | Yes |
| Health assessment | Built-in (Liveness, Rollout) | Via alerts |
| Notifications | Built-in (Slack, email, webhook) | Via notification-controller |
| RBAC | Built-in, SSO | Via Kubernetes RBAC |
| Progressive delivery | Argo Rollouts | Flagger |
| Ecosystem | Larger community | Tight CNCF integration |

**Default pick**: ArgoCD for teams that want a UI; Flux for CLI-native teams. Both are production-grade.

## ArgoCD Setup

```bash
kubectl create namespace argocd
kubectl apply -n argocd -f https://raw.githubusercontent.com/argoproj/argo-cd/stable/manifests/install.yaml

# Get admin password
kubectl -n argocd get secret argocd-initial-admin-secret -o jsonpath="{.data.password}" | base64 -d

# Port-forward UI
kubectl port-forward svc/argocd-server -n argocd 8080:443

# Install CLI
brew install argocd
argocd login localhost:8080
```

### Application (declarative)

```yaml
apiVersion: argoproj.io/v1alpha1
kind: Application
metadata:
  name: orders-api
  namespace: argocd
  finalizers:
    - resources-finalizer.argocd.argoproj.io   # cascade delete
spec:
  project: default
  source:
    repoURL: https://github.com/org/gitops-repo
    targetRevision: main
    path: apps/orders-api/overlays/production
  destination:
    server: https://kubernetes.default.svc
    namespace: orders
  syncPolicy:
    automated:
      prune: true              # delete resources removed from git
      selfHeal: true           # revert manual edits
    syncOptions:
      - CreateNamespace=true
      - PruneLast=true
      - ApplyOutOfSyncOnly=true
    retry:
      limit: 5
      backoff: { duration: 5s, factor: 2, maxDuration: 3m }
```

### App-of-Apps pattern (one Application that owns others)

```yaml
apiVersion: argoproj.io/v1alpha1
kind: Application
metadata: { name: root, namespace: argocd }
spec:
  source:
    repoURL: https://github.com/org/gitops-repo
    path: apps/
  destination:
    server: https://kubernetes.default.svc
    namespace: argocd
  syncPolicy:
    automated: { prune: true, selfHeal: true }
```

`apps/` contains sub-folders, each with its own `Application`. Recursion scales to hundreds of apps.

### ApplicationSet — generate Applications from a template

```yaml
apiVersion: argoproj.io/v1alpha1
kind: ApplicationSet
metadata: { name: orders-api-all-envs, namespace: argocd }
spec:
  generators:
    - list:
        elements:
          - { env: dev,        cluster: https://1.2.3.4 }
          - { env: staging,    cluster: https://5.6.7.8 }
          - { env: production, cluster: https://9.10.11.12 }
  template:
    metadata: { name: 'orders-api-{{env}}' }
    spec:
      source:
        repoURL: https://github.com/org/gitops-repo
        targetRevision: main
        path: 'apps/orders-api/overlays/{{env}}'
      destination:
        server: '{{cluster}}'
        namespace: orders
      syncPolicy:
        automated: { prune: true, selfHeal: true }
```

Also supports Git directory generator, matrix generator, PR generator (per-PR preview envs).

## Flux Setup

```bash
flux bootstrap github \
  --owner=org \
  --repository=gitops-repo \
  --branch=main \
  --path=clusters/production \
  --personal=false
```

### Flux Kustomization (continuous sync)

```yaml
apiVersion: kustomize.toolkit.fluxcd.io/v1
kind: Kustomization
metadata: { name: orders-api, namespace: flux-system }
spec:
  interval: 1m
  path: ./apps/orders-api/overlays/production
  sourceRef:
    kind: GitRepository
    name: gitops-repo
  prune: true
  validation: server
  healthChecks:
    - apiVersion: apps/v1
      kind: Deployment
      name: orders-api
      namespace: orders
  postBuild:
    substitute: { cluster_name: production }
```

### HelmRelease (Flux-managed Helm)

```yaml
apiVersion: helm.toolkit.fluxcd.io/v2beta1
kind: HelmRelease
metadata: { name: orders-api, namespace: orders }
spec:
  interval: 1m
  chart:
    spec:
      chart: orders-api
      version: "1.2.x"
      sourceRef:
        kind: HelmRepository
        name: org-charts
        namespace: flux-system
  values:
    replicaCount: 3
    autoscaling: { enabled: true, minReplicas: 3, maxReplicas: 30 }
```

## Repository Layout

### Per-env overlay (classic)

```
gitops-repo/
├── apps/
│   ├── orders-api/
│   │   ├── base/                  # kustomize base
│   │   │   ├── kustomization.yaml
│   │   │   ├── deployment.yaml
│   │   │   └── service.yaml
│   │   └── overlays/
│   │       ├── dev/
│   │       ├── staging/
│   │       └── production/
│   └── frontend/
├── clusters/
│   ├── production/
│   │   ├── flux-system/           # flux bootstrap output
│   │   └── apps.yaml              # Kustomization pointing to apps/
│   └── staging/
└── infrastructure/
    ├── argocd/
    ├── cert-manager/
    └── external-secrets/
```

### Hub-and-spoke (one repo, multiple clusters)

Use ApplicationSet's cluster generator to deploy the same app to many clusters with cluster-specific patches.

## Progressive Delivery

### Argo Rollouts — canary

```yaml
apiVersion: argoproj.io/v1alpha1
kind: Rollout
metadata: { name: orders-api, namespace: orders }
spec:
  replicas: 10
  strategy:
    canary:
      canaryService: orders-api-canary
      stableService: orders-api-stable
      trafficRouting:
        istio:
          virtualService: { name: orders-api, routes: [primary] }
      steps:
        - setWeight: 10
        - pause: { duration: 5m }
        - analysis:
            templates:
              - templateName: success-rate
            args: [{ name: service-name, value: orders-api-canary }]
        - setWeight: 30
        - pause: { duration: 5m }
        - setWeight: 50
        - pause: { duration: 10m }
        - setWeight: 100
```

### Flagger (Flux-native)

```yaml
apiVersion: flagger.app/v1beta1
kind: Canary
metadata: { name: orders-api, namespace: orders }
spec:
  targetRef: { apiVersion: apps/v1, kind: Deployment, name: orders-api }
  service: { port: 80, gateways: [orders-gateway], hosts: [orders.example.com] }
  progressDeadlineSeconds: 600
  analysis:
    interval: 1m
    threshold: 5
    maxWeight: 50
    stepWeight: 10
    metrics:
      - name: request-success-rate
        threshold: 99
        interval: 1m
      - name: request-duration
        threshold: 500
        interval: 1m
```

Both auto-rollback if metrics breach thresholds.

## Secrets Management

Never commit plaintext secrets to Git. Options:

| Tool | Approach |
|---|---|
| **External Secrets Operator** | Syncs from AWS SM / Azure KV / GCP SM / Vault into K8s Secrets |
| **Sealed Secrets** (Bitnami) | Encrypts secret with cluster's private key; safe in Git |
| **SOPS + age** | Encrypt values files; decrypted by Flux/ArgoCD via KMS |
| **ESO + Vault** | Pull dynamic secrets from HashiCorp Vault |

```yaml
# External Secrets Operator example
apiVersion: external-secrets.io/v1beta1
kind: ExternalSecret
metadata: { name: orders-db, namespace: orders }
spec:
  refreshInterval: 1h
  secretStoreRef:
    name: aws-secrets
    kind: ClusterSecretStore
  target:
    name: orders-db       # creates this K8s Secret
    creationPolicy: Owner
  data:
    - secretKey: password
      remoteRef: { key: prod/orders/db, property: password }
```

## Multi-Cluster Patterns

| Pattern | Use case |
|---|---|
| **Hub-spoke** | One control cluster pushes to N workload clusters (ArgoCD App-of-Apps) |
| **Per-cluster repos** | Each cluster has its own repo path; isolated blast radius |
| **Single repo, multi-cluster** | ApplicationSet generates per-cluster Applications from one source |
| **Pull-based** (Flux) | Each cluster has its own Flux instance pulling its path |

### ArgoCD multi-cluster

```bash
# Register a cluster with ArgoCD
argocd cluster add workload-cluster-context --name prod-east

# Now Applications can target:
# destination: { server: https://workload-cluster-API }
```

### DR via GitOps

GitOps makes DR trivial:
1. Cluster dies.
2. Provision new cluster (via `cluster-api` or Terraform).
3. Install ArgoCD/Flux pointing at the same Git repo.
4. Wait for sync.
5. Done — system state reconstructed from Git in minutes.

Document RTO = cluster provision time + sync time (typically 15-30 min).

## Sync Waves & Hooks

ArgoCD sync waves let you order resource creation:

```yaml
metadata:
  annotations:
    argocd.argoproj.io/sync-wave: "-1"   # earlier (lower = earlier)
```

| Wave | Use |
|---|---|
| `-5` | CRDs, namespaces |
| `-1` | Operators, controllers |
| `0` | Default — most resources |
| `+1` | Apps |
| `+5` | Tests, smoke checks |

Hooks (`PreSync`, `Sync`, `PostSync`, `SyncFail`) run Jobs at specific points — useful for DB migrations:

```yaml
metadata:
  annotations:
    argocd.argoproj.io/hook: PreSync
    argocd.argoproj.io/hook-delete-policy: HookSucceeded
```

## Verification

```bash
# ArgoCD: check app health
argocd app get orders-api
argocd app wait orders-api --sync --health --timeout 5m

# Flux: force reconciliation
flux reconcile kustomization orders-api --with-source
flux get kustomizations --watch

# Both: confirm git HEAD matches cluster
argocd app history orders-api
```

## Common Pitfalls

1. **`selfHeal: true` without `prune: true`** — manual edits reverted but orphan resources persist.
2. **No sync windows** — deploys during peak traffic. Use ArgoCD sync windows.
3. **One giant Application for whole cluster** — blast radius too big. Split per app/team.
4. **Storing cluster secrets in Git** — use External Secrets Operator instead.
5. **No `progressDeadlineSeconds`** — hung rollouts block further syncs forever.
6. **Forgetting ImagePullPolicy + private registry creds** — pods ImagePullBackOff.
7. **Drift between clusters** — use ApplicationSet to keep them aligned from one template.

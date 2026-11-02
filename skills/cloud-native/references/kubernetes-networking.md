# Kubernetes Networking, Storage, RBAC

## Service Types — Pick Correctly

| Type | When | Notes |
|---|---|---|
| `ClusterIP` | Internal-only service | Default; never expose externally |
| `NodePort` | Quick dev / metallb | 30000-32767; avoid in prod |
| `LoadBalancer` | Cloud-managed LB | Creates AWS ALB/NLB, Azure LB, GCP LB |
| `Headless` (`clusterIP: None`) | StatefulSet peer DNS | `pod-0.svc.ns.svc.cluster.local` |
| `ExternalName` | CNAME alias to external service | DNS-only, no proxy |

```yaml
apiVersion: v1
kind: Service
metadata:
  name: orders-api
  namespace: orders
spec:
  type: ClusterIP
  selector: { app: orders-api }
  ports:
    - { name: http, port: 80, targetPort: http, protocol: TCP }
```

## Ingress

```yaml
apiVersion: networking.k8s.io/v1
kind: Ingress
metadata:
  name: orders-api
  namespace: orders
  annotations:
    cert-manager.io/cluster-issuer: letsencrypt-prod
    nginx.ingress.kubernetes.io/rate-limit: "100"
    nginx.ingress.kubernetes.io/ssl-redirect: "true"
spec:
  ingressClassName: nginx
  tls:
    - hosts: [api.example.com]
      secretName: orders-tls
  rules:
    - host: api.example.com
      http:
        paths:
          - path: /orders
            pathType: Prefix
            backend:
              service:
                name: orders-api
                port: { name: http }
```

Cloud-specific Ingress controllers to know:
- **AWS**: AWS Load Balancer Controller → ALB (L7) or NLB (L4)
- **Azure**: Application Gateway Ingress Controller (AGIC)
- **GCP**: GKE Ingress → Cloud Load Balancing
- **Generic**: ingress-nginx, Traefik, Envoy Gateway, Cilium

## NetworkPolicy — Zero-Trust in Cluster

Default-deny first, then allow explicitly. CNI must support NetworkPolicy (Cilium, Calico, Antrea — **not** flannel default).

```yaml
# 1. Default deny all ingress + egress in namespace
apiVersion: networking.k8s.io/v1
kind: NetworkPolicy
metadata: { name: default-deny-all, namespace: orders }
spec:
  podSelector: {}
  policyTypes: [Ingress, Egress]
---
# 2. Allow frontend -> orders-api on 8080
apiVersion: networking.k8s.io/v1
kind: NetworkPolicy
metadata: { name: allow-frontend, namespace: orders }
spec:
  podSelector: { matchLabels: { app: orders-api } }
  policyTypes: [Ingress]
  ingress:
    - from:
        - podSelector: { matchLabels: { app: frontend } }
      ports:
        - { protocol: TCP, port: 8080 }
---
# 3. Allow orders-api -> postgres (in `data` namespace) on 5432
apiVersion: networking.k8s.io/v1
kind: NetworkPolicy
metadata: { name: allow-egress-postgres, namespace: orders }
spec:
  podSelector: { matchLabels: { app: orders-api } }
  policyTypes: [Egress]
  egress:
    - to:
        - namespaceSelector: { matchLabels: { name: data } }
          podSelector: { matchLabels: { app: postgres } }
      ports:
        - { protocol: TCP, port: 5432 }
    # DNS resolution
    - to:
        - namespaceSelector: { matchLabels: { kubernetes.io/metadata.name: kube-system } }
          podSelector: { matchLabels: { k8s-app: kube-dns } }
      ports:
        - { protocol: UDP, port: 53 }
```

## RBAC — Least Privilege

Never use the `default` ServiceAccount for app pods. Always create a dedicated SA with a Role granting only what's needed.

```yaml
apiVersion: v1
kind: ServiceAccount
metadata: { name: orders-api-sa, namespace: orders }
automountServiceAccountToken: false   # don't mount token if pod doesn't talk to K8s API
---
apiVersion: rbac.authorization.k8s.io/v1
kind: Role
metadata: { name: orders-api, namespace: orders }
rules:
  - apiGroups: [""]
    resources: ["configmaps"]
    verbs: ["get", "list", "watch"]
    resourceNames: ["orders-config"]   # scope to specific resource
---
apiVersion: rbac.authorization.k8s.io/v1
kind: RoleBinding
metadata: { name: orders-api, namespace: orders }
subjects:
  - kind: ServiceAccount
    name: orders-api-sa
    namespace: orders
roleRef:
  kind: Role
  name: orders-api
  apiGroup: rbac.authorization.k8s.io
```

Audit who-can-do-what:

```bash
# List everything a SA can do
kubectl auth can-i --list --as=system:serviceaccount:orders:orders-api-sa -n orders

# Check a specific action
kubectl auth can-i delete pods --as=system:serviceaccount:orders:orders-api-sa -n orders
```

## Cloud Identity Mapping (no static creds)

| Cloud | Mechanism |
|---|---|
| AWS | **IRSA** (IAM Roles for Service Accounts) — annotations on SA |
| Azure | **Workload Identity** — annotations + federated credential |
| GCP | **Workload Identity** — service account annotation + IAM binding |

```yaml
# AWS IRSA
apiVersion: v1
kind: ServiceAccount
metadata:
  name: orders-api-sa
  namespace: orders
  annotations:
    eks.amazonaws.com/role-arn: arn:aws:iam::123456789012:role/orders-api-irsa
```

## Storage

### StorageClass (SSD example per cloud)

```yaml
# AWS
apiVersion: storage.k8s.io/v1
kind: StorageClass
metadata: { name: fast-ssd }
provisioner: ebs.csi.aws.com
parameters:
  type: gp3
  iops: "3000"
  throughput: "125"
volumeBindingMode: WaitForFirstConsumer
allowVolumeExpansion: true
```

| Cloud | Provisioner | Class |
|---|---|---|
| AWS | `ebs.csi.aws.com` | gp3 (default), io2 (high IOPS) |
| Azure | `disk.csi.azure.com` | managed-csi (Premium SSD), managed-csi-premium |
| GCP | `pd.csi.storage.gke.io` | pd-balanced, pd-ssd, pd-extreme |

### PVC patterns

```yaml
# Single pod RWO
apiVersion: v1
kind: PersistentVolumeClaim
metadata: { name: orders-data, namespace: orders }
spec:
  accessModes: [ReadWriteOnce]
  storageClassName: fast-ssd
  resources: { requests: { storage: 50Gi } }
```

| AccessMode | Use case |
|---|---|
| `ReadWriteOnce` | Single pod (DB, queue) |
| `ReadOnlyMany` | Multi-pod read (config, assets) |
| `ReadWriteMany` | Multi-pod write (NFS, CephFS, AWS EFS) |
| `ReadWriteOncePod` | Single pod exclusive (1.27+; prevents multi-attach races) |

## DNS & Service Discovery

- Every Service gets `<svc>.<ns>.svc.cluster.local` A record.
- Headless Services return pod IPs directly.
- StatefulSet pods get stable names: `<sts-name>-<ordinal>.<svc-headless>.<ns>.svc.cluster.local`.
- CoreDNS: customize via ConfigMap `coredns` in `kube-system`; add custom zones, rewrites, fallbacks.

## Common Pitfalls

1. **`NetworkPolicy` with no CNI support** → silently ignored. Verify CNI is Cilium/Calico/Antrea.
2. **`automountServiceAccountToken: true` on a pod that doesn't need API access** → credential exposure risk.
3. **Ingress with no `ingressClassName`** → picks the default ingress class, which may not exist.
4. **PVC on `gp2` for a database** → gp3 is 20% cheaper and faster; migrate.
5. **Forgetting DNS egress in NetworkPolicy** → pods can't resolve services. Add the kube-dns allow rule.

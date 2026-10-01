# Service Mesh — Istio / Linkerd

## Do You Need a Service Mesh?

A service mesh adds mTLS, traffic shaping, and observability to inter-service traffic via sidecars (or Ambient mode).

| Symptom | Mesh helps? |
|---|---|
| mTLS between all services | Yes |
| Canary / blue-green / traffic mirroring | Yes |
| Per-service circuit breakers, retries, timeouts | Yes |
| Distributed tracing automatically | Yes |
| Single-service, single ingress | No — just use Ingress |
| <5 services | Probably no — KISS |

## Istio vs Linkerd

| Feature | Istio | Linkerd |
|---|---|---|
| Data plane | Envoy sidecar (or Ambient mode in 1.20+) | linkerd2-proxy (Rust) sidecar |
| Resource usage | Higher (Envoy ~50-100MB/sidecar) | Lower (~10-30MB/sidecar) |
| Features | Most extensive | Focused, simpler |
| mTLS | Built-in, automatic | Built-in, automatic |
| Traffic management | VirtualService + DestinationRule (very flexible) | ServiceProfile + TrafficSplit (SMI) |
| Multi-cluster | Native support | Requires setup |
| Policy | AuthorizationPolicy + RequestAuthentication | Server authorization only |
| Telemetry | Rich (with Kiali) | Rich (built-in dashboards) |
| Learning curve | Steeper | Gentler |
| Ambient mode | Yes (1.20+, no sidecar for L4) | No |

**Default pick**: Linkerd for simplicity + low overhead; Istio when you need Ambient, advanced policy, or multi-cluster.

## Istio Installation

```bash
# Install CLI
curl -L https://istio.io/downloadIstio | sh -
export PATH=$PWD/istio-*/bin:$PATH

# Install with default profile
istioctl install --set profile=default -y

# Enable injection for a namespace
kubectl label namespace production istio-injection=enabled

# Verify
istioctl verify-install
kubectl get pods -n istio-system
```

Profiles:
- `minimal` — control plane only
- `default` — control plane + ingress gateway
- `demo` — extra features (egress, telemetry)
- `empty` — fully custom

## Traffic Management

### VirtualService — routing, canary, retries

```yaml
apiVersion: networking.istio.io/v1beta1
kind: VirtualService
metadata: { name: orders-api, namespace: production }
spec:
  hosts: [orders-api, orders.example.com]
  gateways: [mesh, orders-gateway]
  http:
    # Canary: 90% v1, 10% v2
    - match: [{ uri: { prefix: /api } }]
      route:
        - destination: { host: orders-api, subset: v1 }
          weight: 90
        - destination: { host: orders-api, subset: v2 }
          weight: 10
      timeout: 30s
      retries:
        attempts: 3
        perTryTimeout: 10s
        retryOn: connect-failure,refused-stream,5xx
    # Traffic mirroring (shadow v2 with 100% traffic)
    - route:
        - destination: { host: orders-api, subset: v1 }
      mirror:
        host: orders-api
        subset: v2
      mirrorPercentage: { value: 100.0 }
```

### DestinationRule — subsets, circuit breaker, outlier detection

```yaml
apiVersion: networking.istio.io/v1beta1
kind: DestinationRule
metadata: { name: orders-api, namespace: production }
spec:
  host: orders-api
  trafficPolicy:
    connectionPool:
      tcp: { maxConnections: 100, connectTimeout: 5s }
      http: { http2MaxRequests: 1000, maxRequestsPerConnection: 100 }
    loadBalancer: { simple: LEAST_REQUEST }
    outlierDetection:
      consecutive5xxErrors: 5
      interval: 10s
      baseEjectionTime: 30s
      maxEjectionPercent: 50
  subsets:
    - name: v1
      labels: { version: v1 }
    - name: v2
      labels: { version: v2 }
```

### Gateway — external entry

```yaml
apiVersion: networking.istio.io/v1beta1
kind: Gateway
metadata: { name: orders-gateway, namespace: production }
spec:
  selector: { istio: ingressgateway }
  servers:
    - port: { number: 80, name: http, protocol: HTTP }
      hosts: [orders.example.com]
      tls: { httpsRedirect: true }
    - port: { number: 443, name: https, protocol: HTTPS }
      hosts: [orders.example.com]
      tls:
        mode: SIMPLE
        credentialName: orders-tls
```

## Security

### mTLS — Permissive then Strict

```yaml
# Step 1: Permissive (allow both mTLS and plaintext during migration)
apiVersion: security.istio.io/v1beta1
kind: PeerAuthentication
metadata: { name: default, namespace: production }
spec:
  mtls: { mode: PERMISSIVE }
---
# Step 2: Strict (after all services have sidecars)
apiVersion: security.istio.io/v1beta1
kind: PeerAuthentication
metadata: { name: default, namespace: production }
spec:
  mtls: { mode: STRICT }
```

Mesh-wide strict: apply in `istio-system` namespace.

### AuthorizationPolicy — explicit allow-list

```yaml
apiVersion: security.istio.io/v1beta1
kind: AuthorizationPolicy
metadata: { name: orders-api-authz, namespace: production }
spec:
  selector: { matchLabels: { app: orders-api } }
  action: ALLOW
  rules:
    # Only frontend + api-gateway SAs can call /api
    - from:
        - source:
            principals:
              - cluster.local/ns/production/sa/frontend
              - cluster.local/ns/production/sa/api-gateway
      to:
        - operation: { methods: [GET, POST], paths: [/api/*] }
    # Anyone can hit health
    - to:
        - operation: { methods: [GET], paths: [/health, /ready] }
```

Implicit deny: when any ALLOW rule exists, only matching traffic is allowed.

### RequestAuthentication — JWT

```yaml
apiVersion: security.istio.io/v1beta1
kind: RequestAuthentication
metadata: { name: orders-api-jwt, namespace: production }
spec:
  selector: { matchLabels: { app: orders-api } }
  jwtRules:
    - issuer: https://auth.example.com/
      jwksUri: https://auth.example.com/.well-known/jwks.json
      forwardOriginalToken: true
```

Pair with AuthorizationPolicy that requires a valid token for protected paths.

## Fault Injection (for testing)

```yaml
apiVersion: networking.istio.io/v1beta1
kind: VirtualService
metadata: { name: orders-api-fault, namespace: production }
spec:
  hosts: [orders-api]
  http:
    - match: [{ headers: { x-test-fault: { exact: inject } } }]
      fault:
        delay: { percentage: { value: 50 }, fixedDelay: 5s }
        abort: { percentage: { value: 10 }, httpStatus: 503 }
      route: [{ destination: { host: orders-api } }]
```

## Observability

Install addons:

```bash
kubectl apply -f https://raw.githubusercontent.com/istio/istio/release-1.22/samples/addons/kiali.yaml
kubectl apply -f https://raw.githubusercontent.com/istio/istio/release-1.22/samples/addons/jaeger.yaml
kubectl apply -f https://raw.githubusercontent.com/istio/istio/release-1.22/samples/addons/prometheus.yaml
kubectl apply -f https://raw.githubusercontent.com/istio/istio/release-1.22/samples/addons/grafana.yaml

istioctl dashboard kiali
istioctl dashboard jaeger
```

Istio auto-generates golden-signal metrics per service (R/E/D — rate, errors, duration) with labels for source/destination.

## Linkerd Quick Start

```bash
curl --proto '=https' --tlsv1.2 -sSfL https://run.linkerd.io/install | sh
export PATH=$HOME/.linkerd2/bin:$PATH

linkerd check --pre                          # pre-flight
linkerd install --crds | kubectl apply -f -  # CRDs first
linkerd install | kubectl apply -f -         # control plane
linkerd check                                # verify

kubectl annotate namespace production linkerd.io/inject=enabled
# Restart pods to inject sidecar
kubectl rollout restart deployment -n production
```

Linkerd ServiceProfile for per-route metrics + retries:

```yaml
apiVersion: linkerd.io/v1alpha2
kind: ServiceProfile
metadata:
  name: orders-api.production.svc.cluster.local
  namespace: production
spec:
  routes:
    - name: GET /api/orders
      condition: { method: GET, pathRegex: /api/orders }
      timeout: 5s
      isRetryable: true
  retryBudget: { retryRatio: 0.2, minRetriesPerSecond: 10, ttl: 10s }
```

## Sidecar Overhead — Plan for It

| Sidecar | CPU req | Mem req | Notes |
|---|---|---|---|
| Istio Envoy | 100m | 128Mi | Tune per service |
| Linkerd2-proxy | 10m | 64Mi | Much lighter |
| Istio Ambient (ztunnel) | 10m | 32Mi | L4 only; Envoy for L7 per-route |

Multiply by pod count: 100 pods × Istio = 10 vCPU + 12.8 GB just for sidecars. Use Ambient mode or Linkerd to reduce.

## Multi-Cluster Mesh (Istio)

```yaml
# Primary cluster (cluster1)
apiVersion: install.istio.io/v1alpha1
kind: IstioOperator
spec:
  values:
    global:
      meshID: mesh1
      multiCluster: { clusterName: cluster1 }
      network: network1
```

```bash
# Share cluster1's API with cluster2
istioctl x create-remote-secret --context=cluster1 --name=cluster1 \
  | kubectl apply -f - --context=cluster2
```

Service-to-service calls then traverse cross-cluster gateways automatically.

## Migration Path (Adding Mesh to Existing Cluster)

1. **Install mesh control plane** (`istioctl install` or `linkerd install`).
2. **Enable injection** on one namespace: `kubectl label ns <ns> istio-injection=enabled`.
3. **Roll restart** pods in that namespace to get sidecars.
4. **Set PeerAuthentication to PERMISSIVE** — let mTLS negotiate without breaking.
5. **Verify** in Kiali / Linkerd viz — confirm traffic flowing.
6. **Add AuthorizationPolicy** gradually (start with ALLOW for known callers, deny rest).
7. **Switch to STRICT mTLS** once all services have sidecars.
8. **Add VirtualService / ServiceProfile** for traffic shaping only when needed.

Don't enable mesh everywhere on day one. Adopt incrementally, one namespace at a time.

## Pitfalls

1. **Forgetting to roll restart pods after enabling injection** — no sidecars appear.
2. **Strict mTLS before all services have sidecars** — traffic breaks for plaintext callers.
3. **AuthorizationPolicy with `action: ALLOW` but no rules** — denies everything.
4. **Sidecar resource limits too low** — Envoy OOM-killed; bump to 500Mi limit.
5. **Cross-namespace calls without proper DNS** — use `<svc>.<ns>.svc.cluster.local` FQDN.
6. **Mirroring 100% traffic to canary** — can overload canary; start with 10%.

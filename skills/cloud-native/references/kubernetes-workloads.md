# Kubernetes Workload Patterns (1.30+)

## Choosing a Workload Kind

| Kind | When | Stateful? | Order | Scaling |
|---|---|---|---|---|
| `Deployment` | Stateless services, web APIs | No | Rolling | HPA/KEDA |
| `StatefulSet` | Databases, queues, leader election | Yes (PVC + stable network name) | OrderedReady or Parallel | Manual or HPA with care |
| `DaemonSet` | Per-node agents (log shippers, exporters) | No | Rolling | One per node |
| `Job` | One-shot batch (migration, batch compute) | No | Completion | Indexed for parallel |
| `CronJob` | Scheduled maintenance, backups | No | Schedule | Concurrency policy |

## Deployment — Hardened Baseline

```yaml
apiVersion: apps/v1
kind: Deployment
metadata:
  name: orders-api
  namespace: orders
spec:
  replicas: 3
  revisionHistoryLimit: 10
  strategy:
    type: RollingUpdate
    rollingUpdate: { maxSurge: 1, maxUnavailable: 0 }
  selector: { matchLabels: { app: orders-api } }
  template:
    metadata:
      labels: { app: orders-api, version: "2.1.0" }
    spec:
      serviceAccountName: orders-api-sa
      terminationGracePeriodSeconds: 60
      securityContext:
        runAsNonRoot: true
        runAsUser: 10001
        fsGroup: 10001
        seccompProfile: { type: RuntimeDefault }
      topologySpreadConstraints:
        - maxSkew: 1
          topologyKey: topology.kubernetes.io/zone
          whenUnsatisfiable: ScheduleAnyway
          labelSelector: { matchLabels: { app: orders-api } }
      containers:
        - name: api
          image: registry.example.com/orders-api@sha256:...
          imagePullPolicy: IfNotPresent
          ports: [{ name: http, containerPort: 8080 }]
          resources:
            requests: { cpu: 250m, memory: 256Mi }
            limits:   { cpu: 1000m, memory: 512Mi }
          env:
            - name: DB_HOST
              valueFrom: { configMapKeyRef: { name: orders-config, key: db.host } }
            - name: DB_PASSWORD
              valueFrom: { secretKeyRef: { name: orders-db, key: password } }
          probes:
            liveness:  { httpGet: { path: /healthz, port: http }, periodSeconds: 20 }
            readiness: { httpGet: { path: /ready, port: http }, periodSeconds: 5 }
            startup:   { httpGet: { path: /healthz, port: http }, failureThreshold: 30, periodSeconds: 10 }
          securityContext:
            allowPrivilegeEscalation: false
            readOnlyRootFilesystem: true
            capabilities: { drop: [ALL] }
          volumeMounts:
            - { name: tmp, mountPath: /tmp }
            - { name: config, mountPath: /etc/orders, readOnly: true }
      volumes:
        - { name: tmp, emptyDir: {} }
        - { name: config, configMap: { name: orders-config } }
```

### Why each piece

- **`revisionHistoryLimit: 10`** keeps old ReplicaSets so `kubectl rollout undo` works.
- **`maxUnavailable: 0`** with `maxSurge: 1` ensures capacity never drops during rollout.
- **`topologySpreadConstraints`** spreads pods across AZs (use `topology.kubernetes.io/zone`).
- **`startupProbe`** lets slow-booting apps (JVM, Python ML) avoid being killed by liveness.
- **`readOnlyRootFilesystem: true`** forces writes to mounted `emptyDir`/PVC.

## StatefulSet — PostgreSQL-style

```yaml
apiVersion: apps/v1
kind: StatefulSet
metadata: { name: postgres, namespace: data }
spec:
  serviceName: postgres-headless
  replicas: 3
  podManagementPolicy: Parallel       # faster scaling; use OrderedReady for primaries
  updateStrategy: { type: RollingUpdate }
  selector: { matchLabels: { app: postgres } }
  template:
    metadata: { labels: { app: postgres } }
    spec:
      serviceAccountName: postgres-sa
      securityContext: { runAsUser: 999, fsGroup: 999 }
      containers:
        - name: postgres
          image: postgres:16-alpine
          ports: [{ name: pg, containerPort: 5432 }]
          env:
            - { name: POSTGRES_PASSWORD, valueFrom: { secretKeyRef: { name: pg, key: password } } }
            - { name: PGDATA, value: /var/lib/postgresql/data/pgdata }
          resources:
            requests: { cpu: 500m, memory: 1Gi }
            limits:   { cpu: 2000m, memory: 4Gi }
          livenessProbe:
            exec: { command: [pg_isready, -U, postgres] }
          volumeMounts:
            - { name: data, mountPath: /var/lib/postgresql/data }
  volumeClaimTemplates:
    - metadata: { name: data }
      spec:
        accessModes: [ReadWriteOnce]
        storageClassName: fast-ssd
        resources: { requests: { storage: 100Gi } }
```

Pair with a **headless Service** (`clusterIP: None`) so pods get stable DNS names `postgres-0.postgres-headless.data.svc.cluster.local`.

## Job — Indexed parallel batch

```yaml
apiVersion: batch/v1
kind: Job
metadata: { name: etl-2024-03-15, namespace: data }
spec:
  completions: 10
  parallelism: 5
  completionMode: Indexed          # 1.24+ stable; pod gets JOB_COMPLETION_INDEX env
  backoffLimit: 3
  ttlSecondsAfterFinished: 3600    # auto-cleanup
  template:
    spec:
      restartPolicy: OnFailure
      serviceAccountName: etl-sa
      containers:
        - name: etl
          image: registry.example.com/etl:1.0
          args: ["--shard=$(JOB_COMPLETION_INDEX)"]
```

## HPA on custom metrics (Prometheus Adapter)

```yaml
apiVersion: autoscaling/v2
kind: HorizontalPodAutoscaler
metadata: { name: orders-api, namespace: orders }
spec:
  scaleTargetRef: { apiVersion: apps/v1, kind: Deployment, name: orders-api }
  minReplicas: 3
  maxReplicas: 50
  metrics:
    - type: Pods
      pods:
        metric: { name: http_requests_per_second }
        target: { type: AverageValue, averageValue: 100 }
  behavior:
    scaleUp:
      stabilizationWindowSeconds: 0
      policies:
        - { type: Percent, value: 100, periodSeconds: 30 }
    scaleDown:
      stabilizationWindowSeconds: 300
      policies:
        - { type: Percent, value: 25, periodSeconds: 60 }
```

## KEDA — event-driven autoscaling

Use KEDA when scaling on external signals (queue depth, Kafka lag, Cron schedule). It scales from zero — useful for cost-sensitive workers.

```yaml
apiVersion: keda.sh/v1alpha1
kind: ScaledObject
metadata: { name: orders-worker, namespace: orders }
spec:
  scaleTargetRef: { name: orders-worker }
  minReplicaCount: 0          # scale to zero
  maxReplicaCount: 50
  pollingInterval: 30
  cooldownPeriod: 300
  triggers:
    - type: kafka
      metadata:
        bootstrapServers: kafka.data:9092
        consumerGroup: orders-worker
        topic: orders.events
        lagThreshold: "100"
```

## Pod Disruption Budgets

```yaml
apiVersion: policy/v1
kind: PodDisruptionBudget
metadata: { name: orders-api, namespace: orders }
spec:
  minAvailable: 2
  selector: { matchLabels: { app: orders-api } }
```

Required for any Deployment you want to survive voluntary disruptions (node drains, cluster autoscaler).

## Resource Sizing Cheatsheet

| Workload profile | CPU req | Mem req | CPU lim | Mem lim |
|---|---|---|---|---|
| Light API | 100m | 128Mi | 500m | 512Mi |
| Standard API | 250m | 256Mi | 1000m | 512Mi |
| CPU-bound worker | 500m | 512Mi | 2000m | 1Gi |
| DB (postgres) | 500m | 1Gi | 2000m | 4Gi |
| Sidecar proxy | 100m | 128Mi | 500m | 256Mi |

Use **VPA in `Off` mode** for a week to gather recommendations before right-sizing.

## Verification

```bash
# Dry-run against the API server (validates schemas, quota, admission webhooks)
kubectl apply --dry-run=server -f manifests/

# Watch rollout
kubectl rollout status deployment/orders-api -n orders --timeout=5m

# Audit resources vs requests
kubectl top pods -n orders
kubectl describe pod -n orders -l app=orders-api | grep -A5 "Limits:"

# Check PDB protects against drain
kubectl get pdb -n orders
```

## Common Pitfalls

1. **No `startupProbe` on slow JVM apps** → liveness kills them on cold start. Fix with startup probe.
2. **`maxUnavailable: 1` on a 2-replica Deployment** → 50% capacity drop during rollout. Use `maxUnavailable: 0`.
3. **HPA with CPU target but no `requests`** → HPA can't compute utilization. Always set requests.
4. **StatefulSet with `OrderedReady` and 10 replicas** → rolling update takes hours. Use `podManagementPolicy: Parallel` when safe.
5. **PVC on `emptyDir`** thinking it persists → it doesn't; data lost on pod evict.

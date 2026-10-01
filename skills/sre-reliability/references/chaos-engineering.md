# Chaos Engineering

## What It Is

Chaos engineering is the discipline of **experimenting** on a system to build confidence in its capability to withstand turbulent conditions in production.

It is NOT:
- Randomly breaking things ("let's see what happens")
- Production load testing (different goal)
- A bug-finding technique (use testing for that)
- A replacement for monitoring (chaos REQUIRES monitoring)

It IS:
- Hypothesis-driven experiments
- Controlled fault injection with safety nets
- Validation that steady-state holds under failure
- A practice, not a one-time event

## When to Use

| Situation | Chaos helps? |
|---|---|
| You have SLOs and observability in place | Yes |
| Multi-AZ/multi-region; want to verify failover | Yes |
| Adopting a new pattern (circuit breaker, bulkhead) | Yes |
| No monitoring, no SLOs | No — fix those first |
| Single instance, no redundancy | No — chaos will just break it |
| Pre-launch, no production traffic | No — use load testing instead |

## Experiment Lifecycle

```
1. Hypothesize → 2. Define steady state → 3. Choose blast radius →
4. Inject → 5. Observe → 6. Abort if violated → 7. Rollback → 8. Learn
```

### 1. Hypothesis

Format: **Given [normal state], when [failure occurs], then [expected behavior], measured by [metrics]**

Example:
> Given orders-api is in steady state (p99 < 200ms, errors < 0.1%), when 50% of pods are killed, then latency stays < 500ms and errors < 1% for < 30s, measured by Prometheus.

Bad: "What happens if we kill pods?"  (no hypothesis, no measurement)

### 2. Steady-state definition

```yaml
steady_state:
  metrics:
    - name: Error Rate
      source: prometheus
      query: 'sum(rate(http_requests_total{job="orders-api",status=~"5.."}[5m])) / sum(rate(http_requests_total{job="orders-api"}[5m]))'
      threshold: "< 0.001"
    - name: Latency P99
      source: prometheus
      query: 'histogram_quantile(0.99, sum(rate(http_request_duration_seconds_bucket{job="orders-api"}[5m])) by (le))'
      threshold: "< 0.2"  # seconds
    - name: Throughput
      source: prometheus
      query: 'sum(rate(http_requests_total{job="orders-api"}[5m]))'
      threshold: "> 100"  # req/s minimum
```

If steady state doesn't hold BEFORE injection, abort. Don't run chaos on an already-sick system.

### 3. Blast radius

```yaml
blast_radius:
  environment: staging         # start here; move to prod canary later
  traffic_percentage: 10       # % of traffic affected
  duration_seconds: 300
  max_error_rate: "5%"         # abort threshold
  max_latency_p99: "1s"        # abort threshold
  auto_rollback: true
  canary_users: ["internal-team@example.com"]
  feature_flag: "chaos_orders_canary"
```

| Level | Env | Traffic | Approval |
|---|---|---|---|
| Minimal | dev | 100% | none |
| Low | staging | 100% | team lead |
| Medium | prod canary | 1-5% | team + SRE |
| High | prod broader | 5-25% | VP Eng + SRE |
| Critical | prod 50%+ | EXEC approval + runbook |

Progressive rollout: dev → staging → prod canary (1%) → prod broader.

### 4-7. Execute, observe, abort, rollback

```python
from datetime import datetime, timedelta
import time

class ChaosRunner:
    def __init__(self, max_duration_min=15, max_error_rate=0.05, max_p99_latency=1.0):
        self.max_duration = timedelta(minutes=max_duration_min)
        self.max_error_rate = max_error_rate
        self.max_p99_latency = max_p99_latency

    def run(self, experiment, injector, get_metrics):
        # Pre-flight: verify steady state
        baseline = get_metrics()
        if not self._steady_state_holds(baseline):
            return {"status": "aborted", "reason": "steady state not held at baseline"}

        # Inject
        experiment["started_at"] = datetime.utcnow().isoformat()
        injector.inject()

        try:
            start = datetime.utcnow()
            while datetime.utcnow() - start < self.max_duration:
                time.sleep(10)
                metrics = get_metrics()
                if self._should_abort(metrics):
                    return {"status": "aborted", "reason": "safety constraint violated", "metrics": metrics}
            return {"status": "success", "started_at": experiment["started_at"]}
        finally:
            # ALWAYS rollback
            injector.rollback()
            # Verify recovery
            time.sleep(60)
            recovery = get_metrics()
            if not self._steady_state_holds(recovery):
                return {"status": "failed_recovery", "metrics": recovery}

    def _steady_state_holds(self, m):
        return m["error_rate"] < self.max_error_rate and m["latency_p99"] < self.max_p99_latency

    def _should_abort(self, m):
        return m["error_rate"] > self.max_error_rate or m["latency_p99"] > self.max_p99_latency * 2
```

### 8. Learn

Every experiment produces a written learning summary:
- Hypothesis: confirmed / refuted / partial
- What happened
- What we learned
- Action items (improve circuit breakers, fix monitoring gaps, etc.)

## Tools

| Tool | Best for | Style |
|---|---|---|
| **Litmus Chaos** | Kubernetes-native experiments | CRD-based, lots of experiments |
| **Chaos Mesh** | Kubernetes, good UX | CRD-based, dashboard |
| **AWS FIS** | AWS-native fault injection | Managed service |
| **Gremlin** | Enterprise, multi-platform | Commercial, polished |
| **Chaos Monkey** | Random instance termination | Spinnaker-integrated |
| **Pumba** | Docker/network chaos | CLI, lightweight |
| **toxiproxy** | Network latency/loss | TCP proxy |
| **Chaos Toolkit** | Framework + DSL | Python, extensible |

## Litmus Chaos on Kubernetes

### Install

```bash
kubectl apply -f https://raw.githubusercontent.com/litmuschaos/litmus/master/litmus-2.15.x/litmus-namespace.yaml
kubectl apply -f https://raw.githubusercontent.com/litmuschaos/litmus/master/litmus-2.15.x/litmus-operator.yaml
```

### Pod-delete experiment

```yaml
apiVersion: litmuschaos.io/v1alpha1
kind: ChaosEngine
metadata:
  name: orders-api-pod-delete
  namespace: orders
spec:
  appinfo:
    appns: orders
    applabel: "app=orders-api"
    appkind: deployment
  engineState: active
  chaosServiceAccount: litmus-admin
  experiments:
    - name: pod-delete
      spec:
        components:
          env:
            - { name: TOTAL_CHAOS_DURATION, value: "60" }
            - { name: CHAOS_INTERVAL, value: "20" }
            - { name: FORCE, value: "false" }
            - { name: PODS_AFFECTED_PERC, value: "33" }
            - { name: TARGET_CONTAINER, value: "api" }
```

```bash
kubectl apply -f chaos-engine.yaml
kubectl describe chaosengine orders-api-pod-delete -n orders
kubectl get chaosresult -n orders -w
```

Abort:

```bash
kubectl patch chaosengine orders-api-pod-delete -n orders \
  --type merge -p '{"spec":{"engineState":"stop"}}'
```

## Chaos Mesh

```yaml
apiVersion: chaos-mesh.org/v1alpha1
kind: PodChaos
metadata:
  name: orders-api-pod-kill
  namespace: orders
spec:
  action: pod-kill
  mode: fixed-percent
  value: "33"
  selector:
    namespaces: [orders]
    labelSelectors: { app: orders-api }
  scheduler:
    cron: "@every 1h"
```

Network chaos:

```yaml
apiVersion: chaos-mesh.org/v1alpha1
kind: NetworkChaos
metadata: { name: db-latency, namespace: orders }
spec:
  action: delay
  mode: all
  selector:
    namespaces: [orders]
    labelSelectors: { app: orders-api }
  delay:
    latency: "300ms"
    jitter: "30ms"
  direction: to
  target:
    selector:
      namespaces: [data]
      labelSelectors: { app: postgres }
    mode: all
```

## toxiproxy (network faults without K8s)

```bash
toxiproxy-cli create -l 0.0.0.0:22222 -u postgres:5432 db-proxy
toxiproxy-cli toxic add db-proxy -t latency -a latency=300 -a jitter=30

# Run load test / observe metrics
# ...

toxiproxy-cli toxic remove db-proxy -n latency_downstream
```

## AWS FIS

```yaml
# Roll back 50% of an ASG
Resources:
  Experiment:
    Type: AWS::FIS::ExperimentTemplate
    Properties:
      Description: Terminate 50% of orders-api ASG
      Targets:
        asgTargets:
          ResourceType: aws:ec2:autoscaling-group
          ResourceTags: { app: orders-api }
          SelectionMode: PERCENT(50)
      Actions:
        terminate:
          ActionId: aws:ec2:terminate-instances
          Parameters: {}
          Targets: { Instances: asgTargets }
      StopConditions:
        - { Source: aws:cloudwatch:alarm, Value: !Ref HighErrorRateAlarm }
      RoleArn: !GetAtt FISRole.Arn
```

## Game Days

A scheduled practice session where the team runs chaos experiments and rehearses incident response.

### Plan

```yaml
gameday:
  date: "2024-04-15"
  duration: "2 hours"
  participants: [SRE, backend, on-call rotation, IC shadow]

  objectives:
    - Test incident response procedures
    - Validate monitoring + alerting fires correctly
    - Practice communication protocols
    - Identify gaps in runbooks

  scenarios:
    - scenario: Database primary failover
      inject: terminate primary DB pod
      expected: auto-failover in <30s, <1% errors
      rollback: restore primary, redirect traffic

    - scenario: API overload
      inject: generate 10x normal traffic
      expected: rate-limiting activates, no 5xx cascade
      rollback: stop load generator

    - scenario: Network partition (API → DB)
      inject: NetworkChaos deny egress
      expected: circuit breaker opens, graceful 503
      rollback: delete NetworkChaos resource

  success_criteria:
    - All scenarios handled without escalation
    - MTTR < 30 min for all scenarios
    - Every action item documented
    - Runbook gaps filed as tickets

  safety:
    - Run in staging first; prod canary only after staging success
    - VP Eng notified beforehand
    - Abort plan documented per scenario
    - Customer support on standby
```

### Run

1. **Pre-brief** — 30 min: walk through scenarios, abort plans, roles (IC, comms, scribe, SMEs).
2. **Execute** — Each scenario 15-20 min: inject → observe → rollback → debrief.
3. **Debrief** — 30 min: what worked, what didn't, action items.

### After

- Within 48h: write up findings + action items
- Update runbooks with new learnings
- Schedule next game day (quarterly)

## Safety Checklist (Every Experiment)

- [ ] Steady state defined and verified before injecting
- [ ] Blast radius capped (env, %, duration)
- [ ] Automated rollback tested BEFORE experiment
- [ ] Single variable changed (one failure at a time)
- [ ] Manual kill switch available ( Slack command, kubectl patch )
- [ ] Observability dashboard live during experiment
- [ ] Runbook for the failure mode exists
- [ ] Approval documented (per blast-radius policy)
- [ ] On-call aware (or shadowing) during prod experiments
- [ ] Post-experiment learning summary scheduled

## Chaos Maturity Model

| Level | Practice |
|---|---|
| 0 | No chaos testing |
| 1 | Ad hoc — occasional manual experiments |
| 2 | Scheduled — regular game days |
| 3 | Continuous — automated in CI/CD |
| 4 | Cultural — chaos embedded in dev workflow |

Most organizations should aim for level 2-3. Level 4 (continuous automated chaos in production) requires significant investment.

## Common Pitfalls

1. **No steady state defined** — can't tell if experiment "succeeded"
2. **Blast radius too big first time** — start at dev/staging, expand
3. **No rollback tested** — chaos goes wrong, no fast recovery
4. **Running chaos on unmonitored systems** — you won't learn anything
5. **Doing it once** — chaos is a practice, not a one-time event
6. **Surprising on-call** — always notify; better, run during business hours with team
7. **No learning summary** — experiment is wasted if not documented
8. **Running in prod without canary** — always do staging first
9. **Multiple faults at once** — single-variable experiments only
10. **Treating it as a bug hunt** — chaos validates resilience; use testing for bugs

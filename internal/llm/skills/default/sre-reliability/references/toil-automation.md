# Toil Reduction, Automation, Capacity Planning

## What Is Toil?

> "Operational work that is manual, repetitive, automatable, tactical, devoid of enduring value, and that scales linearly as a service grows." — SRE Book

Six traits (any one is enough to call it toil):
1. **Manual** — done by a human, not a script
2. **Repetitive** — same task recurring
3. **Automatable** — could be scripted but isn't
4. **Tactical** — reactive, no enduring value
5. **No enduring value** — done, then forgotten
6. **Scales linearly with growth** — 2x traffic → 2x toil

## Track Toil

Track toil hours per team per sprint. Target: **<50% of team time**. Above 50% → automation sprint.

```python
from dataclasses import dataclass
from datetime import date

@dataclass
class SprintReport:
    team: str
    sprint_start: date
    sprint_end: date
    total_engineer_hours: float
    toil_hours: float
    feature_hours: float
    incident_hours: float

    @property
    def toil_percentage(self) -> float:
        return self.toil_hours / self.total_engineer_hours * 100

# Example
report = SprintReport(
    team="orders",
    sprint_start=date(2024, 3, 1),
    sprint_end=date(2024, 3, 14),
    total_engineer_hours=640,
    toil_hours=350,      # too high
    feature_hours=200,
    incident_hours=90,
)
print(f"Toil: {report.toil_percentage:.0f}%")  # 55% — over threshold
```

Categories to track:
- Manual deployments (count → automate via CI/CD)
- Manual restarts (count → automate auto-remediation)
- Manual scaling (count → autoscaling)
- Manual log investigation (hours → improve dashboards/alerts)
- Manual runbook steps that don't auto-execute (count → make runbook executable)
- Hand-rolled one-off scripts (count → productize)

## Automation Patterns

### Level 0: Manual

Human does everything by hand via console/CLI.

### Level 1: External script

A human runs a script that does part of the task. Script is in source control.

```bash
# restart-unhealthy-pods.sh — runs manually
kubectl get pods -n production --field-selector=status.phase!=Running -o name | xargs kubectl delete -n production
```

### Level 2: Scheduled script (cron)

Script runs on a schedule. Human monitors output.

```yaml
# cronjob.yaml
apiVersion: batch/v1
kind: CronJob
metadata: { name: cleanup-old-pvcs, namespace: ops }
spec:
  schedule: "0 2 * * *"   # daily 2am
  jobTemplate:
    spec:
      template:
        spec:
          serviceAccountName: pvc-cleaner
          restartPolicy: OnFailure
          containers:
            - name: cleanup
              image: bitnami/kubectl:1.30
              command: ["/bin/sh", "-c"]
              args:
                - |
                  kubectl get pvc --all-namespaces -o json |
                    jq -r '.items[] | select(.status.phase == "Bound" and (.metadata.annotations.stale|tonumber < now - 86400)) | "\(.metadata.namespace) \(.metadata.name)"' |
                    while read ns name; do
                      kubectl delete pvc "$name" -n "$ns"
                    done
```

### Level 3: Triggered automation

Automation runs on event (alert firing, alertmanager webhook, GitHub PR).

```yaml
# Alertmanager → webhook → remediation
receivers:
  - name: auto-remediate-pod-crashloop
    webhook_configs:
      - url: http://remediator.ops.svc.cluster.local/remediate
        send_resolved: true
```

```python
# remediator.py (FastAPI)
from fastapi import FastAPI, Request
import subprocess

app = FastAPI()

@app.post("/remediate")
async def remediate(request: Request):
    alert = await request.json()
    for a in alert.get("alerts", []):
        if a["labels"].get("alertname") == "PodCrashLooping":
            namespace = a["labels"]["namespace"]
            pod = a["labels"]["pod"]
            # Restart the parent deployment
            deploy = subprocess.run(
                ["kubectl", "get", "pod", pod, "-n", namespace,
                 "-o", "jsonpath={.metadata.ownerReferences[0].name}"],
                capture_output=True, text=True
            ).stdout
            subprocess.run(["kubectl", "rollout", "restart", f"deployment/{deploy}", "-n", namespace])
            return {"status": "restarted", "deployment": deploy}
    return {"status": "no action"}
```

### Level 4: Controller/operator (declarative, reconciles)

The K8s-native way. Operator watches resources and reconciles.

Examples: HPA (autoscaling), External Secrets Operator, Cluster Autoscaler. You write your own operator for domain-specific automation (see cloud-native/operators-crds.md).

### Level 5: Autonomous (no human in the loop)

System self-heals without human awareness. Requires:
- Well-tested automation
- Tight safety bounds (auto-rollback, blast radius limits)
- Observability so humans can audit post-hoc

Most teams stop at Level 3 or 4. Level 5 is the aspirational goal.

## What to Automate First

Rank toil by:
1. **Frequency** — done weekly > done monthly
2. **Time per occurrence** — 4-hour task > 5-minute task
3. **Risk of human error** — destructive ops > safe ops
4. **Stability of the procedure** — same steps every time > varies

Top targets usually:
- Deployments (CI/CD — done first)
- Scaling (HPA/KEDA — done early)
- Restart of unhealthy pods (auto-remediation)
- Log investigation (better dashboards + alerts)
- Backups (scripted + monitored)
- Certificate rotation (cert-manager)
- DB migrations (Helm hooks / job)
- User onboarding (Terraform / scripts)

## Auto-Remediation Examples

### Restart unhealthy pods via alert webhook

```yaml
# Prometheus alert
- alert: PodHighRestartRate
  expr: rate(kube_pod_container_status_restarts_total[15m]) * 60 * 12 > 0
  for: 10m
  labels: { severity: warning, auto_remediate: "restart-deployment" }
  annotations:
    namespace: "{{ $labels.namespace }}"
    deployment: "{{ $labels.deployment }}"
```

Webhook receiver forwards to a remediation service that calls `kubectl rollout restart`.

### Auto-scale on queue depth

KEDA ScaledObject (see cloud-native/kubernetes-workloads.md) — no human action needed.

### Auto-rollback on SLO breach

Argo Rollouts / Flagger (see cloud-native/gitops.md) automatically roll back canaries when error rate exceeds threshold.

## Capacity Planning

### Forecast inputs

1. **Historical growth** — last 12 months of monthly peak QPS, CPU, memory, storage
2. **Business projections** — marketing launches, user growth forecasts, new markets
3. **Headroom target** — typically 30-50% buffer above forecasted peak
4. **Lead time** — how long to provision new capacity (1 day for autoscale, 6 weeks for hardware)

### Forecasting model

```python
from datetime import date, timedelta
from dataclasses import dataclass

@dataclass
class CapacityPlan:
    service: str
    current_qps_peak: float           # current peak QPS
    monthly_growth_rate: float         # e.g., 0.05 for 5%/month
    forecast_months: int
    headroom_factor: float             # e.g., 1.30 for 30% headroom
    provision_lead_time_weeks: int     # weeks to add capacity

    def forecast_qps(self, months_ahead: int) -> float:
        """Project QPS N months out."""
        return self.current_qps_peak * (1 + self.monthly_growth_rate) ** months_ahead

    def capacity_needed(self, months_ahead: int) -> float:
        """QPS capacity needed (with headroom) N months out."""
        return self.forecast_qps(months_ahead) * self.headroom_factor

    def when_to_order(self) -> date:
        """When to start provisioning to meet 6-month forecast by lead time."""
        target_capacity = self.capacity_needed(self.forecast_months)
        # Find earliest month where current capacity is exceeded
        # Subtract lead time
        ...

plan = CapacityPlan(
    service="orders-api",
    current_qps_peak=10000,         # 10k peak QPS
    monthly_growth_rate=0.07,       # 7%/month
    forecast_months=6,
    headroom_factor=1.30,
    provision_lead_time_weeks=2,    # 2 weeks to add capacity (autoscale + node provision)
)
print(f"6-month forecast: {plan.forecast_qps(6):.0f} QPS")
print(f"Capacity needed:  {plan.capacity_needed(6):.0f} QPS")
```

### Per-resource capacity

| Resource | Metric | Action trigger |
|---|---|---|
| CPU | `rate(container_cpu_usage_seconds_total)` > 70% sustained | Add pods (HPA) or nodes |
| Memory | `container_memory_working_set_bytes` > 80% | Add pods or increase limit |
| Disk | `node_filesystem_avail_bytes` < 20% | Add storage or clean up |
| Network | bandwidth > 70% NIC capacity | Upgrade NIC or distribute load |
| DB connections | `pg_stat_activity` > 80% of pool | Increase pool size or scale DB |
| Queue depth | backlog growing sustained | Scale consumers |

### Capacity review cadence

- **Weekly**: top-3 services growth vs forecast (15 min)
- **Monthly**: full capacity review per service (1 hour per service)
- **Quarterly**: cross-service capacity plan for next quarter (half day)

## Runbook-as-Code

Move runbooks from wikis into executable automation.

### Pattern: Runbook in YAML, executed by script

```yaml
# runbooks/restart-deployment.yaml
name: Restart Deployment
description: Rollout restart a deployment to recover from issues
trigger: PodCrashLooping, HighMemoryUsage
steps:
  - name: identify-deployment
    type: shell
    cmd: "kubectl get pod {{ pod_name }} -n {{ namespace }} -o jsonpath='{.metadata.ownerReferences[0].name}'"
    output: deployment_name
  - name: rollout-restart
    type: shell
    cmd: "kubectl rollout restart deployment/{{ deployment_name }} -n {{ namespace }}"
  - name: verify
    type: shell
    cmd: "kubectl rollout status deployment/{{ deployment_name }} -n {{ namespace }} --timeout=5m"
```

Execute via `runbook-exec runbooks/restart-deployment.yaml --set namespace=orders --set pod_name=orders-api-abc123`.

Tools: Coder, Saltstack, or home-grown. Goal: every runbook step is a script, not prose.

## Toil Budget per Engineer

SRE principle: each SRE spends **≤50% on toil, ≥50% on engineering work** (code, design, automation). Above 50% toil → SRE team stops taking new operational work and does an automation sprint.

Track at the team and individual level. If sustained > 50%, escalate to management — that's a hiring or priority signal.

## Common Pitfalls

1. **Automating without observability** — automation fails silently. Always emit metrics + log.
2. **Auto-remediation with no safety bounds** — script restarts all pods in an infinite loop. Cap rate, require human ack after N retries.
3. **Manual capacity planning** — spreadsheets go stale. Pull from metrics directly.
4. **No headroom target** — service hits limit and falls over on first unexpected spike.
5. **Forecast based on average, not peak** — averages hide spikes. Plan for peak × growth.
6. **Runbook in wiki only** — out of date; no one reads; no one executes. Move to code.
7. **Treating toil reduction as low priority** — compounds; team burns out.
8. **Hiring more ops people instead of automating** — linear cost; doesn't scale.
9. **No toil metric** — can't manage what you don't measure. Track hours per sprint.
10. **Forgetting to retire automations** — old scripts that no one remembers still run, sometimes break.

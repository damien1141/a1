# SLO, SLI, SLA, Error Budgets

## Definitions

| Term | Definition |
|---|---|
| **SLI** (Service Level Indicator) | Quantitative measure of service behavior (e.g., 200-OK ratio, p99 latency) |
| **SLO** (Service Level Objective) | Target for an SLI over a window (e.g., 99.9% availability over 30d) |
| **SLA** (Service Level Agreement) | Contractual consequence of missing SLO (e.g., refund, credit) — looser than SLO |
| **Error budget** | `1 - SLO target`. The allowed unreliability you can "spend" |

```
SLA ≤ SLO ≤ actual reliability
   99%    99.9%     99.95%
```

SLA is what customers can sue over. SLO is what engineering commits to. Error budget is the buffer between SLO and SLA.

## SLI Definition Patterns

### Request-based availability

```python
def availability_sli(good_requests: int, total_requests: int) -> float:
    """Proportion of successful requests.

    "Good" = HTTP 2xx + 4xx (client errors aren't the service's fault).
    """
    if total_requests == 0:
        return 1.0
    return good_requests / total_requests

# SLO: 99.9% of requests succeed over 30d
# Error budget: 0.1% of requests can fail
```

### Latency-based

```python
def latency_sli(histogram: dict, threshold_ms: float) -> float:
    """Proportion of requests faster than threshold."""
    fast = sum(c for bucket, c in histogram.items() if bucket <= threshold_ms)
    total = sum(histogram.values())
    return fast / total if total > 0 else 1.0

# SLO: 99% of requests complete in < 500ms
# Error budget: 1% of requests can be slow
```

### Window-based vs request-based

| Type | Definition | When |
|---|---|---|
| Request-based | good_events / total_events | Most user-facing APIs |
| Window-based | count_of_good_windows / total_windows | Long-running streams, batch |

Request-based is preferred — it directly measures user experience.

## Picking SLIs

Use **user journey** SLIs, not infrastructure SLIs.

| Bad (infra) | Good (user) |
|---|---|
| CPU utilization < 80% | 99% of `/checkout` requests complete in < 1s |
| Memory available > 20% | 99.9% of `/api/auth` returns 2xx |
| Disk IOPS within limits | 99% of webhook deliveries arrive < 5s |

Use the **four golden signals** as inspiration: latency, traffic, errors, saturation.

## SLO Targets by Tier

| Tier | Availability SLO | Latency SLO | Allowed downtime/30d | Example |
|---|---|---|---|---|
| 1 (critical) | 99.99% | p99 < 100ms | 4m 19s | Auth, payments |
| 2 (important) | 99.9% | p99 < 500ms | 43m 11s | User profile, search |
| 3 (standard) | 99.5% | p99 < 1s | 3h 36m | Internal dashboards |

Rule of thumb: each "9" costs roughly 10x more to achieve. Don't set 99.99% if business doesn't justify it.

## Error Budget Math

```python
from datetime import timedelta

class SLOTarget:
    def __init__(self, target: float, window: timedelta):
        self.target = target          # 0.999 for 99.9%
        self.window = window          # 30 days

    @property
    def error_budget(self) -> float:
        return 1 - self.target        # 0.001

    @property
    def allowed_downtime(self) -> timedelta:
        return timedelta(seconds=self.window.total_seconds() * self.error_budget)

slo = SLOTarget(0.999, timedelta(days=30))
print(slo.error_budget)        # 0.001 (0.1%)
print(slo.allowed_downtime)    # 43m 12s
```

### Request-based budget

```
Monthly requests: 10,000,000
SLO: 99.9% → error budget = 10,000 failed requests

If week 1 sees 5,000 errors → 50% budget burned in 25% of window
→ Trigger: feature freeze policy
```

## Multi-Window Burn Rate (Google SRE)

Single-window alerts are noisy or slow. Multi-window catches both fast and slow burns:

```
fast_burn: 2% of monthly budget in 1 hour
  → burn rate = (30d / 1h) * 2% = 720 * 0.02 = 14.4
slow_burn: 5% of monthly budget in 6 hours
  → burn rate = (30d / 6h) * 5% = 120 * 0.05 = 6
very slow: 10% of monthly budget in 3 days
  → burn rate = (30d / 3d) * 10% = 10 * 0.10 = 1
```

To reduce false positives, pair long + short window: **both must be above threshold simultaneously**.

| Alert | Long window | Short window | Burn rate | Action |
|---|---|---|---|---|
| Fast (page) | 1h | 5m | 14.4 | Page on-call |
| Medium (page) | 6h | 30m | 6 | Page on-call |
| Slow (ticket) | 3d | 6h | 1 | Create Jira ticket |

### Prometheus rule (Sloth-generated pattern)

```yaml
groups:
  - name: slo-availability-99.9
  rules:
    # Page: 2% budget in 1h (requires 1h AND 5m both firing)
    - alert: SLOAvailabilityFastBurn
      expr: |
        (
          (sum(rate(http_requests_total{job="api",status=~"5.."}[1h]))
           / sum(rate(http_requests_total{job="api"}[1h]))) > (14.4 * 0.001)
        )
        and
        (
          (sum(rate(http_requests_total{job="api",status=~"5.."}[5m]))
           / sum(rate(http_requests_total{job="api"}[5m]))) > (14.4 * 0.001)
        )
      for: 2m
      labels: { severity: page, slo: availability-99.9 }
      annotations:
        summary: "Fast burn: api consuming 14.4x error budget"
        runbook: "https://runbooks/api/slo-burn"

    # Ticket: 10% budget in 3d
    - alert: SLOAvailabilitySlowBurn
      expr: |
        (
          (sum(rate(http_requests_total{job="api",status=~"5.."}[3d]))
           / sum(rate(http_requests_total{job="api"}[3d]))) > (1 * 0.001)
        )
        and
        (
          (sum(rate(http_requests_total{job="api",status=~"5.."}[6h]))
           / sum(rate(http_requests_total{job="api"}[6h]))) > (1 * 0.001)
        )
      for: 30m
      labels: { severity: ticket, slo: availability-99.9 }
      annotations:
        summary: "Slow burn: api consuming 1x error budget sustained"
```

## Error Budget Policy

```yaml
service: orders-api
slo: { target: 99.9%, window: 30d }

policy:
  - threshold: 100%   # healthy
    state: normal
    actions:
      - Continue feature development
      - Standard deploy cadence

  - threshold: 50%    # warning
    state: careful
    actions:
      - Require senior approval for deploys
      - Pre-deploy risk assessment
      - Enhanced monitoring during deploy

  - threshold: 25%    # critical
    state: restricted
    actions:
      - Halt non-critical feature work
      - Reliability improvements prioritized
      - VP approval for deploys
      - Daily error budget review

  - threshold: 0%     # exhausted
    state: freeze
    actions:
      - Feature freeze
      - Emergency fixes only
      - Mandatory postmortem for all incidents
      - Weekly exec review

exceptions:
  - type: security_patch
    approval: security_team
  - type: critical_business
    approval: vp_eng + product_lead
```

## Decision Framework: Should We Deploy?

```python
def should_deploy(
    budget_remaining: float,    # 0.0 to 1.0
    change_risk: str,           # low | medium | high
    business_priority: str,    # low | medium | high | critical
) -> tuple[bool, str]:
    if budget_remaining <= 0:
        if business_priority == "critical":
            return True, "Critical need; budget exhausted"
        return False, "Budget exhausted; feature freeze"

    if budget_remaining < 0.25:  # critical
        if change_risk == "high":
            return False, "High risk + critical budget"
        if business_priority in ("high", "critical"):
            return True, "High priority + critical budget; careful"
        return False, "Critical budget; defer non-essential"

    if budget_remaining < 0.75:  # warning
        if change_risk == "high" and business_priority == "low":
            return False, "High risk + low priority + warning budget"
        return True, "Approved with enhanced review"

    return True, "Normal operations; budget healthy"
```

## Tools

| Tool | Purpose |
|---|---|
| **Prometheus + PromQL** | Metric collection + alert expressions |
| **Sloth** | Generate SLO alert rules from YAML spec |
| **Pyrra** | Kubernetes SLO operator; auto-creates alerts + dashboards |
| **OpenSlo** | Vendor-neutral SLO spec |
| **Nobl9** | Commercial SLO platform |
| **Grafana OnCall** | Incident scheduling + escalation |

### Sloth example

```yaml
# slo.yml
version: "prometheus/v1"
service: "orders-api"
slos:
  - name: "availability"
    objective: 99.9
    sli:
      events:
        error_query: sum(rate(http_requests_total{job="orders-api",status=~"5.."}[{{.window}}]))
        total_query: sum(rate(http_requests_total{job="orders-api"}[{{.window}}]))
    alerting:
      name: OrdersApiAvailability
      page_alert: { labels: { severity: page } }
      ticket_alert: { labels: { severity: ticket } }
```

```bash
sloth generate -i slo.yml -o alerts.yml
promtool check rules alerts.yml
```

## SLI Review Checklist

Before locking in an SLO:

1. **User-centric** — measures user-visible impact, not internal infra
2. **Achievable** — current architecture can hit it (check last 90d actual)
3. **Measurable** — SLI query is unambiguous and stable
4. **Meaningful** — violating it means users are suffering
5. **Documented** — calculation agreed by eng + product + SRE
6. **Budgeted** — error budget policy exists and is enforced
7. **Reviewed** — quarterly review; tighten if consistently beat, loosen if consistently miss

## Common Pitfalls

1. **100% SLO** — impossible; you have no error budget, can never deploy.
2. **SLI measures infra, not users** — "CPU < 80%" tells you nothing about user impact.
3. **Single-window alerts** — too noisy (1m) or too slow (1h). Always use multi-window.
4. **No error budget policy** — SLO becomes a vanity metric; teams deploy through burn.
5. **SLO too high** — 99.99% on single-AZ service = fiction. Match architecture.
6. **Alert on raw counters** — `errors_total > 100` is meaningless without rate + total.
7. **Cardinality explosion** — `http_requests_total{user_id="..."}` will OOM Prometheus. Keep label cardinality < 10 per label, total active series < few million.
8. **Forgetting to revisit** — SLOs should evolve. Quarterly review.

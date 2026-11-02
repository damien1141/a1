---
name: sre-reliability
description: Designs and operates reliable systems using SLO/SLI/SLA, error budgets, blameless incident response, observability (Prometheus, Grafana, OpenTelemetry, structured logging, distributed tracing), and chaos engineering (game days, fault injection, steady-state hypotheses). Use for setting SLOs, multi-window burn-rate alerts, incident commander role, postmortems, capacity planning, and resilience validation. Produces decision tables, Prometheus rules validated by promtool, and chaos experiment manifests.
license: MIT
metadata:
  author: super-skills
  version: "2.0.0"
  domain: devops
  triggers: SRE, site reliability, SLO, SLI, SLA, error budget, burn rate, incident response, incident commander, postmortem, blameless, on-call, MTTR, MTBF, observability, Prometheus, Grafana, OpenTelemetry, OTel, distributed tracing, structured logging, chaos engineering, game day, fault injection, steady state, blast radius, Litmus, Chaos Mesh, toil, runbook
  role: specialist
  scope: implementation
  output-format: code
  related-skills: cloud-native, infrastructure-as-code, debugging-wizard, system-architecture
---

# SRE & Reliability

## When to Use

- Defining SLOs/SLIs and error budgets for a new service
- Setting up multi-window burn-rate alerting with Prometheus
- Running incident response: incident commander, comms lead, on-call
- Writing blameless postmortems with action items
- Building observability: metrics (Prometheus), logs (Loki/ELK), traces (Tempo/Jaeger) via OpenTelemetry
- Designing chaos experiments (game days, fault injection, steady-state hypotheses)
- Reducing toil via automation and capacity planning

## Operating Loop

1. **Define SLOs** — pick user-facing SLIs (latency, availability), set targets per service tier, write the error budget policy that governs behavior at 50% / 25% / 0% remaining.
2. **Instrument** — emit Prometheus metrics, structured JSON logs with request IDs, OpenTelemetry traces. Verify scrape is up before proceeding.
3. **Alert** — multi-window multi-burn-rate alerts (1h/5m, 6h/30m, 3d/6h). Validate rules with `promtool check rules`. Every alert must have a runbook.
4. **Respond** — detect → ack → declare incident → assign IC + comms lead → investigate → mitigate → resolve. Time-box decisions; favor mitigation over root cause during incident.
5. **Postmortem** — blameless, within 48h. Identify contributing causes, action items with owners and due dates, what went well / poorly / lucky.
6. **Test resilience** — quarterly game days with steady-state hypotheses, blast radius caps, and automated rollback ≤ 30s.
7. **Reduce toil** — track toil % per team; automate anything done >5x/quarter. Target <50% toil.

### Validation Gates

| Gate | Command | When |
|---|---|---|
| Prometheus rules | `promtool check rules alerts.yml` | Pre-merge |
| Prometheus config | `promtool check config prometheus.yml` | Pre-deploy |
| OTel collector | `otelcol --dry-run --config otel-collector.yaml` | Pre-deploy |
| Dashboard JSON | `gcloud monitoring dashboards validate` or Grafana API lint | Pre-merge |
| Alerting policy | review every alert has runbook URL | Quarterly |
| Chaos experiment | dry-run injection on staging; verify rollback ≤ 30s | Before each game day |
| SLO review | `sloctl verify` / Sloth validate | Per SLO change |
| Incident-readiness | quarterly unannounced game day | Quarterly |

## Reference Guide

| Topic | Reference | Load When |
|---|---|---|
| SLO/SLI/SLA, error budgets, multi-window burn-rate, decision policy | `references/slo-error-budgets.md` | Setting SLOs, designing alerts |
| Observability: Prometheus metrics, Grafana dashboards, OTel traces, structured logs | `references/observability.md` | Instrumenting a service |
| Alerting rules + incident response (IC role, postmortems, blameless) | `references/alerting-incident-response.md` | Authoring alerts, running incidents |
| Chaos engineering: game days, fault injection, steady-state hypotheses, Litmus/Chaos Mesh | `references/chaos-engineering.md` | Resilience testing |
| Toil reduction, automation patterns, capacity planning | `references/toil-automation.md` | Operational maturity, scaling |

## Constraints

### MUST DO
- Define quantitative SLOs (e.g., 99.9% availability) tied to user-facing impact, never to internal infrastructure metrics alone.
- Calculate error budget = `1 - SLO`. Track consumption; freeze feature work when budget exhausted.
- Alert on **burn rate** (multi-window), not on absolute thresholds — protects against noisy single-window alerts.
- Every alert MUST have a runbook URL and an actionable remediation step.
- Run incident response with an explicit Incident Commander role; IC does not debug, they coordinate.
- Write blameless postmortems within 48h of every SEV1/SEV2 incident; assign action items with owners + due dates.
- Emit the four golden signals (latency, traffic, errors, saturation) for every user-facing service.
- Use structured JSON logs with request IDs / trace IDs for correlation.
- Sample traces in production (e.g., 1-10%) but always keep error traces.
- Cap chaos experiment blast radius; require automated rollback tested before injection.

### MUST NOT DO
- Set SLOs above what the architecture supports (99.99% on a single-AZ service is fiction).
- Alert on raw counter values (`http_requests_total > 1000`) — always use `rate()` and ratios.
- Alert on CPU > 80% alone without checking user impact (saturation, not utilization, is the golden signal).
- Tolerate >50% toil without an automation plan.
- Skip postmortems or assign blame to individuals ("Alice deployed the bad code").
- Run chaos experiments in production without safety nets (feature flags, canary isolation, circuit breakers).
- Treat dashboards as alerts — dashboards are for humans looking; alerts are for waking humans up.
- Use multiple metrics backends; pick one (Prometheus + OTel collector) and centralize.
- Mix cardinality-explosive labels in Prometheus (user_id, request_id, IP) — they will OOM your Prometheus.

## Code Examples

### Multi-window burn-rate alert (full YAML in `references/slo-error-budgets.md`)

```yaml
groups:
  - name: slo_availability
    rules:
      - alert: HighErrorBudgetBurn           # fast burn: 2% budget in 1h → page
        expr: |
          (sum(rate(http_requests_total{job="orders-api",status=~"5.."}[1h]))
            / sum(rate(http_requests_total{job="orders-api"}[1h]))) > 14.4 * 0.001
          and
          (sum(rate(http_requests_total{job="orders-api",status=~"5.."}[5m]))
            / sum(rate(http_requests_total{job="orders-api"}[5m]))) > 14.4 * 0.001
        for: 2m
        labels: { severity: critical, service: orders-api }
        annotations:
          summary: "Fast burn: orders-api burning 14.4x error budget"
          runbook: "https://runbooks.example.com/orders-api/high-error-burn"
```

Validate: `promtool check rules alerts.yml` then `promtool test rules alerts_test.yml`.

### PromQL — golden signals (full queries in `references/observability.md`)

```promql
# Latency p99
histogram_quantile(0.99, sum(rate(http_request_duration_seconds_bucket[5m])) by (le, route))
# Error ratio
sum(rate(http_requests_total{status=~"5.."}[5m])) / sum(rate(http_requests_total[5m]))
# Saturation: CPU throttling ratio
sum(rate(container_cpu_cfs_throttled_seconds_total[5m])) / sum(rate(container_cpu_cfs_periods_total[5m]))
```

### Blameless postmortem (full template in `references/alerting-incident-response.md`)

Sections required: Summary, Impact (duration, % users, error-budget consumed), Timeline (UTC), Root Cause (no blame), Resolution, What went well/poorly/lucky, Action Items with owners + due dates. Written within 48h, blameless, action items reviewed weekly until closed.

## Output Template

```
## SRE & Reliability Plan

### SLO definition
- Service: <name>
- Tier: <1/2/3>
- Availability SLO: <99.9%>  → error budget: <43.2 min/30d>
- Latency SLO: <p99 < 500ms>
- Window: <30d>

### SLIs (Prometheus queries)
- availability: sum(rate(http_requests_total{status=~"2..|4.."}[5m])) / sum(rate(http_requests_total[5m]))
- latency p99: histogram_quantile(0.99, ...)

### Alerts (validated)
- HighErrorBudgetBurn (1h + 5m, 14.4x) — severity:critical — runbook: <url>
- SlowErrorBudgetBurn (3d + 6h, 1x)    — severity:warning  — runbook: <url>
- promtool check rules ✓

### Incident response
- IC rotation: <team>
- Pager: <PagerDuty schedule>
- War room: <Zoom link / Slack channel>
- Status page: <url>

### Observability
- Metrics: Prometheus + Grafana
- Logs: <Loki/ELK> with request_id + trace_id
- Traces: OTel → <Tempo/Jaeger> at 5% sampling, 100% errors

### Chaos engineering
- Next game day: <date>
- Hypothesis: <steady-state claim>
- Blast radius: <staging | prod 1% canary>
- Rollback: <method, <30s tested>

### Verified vs Assumed
- VERIFIED: promtool check rules passes; OTel pipeline live; runbook URLs resolve
- ASSUMED: <list any RTO/RPO claims not yet drilled>
```

## Knowledge Reference

- Google SRE Workbook (Beyer, Murphy, Rensin) — multi-window burn rate formula
- Prometheus 2.x + PromQL; `promtool` for rule + config validation
- Grafana 10+, Loki, Tempo, Mimir
- OpenTelemetry 1.x SDK + Collector; OTLP protocol
- Pyrra (SLO operator for Kubernetes), Sloth (Prometheus SLO generator)
- Litmus Chaos, Chaos Mesh, Gremlin, AWS FIS, Chaos Monkey
- Incident Command System (ICS) — adapted from firefighting
- Blameless postmortem culture (Etsy, 2012)
- Toil: "operational work that is manual, repetitive, automatable, tactical, devoid of enduring value, and that scales linearly as a service grows" (SRE Book)

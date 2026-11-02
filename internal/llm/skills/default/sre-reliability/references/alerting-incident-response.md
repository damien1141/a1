# Alerting & Incident Response

## Alert Design

### Every alert must be:

1. **Actionable** — there's a concrete runbook step
2. **Specific** — names the affected service/route/region
3. **User-impact-focused** — alert on SLO burns, not raw infra metrics
4. **Routed** — goes to the team that can fix it
5. **Recoverable** — has a `runbook_url` annotation

Bad alert: `errors_total > 0` (always fires, no action)
Good alert: `SLOAvailabilityFastBurn` (page on-call, runbook, mitigate)

### Severity levels

| Severity | Response time | Example |
|---|---|---|
| **critical (page)** | Immediate, 24/7 | Service down, data loss, SLO fast-burn |
| **warning (ticket)** | Next business day | Slow burn, single AZ degraded, disk > 80% |
| **info** | Weekly review | Unusual traffic pattern, low-priority metric drift |

Rule: only **critical** pages humans. Anything else is a ticket.

## Prometheus Alert Rules

```yaml
# alerts.yml
groups:
  - name: slo.alerts
    rules:
      # Fast burn — page
      - alert: SLOAvailabilityFastBurn
        expr: |
          (
            (sum(rate(http_requests_total{job="orders-api",status=~"5.."}[1h]))
             / sum(rate(http_requests_total{job="orders-api"}[1h]))) > 0.0144
          )
          and
          (
            (sum(rate(http_requests_total{job="orders-api",status=~"5.."}[5m]))
             / sum(rate(http_requests_total{job="orders-api"}[5m]))) > 0.0144
          )
        for: 2m
        labels: { severity: critical, service: orders-api, slo: availability-99.9 }
        annotations:
          summary: "orders-api error budget burning 14.4x"
          runbook: "https://runbooks.example.com/orders-api/high-burn"
          dashboard: "https://grafana.example.com/d/orders-api"

  - name: infra.alerts
    rules:
      - alert: PodCrashLooping
        expr: rate(kube_pod_container_status_restarts_total[15m]) > 0
        for: 5m
        labels: { severity: warning }
        annotations:
          summary: "Pod {{ $labels.namespace }}/{{ $labels.pod }} crash-looping"

      - alert: DiskSpaceLow
        expr: |
          (node_filesystem_avail_bytes{fstype!~"tmpfs|overlay"}
            / node_filesystem_size_bytes{fstype!~"tmpfs|overlay"}) < 0.10
        for: 5m
        labels: { severity: critical }
        annotations:
          summary: "Disk <10% free on {{ $labels.instance }} {{ $labels.mountpoint }}"
          runbook: "https://runbooks.example.com/infra/disk-full"

      - alert: CertExpiringSoon
        expr: |
          cert_manager_certificate_expiration_timestamp_seconds - time() < 1209600  # 14 days
        labels: { severity: warning }
        annotations:
          summary: "Certificate {{ $labels.name }} in {{ $labels.namespace }} expires <14d"
```

Validate:

```bash
promtool check rules alerts.yml
```

### Unit testing alert rules

```yaml
# alerts_test.yml
rule_files:
  - alerts.yml
tests:
  - interval: 1m
    input_series:
      - series: 'http_requests_total{job="orders-api",status="500"}'
        values: '0+10x60'   # 10/s for 60 minutes
      - series: 'http_requests_total{job="orders-api",status="200"}'
        values: '0+1000x60'
    alert_rule_test:
      - eval_time: 10m
        alertname: SLOAvailabilityFastBurn
        exp_alerts:
          - exp_labels: { severity: critical, service: orders-api, slo: availability-99.9 }
            exp_annotations:
              summary: "orders-api error budget burning 14.4x"
```

```bash
promtool test rules alerts_test.yml
```

## Alertmanager Routing

```yaml
# alertmanager.yml
global:
  slack_api_url: 'https://hooks.slack.com/services/...'
  pagerduty_url: 'https://events.pagerduty.com/v2/enqueue'

route:
  receiver: 'slack-default'
  group_by: ['alertname', 'service', 'severity']
  group_wait: 30s         # batch alerts for 30s
  group_interval: 5m      # then send group updates every 5m
  repeat_interval: 4h     # re-notify every 4h if still firing

  routes:
    - matchers: ['severity="critical"']
      receiver: 'pagerduty'
      group_wait: 0s
      repeat_interval: 30m

    - matchers: ['severity="warning"']
      receiver: 'slack-warnings'
      group_wait: 5m

    - matchers: ['alertname=~"Watchdog|InfoInhibitor"']
      receiver: 'null'

receivers:
  - name: 'null'

  - name: 'pagerduty'
    pagerduty_configs:
      - service_key: '${PD_KEY}'
        severity: '{{ .CommonLabels.severity }}'
        description: '{{ .CommonAnnotations.summary }}'

  - name: 'slack-warnings'
    slack_configs:
      - channel: '#alerts-warn'
        send_resolved: true
        title: '[{{ .Status }}] {{ .CommonLabels.alertname }}'
        text: '{{ .CommonAnnotations.summary }}\nRunbook: {{ .CommonAnnotations.runbook }}'

  - name: 'slack-default'
    slack_configs:
      - channel: '#alerts'
        send_resolved: true

inhibit_rules:
  # Don't page about pod-level alerts if the node is down
  - source_matchers: ['alertname="NodeDown"']
    target_matchers: ['alertname="PodDown"']
    equal: ['node']
```

Validate:

```bash
amtool check-config alertmanager.yml
```

## Alert Fatigue — Death by 1000 Pages

Symptoms: on-call ignores pages, alert volume > 5/page/day, no runbook → "it'll auto-resolve".

Remedies:

1. **Audit every alert** quarterly — does it have a runbook? Did someone take action when it fired last month?
2. **Auto-suppress dependent alerts** — node-down inhibits pod-down (see inhibit_rules above)
3. **SLO-based alerts only** — stop alerting on individual metrics; alert on burn rate
4. **Tick silence during deploys** — use Alertmanager silences via deploy tooling
5. **Track page volume per shift** — target <2 pages/shift; >5 = broken alerting

## Incident Response — Incident Commander Role

### Roles

| Role | Responsibility |
|---|---|
| **Incident Commander (IC)** | Coordinates response; doesn't debug. Makes decisions. |
| **Comms Lead** | Posts updates to status page, customer comms, Slack |
| **Scribe** | Documents timeline, decisions, action items |
| **Subject Matter Expert (SME)** | Investigates and remediates; reports to IC |
| **On-call engineer** | First responder; may become SME after IC assigned |

### Severity

| Sev | Definition | Response |
|---|---|---|
| **SEV1** | Total outage / data loss / safety | Page VP Eng; IC = staff SRE; updates every 15 min |
| **SEV2** | Partial outage / significant impact | Page on-call + team lead; updates every 30 min |
| **SEV3** | Degraded performance / minor impact | On-call handles; updates hourly |
| **SEV4** | Nuisance / no user impact | Fix during business hours |

### Lifecycle

```
Detect → Ack → Declare → Investigate → Mitigate → Resolve → Postmortem
```

1. **Detect** — Alert fires / customer reports / monitoring
2. **Acknowledge** — On-call acks within target (e.g., 5 min for SEV1)
3. **Declare** — On-call decides severity; opens incident channel; assigns IC
4. **Investigate** — SMEs gather data, form hypothesis, test mitigation
5. **Mitigate** — Stop the bleeding. Rollback, scale up, failover, feature-flag off. Don't fix root cause now.
6. **Resolve** — Confirm metrics back to normal; monitor 30 min; close incident
7. **Postmortem** — Within 48h; blameless; assign action items

### Decision framework during incident

- **Stabilize first, root-cause later.** If you can mitigate in 5 min by rollback, do that. Diagnose after.
- **Time-box hypotheses.** "I'll try X for 5 minutes" — then re-evaluate.
- **Prefer reversible actions.** Rollback is reversible. Schema migration is not.
- **Escalate early.** If you don't know in 15 min, page the next tier.

### Runbook template

```markdown
# Runbook: HighErrorBudgetBurn (orders-api)

## Symptom
SLOAvailabilityFastBurn alert firing for orders-api

## Quick Diagnosis (≤5 min)
1. Check Grafana: https://grafana.example.com/d/orders-api
2. Look at error rate, latency p99, traffic
3. Check deploy history: `kubectl rollout history deploy/orders-api -n orders`
4. Check DB connections: `kubectl exec -n data postgres-0 -- psql -c "SELECT count(*) FROM pg_stat_activity"`

## Common causes & mitigations

### Bad deploy (most common)
- Rollback: `kubectl rollout undo deploy/orders-api -n orders`
- Verify: `kubectl rollout status deploy/orders-api -n orders`

### DB connection pool exhaustion
- Scale up: `kubectl scale deploy/orders-api -n orders --replicas=10`
- Check pool metrics in Grafana
- File P0 to add pool monitoring

### Downstream dependency down
- Check status of: auth-service, payment-service, inventory-service
- Failover if possible; otherwise wait

### Traffic spike
- Check if expected (marketing campaign?) or attack
- HPA should auto-scale; verify `kubectl get hpa -n orders`
- If attack: enable rate-limit at ingress

## Escalation
- Primary on-call: PagerDuty schedule "orders-primary"
- Secondary: PagerDuty schedule "orders-secondary"
- SME: #orders-team Slack
- IC rotation: see https://go.example.com/ic-rotation
```

## Blameless Postmortems

### Culture rules

- **Blameless** — focus on systems, not people. "The deploy lacked a canary" not "Alice deployed wrong".
- **Assume good intent** — everyone was doing their best with the info they had.
- **Look for systemic fixes** — tooling, process, automation, not "be more careful next time".
- **Action items have owners + dates** — without an owner, it won't get done.

### Template

```markdown
# Postmortem: <Incident Title>

**Date:** YYYY-MM-DD
**Severity:** SEV<N>
**Authors:** @handles
**Status:** Complete
**Action items complete:** 0/N

## Summary
One paragraph: what happened, impact, resolution.

## Impact
- Duration: <X min>
- Users affected: <N or %>
- Revenue impact: <$/delayed>
- Error budget consumed: <X% of monthly>

## Timeline (all UTC)
| Time | Event |
|------|-------|
| HH:MM | Deploy v1.2.0 |
| HH:MM | Error rate begins rising |
| HH:MM | Alert fires |
| HH:MM | On-call ack |
| HH:MM | Incident declared SEV<N> |
| HH:MM | Root cause identified |
| HH:MM | Mitigation applied |
| HH:MM | Resolved |

## Root Cause
Technical explanation of WHY (not WHO). Include code/config snippets.

## Resolution
What was done to stop the bleeding. Note: this may not be the long-term fix.

## What went well
- Alert fired in 5 min
- Runbook helped quick diagnosis
- Rollback procedure worked first try

## What went poorly
- Issue not caught in staging
- No monitoring for <specific metric>
- Decision-making was slow

## Where we got lucky
- Issue occurred during low-traffic window
- Only one service affected
- Backup was current

## Action items
| Action | Owner | Priority | Due | Status |
|--------|-------|----------|-----|--------|
| Add connection pool metrics | @alice | P0 | 2024-03-20 | Open |
| Extend staging load test | @bob | P1 | 2024-03-25 | Open |
| Audit retry/cleanup paths | @charlie | P1 | 2024-03-30 | Open |

## Appendix
- Logs: <link>
- Dashboards: <link>
- Related incidents: <link>
```

### Postmortem review meeting

- Held within 1 week of incident
- All action items reviewed weekly until closed
- Quarterly review of all postmortems — patterns? recurring themes?

## On-Call Hygiene

| Practice | Target |
|---|---|
| Pages per shift | <2 |
| Pages per week per primary | <5 |
| False-positive rate | <20% |
| Mean time to ack (MTTA) | <5 min for SEV1, <15 for SEV2 |
| Mean time to resolve (MTTR) | <30 min SEV1, <2h SEV2 |
| Follow-the-sun rotation | 8-12h shifts max |
| Post-shift cooldown | 12h min between shifts |
| Unblame reviews | Every quarter |

## Common Pitfalls

1. **Alert on utilization, not saturation** — CPU 80% may be fine; queue depth 1000 is not.
2. **No runbook URL** — on-call wastes time Googling.
3. **Alert spam during deploys** — silence proactively, or set maintenance windows.
4. **IC debugging instead of coordinating** — IC must step back, assign SMEs.
5. **Root-cause during incident** — fix later; mitigate now.
6. **Blame in postmortem** — kills psychological safety; people hide issues next time.
7. **Action items without owners/dates** — die on the vine.
8. **No follow-up on action items** — same incident recurs in 6 months.
9. **Single on-call** — no escalation path; burnout.
10. **On-call also building features** — split role; on-call is a job.

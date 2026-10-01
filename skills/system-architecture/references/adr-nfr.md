# ADRs + NFR Checklist + System Design Template

Architecture Decision Records (ADRs), Non-Functional Requirements (NFRs) checklist, and a system design template. Use these to document decisions explicitly and avoid "we forgot to consider X" failures.

## ADR — Architecture Decision Record

An ADR is a short markdown document capturing one architectural decision. Store them in `docs/adr/` as `0001-short-title.md`, numbered sequentially.

### Template

```markdown
# ADR-0001: Use PostgreSQL for Order Storage

## Status
Accepted          # Proposed | Accepted | Deprecated | Superseded by ADR-0007

## Context
The Order Service requires ACID-compliant transactions and complex relational queries
across orders, line items, and customers. Expected scale: 10K orders/day, 100M rows
in `order_items` over 3 years.

## Decision
Use PostgreSQL 16 as the primary datastore for the Order Service.

## Alternatives Considered
- **MySQL 8** — equivalent ACID, slightly weaker for complex queries; team less familiar.
- **MongoDB** — flexible schema, but lacks strong cross-document transactions; would
  require denormalization for our query patterns.
- **DynamoDB** — excellent scalability, but complex query patterns require denormalization
  and GSIs that don't match our access patterns.

## Consequences
- **Positive:** Strong consistency, mature tooling, complex query support, JSONB for
  semi-structured metadata, partial indexes for query optimization.
- **Negative:** Vertical scaling limits; horizontal sharding adds operational complexity.
  Need connection pooling (PgBouncer) and read replicas for read-heavy workloads.
- **Neutral:** Team needs Postgres training (some members are MySQL-focused).

## Trade-offs
Consistency and query flexibility prioritized over unlimited horizontal write scalability.
If we exceed single-node write capacity, we'll shard by `customer_id` (ADR-0007 will
cover sharding strategy when needed).

## Compliance
- [ ] Decision reviewed by: Architecture review board (date)
- [ ] NFR impact assessed: latency, throughput, RPO/RTO (see NFR-001)
- [ ] Migration plan documented (if replacing existing system)
```

### ADR rules

- One decision per ADR. If you find yourself writing "and also...", split it.
- ADRs are immutable once `Accepted`. To change a decision, write a new ADR that says `Supersedes ADR-0001`.
- ADRs are short — 1-2 pages. If longer, you're writing a design doc, not an ADR.
- ADRs are versioned in the same repo as code. PR that implements the decision includes the ADR.
- ADRs are reviewed by humans (architecture review) — not auto-approved.
- Number sequentially; never reuse numbers.

### When to write an ADR

| Decision type | ADR? |
|---|---|
| New database / data store | Yes |
| New framework / library | Yes (if it's load-bearing) |
| Service boundary change | Yes |
| Auth scheme | Yes |
| Migration strategy (legacy) | Yes |
| Coding style (tabs vs spaces) | No (use lint config) |
| Minor library version bump | No (use changelog) |
| Refactor within a service | No (use PR description) |
| New API endpoint | No (use OpenAPI spec) |

## NFR checklist

Non-functional requirements are the "ilities": latency, throughput, availability, security, etc. Gather these BEFORE designing — they constrain the architecture.

### Performance

| NFR | Question | Metric |
|---|---|---|
| Latency | p50 / p99 / p99.9 for key operations | ms |
| Throughput | Requests/sec sustained peak | RPS |
| Concurrency | Concurrent users / connections | count |
| Data volume | Rows / GB today and in 3 years | rows / GB |
| Batch size | Largest single request/response | MB |

### Availability

| NFR | Question | Metric |
|---|---|---|
| Uptime | Required availability | % (e.g., 99.9%) |
| RTO | Recovery time objective (max downtime) | minutes |
| RPO | Recovery point objective (max data loss) | seconds |
| Maintenance window | Allowed downtime for deploys | minutes/month |
| Failover | Multi-AZ? Multi-region? | yes/no |

### Scalability

| NFR | Question | Metric |
|---|---|---|
| Growth | Expected 10× in what timeframe | months/years |
| Scale axis | Horizontal (more instances) or vertical (bigger boxes)? | both? |
| Bottleneck | DB? CPU? Network? | identified? |
| Sharding | Required? When? | yes/no + trigger |

### Security & compliance

| NFR | Question | Metric |
|---|---|---|
| Auth | Single tenant? Multi-tenant? SSO? | list |
| Encryption | At rest? In transit? Field-level? | list |
| Compliance | SOC 2? HIPAA? PCI-DSS? GDPR? | list |
| Audit | Required log retention | years |
| PII | What data is collected? Where stored? | list |

### Operability

| NFR | Question | Metric |
|---|---|---|
| Deployment | Blue/green? Canary? Rolling? | strategy |
| Rollback | Time to rollback | minutes |
| Observability | Logs + metrics + traces? | list |
| On-call | 24/7? Business hours? | schedule |
| Runbooks | Per-service runbooks? | yes/no |

### Cost

| NFR | Question | Metric |
|---|---|---|
| Monthly cloud cost | Target | $/month |
| Per-user cost | Target | $/user/month |
| Cost ceiling | Hard cap | $/month |
| Cost growth | Linear with scale? Sublinear? | ratio |

### Internationalization

| NFR | Question | Metric |
|---|---|---|
| Languages | Required locales | list |
| Time zones | Multi-TZ? | yes/no |
| Currencies | Multi-currency? | list |
| Data residency | EU only? US only? | list |

Capture NFRs in a `docs/nfr/` document, referenced by ADRs. NFRs that aren't measured aren't real — every NFR needs a metric and a target.

## System design template

Use for any new system or major refactor. Output: a single design doc that a teammate can read in 30 minutes and understand the system.

```markdown
# Design: <system name>

## 1. Context (2-3 paragraphs)
- Why are we building this? What problem does it solve?
- Who are the users / customers?
- What's the current state (if replacing something)?

## 2. Requirements
### Functional
- <list of user-facing capabilities>

### Non-functional (link to docs/nfr/NFR-001.md)
- Latency: p99 < 200ms for read, < 500ms for write
- Throughput: 1000 RPS sustained
- Availability: 99.9% (43min/month downtime)
- RPO: < 1 min; RTO: < 15 min

### Constraints
- Must run on AWS (org policy)
- Must use existing SSO (Okta)
- Team size: 4 engineers

### Out of scope
- <explicit list of what we're NOT building>

## 3. Architecture
### Diagram (Mermaid)
\`\`\`mermaid
graph TD
    Client[Client] --> Gateway[API Gateway]
    Gateway --> AuthSvc[Auth Service]
    Gateway --> OrderSvc[Order Service]
    OrderSvc --> DB[("Orders DB\n(PostgreSQL)")]
    OrderSvc --> Queue["Message Queue\n(Kafka)"]
    Queue --> NotifySvc[Notification Service]
\`\`\`

### Components
| Component | Responsibility | Tech | Scale |
|---|---|---|---|
| API Gateway | Auth, rate limit, routing | Kong | 10K RPS |
| Order Service | Order CRUD, saga orchestration | FastAPI + Postgres | 1000 RPS, 4 instances |
| Notification Service | Email/push delivery | Python + Celery | 100 jobs/sec |

### Data model
- Order Service owns `orders`, `order_items`, `order_events` (Postgres).
- Notification Service owns `notifications`, `delivery_attempts` (Postgres).
- No shared databases.

### Communication
- Client → Gateway: REST (HTTPS).
- Gateway → Services: REST (HTTPS) or gRPC.
- Order → Notification: async via Kafka topic `order-events`.
- Cross-service queries: not allowed; use materialized views or saga.

## 4. Key decisions (ADRs)
- ADR-0001: Postgres for Order storage
- ADR-0002: Kafka for async messaging
- ADR-0003: Saga orchestration in Order Service

## 5. Failure modes + mitigations
| Failure | Impact | Mitigation |
|---|---|---|
| Order DB down | Cannot create/modify orders | Read replica for reads; fail fast on writes; RTO 15min via PITR |
| Kafka down | Notifications delayed | Queue in-process (limited); alert; replay from outbox when Kafka recovers |
| Notification Service down | Emails not sent | Retry with exponential backoff; dead-letter queue; manual replay tool |
| Auth Service down | No logins | Cache JWT validation 5 min; degrade to read-only mode |

## 6. Observability
- Metrics: RED per endpoint; USE per DB; business metrics (orders/min, $/min).
- Logs: structured JSON, correlation ID, shipped to Loki.
- Traces: OpenTelemetry, sampling 10% in prod (100% in dev).
- Dashboards: Grafana per service; SLO dashboard at org level.
- Alerts: PagerDuty for SLO burn; Slack for warnings.

## 7. Security
- Auth: OAuth2 + JWT (15min access, 7d refresh).
- Authz: per-resource owner check; role-based for admin endpoints.
- Secrets: AWS Secrets Manager; rotated quarterly.
- Network: VPC, security groups, no public DB access.
- Audit: all auth events logged; 1-year retention.

## 8. Deployment
- Container: Docker, ECR.
- Orchestration: ECS Fargate (or EKS).
- CI/CD: GitHub Actions → build → test → deploy to staging → manual approve → prod.
- Rollback: 1-click, < 5min.

## 9. Migration plan (if replacing legacy)
- Phase 1 (week 1-2): Strangler fig facade; new endpoints in dark launch.
- Phase 2 (week 3): 5% traffic canary.
- Phase 3 (week 4): 25% traffic.
- Phase 4 (week 5): 50% traffic.
- Phase 5 (week 6): 100% traffic; legacy retained for 1 release cycle.
- Phase 6 (week 8): Retire legacy.

## 10. Open questions
- <things we don't know yet; each has an owner + due date>

## 11. Risks
- <things that could go wrong; each has a mitigation>
```

## Verification gates for the design doc

- [ ] All NFRs have a metric and a target.
- [ ] ADRs written for every significant decision.
- [ ] Failure modes section covers each external dependency.
- [ ] Observability section covers metrics + logs + traces.
- [ ] Security section addresses all 12 checklist items (see `references/security-checklist.md`).
- [ ] Migration plan (if applicable) has rollback triggers per phase.
- [ ] Reviewed by: at least one architect, one SRE, one security engineer.
- [ ] Open questions have owners and due dates.

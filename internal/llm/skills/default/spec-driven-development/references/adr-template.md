# ADR Template

Architecture Decision Records. Lightweight, versioned log of decisions that future maintainers would otherwise have to reverse-engineer from git archaeology and folklore.

## When to write an ADR

| Situation | Write an ADR? |
|-----------|---------------|
| Non-obvious technical decision with real trade-offs | **Yes** |
| Decision that future maintainers will question ("why didn't they just…") | **Yes** |
| Choice between two viable architectures | **Yes** |
| Decision that's hard to reverse (data model, auth scheme) | **Yes** |
| Convention adopted ("we use tabs not spaces") | Maybe — only if the convention is non-default and the rationale isn't obvious |
| Obvious decision with no real alternatives | No |
| Decision forced by external constraint ("AWS only") | No — record the constraint, not the decision |
| Decision that just follows the framework default | No |

Rule of thumb: if a future engineer would ask "why?", write an ADR. If the answer is "because that's the default," skip it.

## ADR format

```markdown
# ADR-{number}: {Title}

## Status
[Proposed | Accepted | Deprecated | Superseded by ADR-XXX]

## Context
[Describe the situation and forces at play. What is the problem?
What constraints exist? What are we trying to achieve?]

## Decision
[State the decision clearly. What are we going to do?]

## Consequences

### Positive
- [Benefit 1]
- [Benefit 2]

### Negative
- [Drawback 1]
- [Drawback 2]

### Neutral
- [Side effect that is neither good nor bad]

## Alternatives Considered
[What other options were evaluated and why were they rejected?
Be fair — steelman the alternatives before rejecting them.]

## References
- [Link to relevant documentation]
- [Link to discussion/RFC]
```

## Worked example — database selection

```markdown
# ADR-001: Use PostgreSQL for primary database

## Status
Accepted (2025-01-15)

## Context
We need a relational database for the e-commerce platform that:
- Handles complex transactions with strong consistency (orders, payments, inventory)
- Supports JSON for flexible product attributes (catalog grows monthly)
- Scales to millions of products and orders over the next 3 years
- Works well with our existing Python/Node stack (drivers, ORM support)
- Has managed-service options so we don't run our own DB ops

The team has experience with PostgreSQL and MySQL. Budget allows for a managed
database service (AWS RDS or equivalent). Launch is in 6 months; we cannot
afford a database re-platform in year 1.

## Decision
Use PostgreSQL as the primary database, hosted on AWS RDS (Multi-AZ).

## Consequences

### Positive
- ACID compliance for financial transactions (orders, payments)
- Rich feature set: JSONB for flexible catalog, full-text search, CTEs, window functions
- Strong community and tooling (pgAdmin, psql, every ORM supports it)
- Excellent performance with proper indexing and connection pooling
- Free and open source; no per-CPU licensing
- RDS Multi-AZ gives us automated failover and backups out of the box
- Team already knows PostgreSQL

### Negative
- Vertical scaling has limits; we'll need read replicas by year 2
- Requires DBA expertise for query optimization and VACUUM tuning
- RDS costs scale with instance size; high-availability instances are 2x base
- Major version upgrades require planned downtime (RDS blue/green helps but is not zero-downtime)

### Neutral
- Team will need to learn PostgreSQL-specific features (extensions, partitioning)
- Migration from current SQLite dev database needed (low risk, dev-only)

## Alternatives Considered

**MySQL 8**
- Rejected: Less feature-rich for JSON operations (JSONB in Postgres is more mature)
- Considered: Similar cost, familiar to half the team, slightly simpler ops
- Would have been acceptable if not for the catalog flexibility requirement

**MongoDB**
- Rejected: Relational data model needed for orders/inventory; transactions in
  MongoDB are now supported but the relational fit is poor
- Considered: Excellent for flexible product catalog (the original JSON argument)
- Re-evaluate if we ever split catalog into its own service

**CockroachDB**
- Rejected: Higher cost, team unfamiliar, overkill for our scale (we don't need
  multi-region active-active yet)
- Considered: Better horizontal scaling, PostgreSQL-compatible wire protocol
- Re-evaluate when we hit multi-region requirements

**SQLite (production)**
- Rejected: No concurrent write throughput for our workload; no managed-service option
- Considered: Lowest ops burden, perfect for dev (still using it for local dev)

## References
- PostgreSQL docs: https://www.postgresql.org/docs/current/
- AWS RDS PostgreSQL: https://aws.amazon.com/rds/postgresql/
- Internal RFC: Database Selection for E-commerce Platform (#rfc-42)
- Team experience survey: results in #db-selection Slack thread
```

## Worked example — architecture decision

```markdown
# ADR-002: Extract payments into a separate service

## Status
Accepted (2025-02-01)

## Context
The monolith's `payments` module is the source of 60% of production incidents
(see incident log Q4 2024). Triggers:
- Deploys of unrelated features take the payment flow down (shared process)
- PCI compliance scope grows with every new endpoint added to the monolith
- The payment team (3 engineers) is blocked on the platform team (5 engineers)
  for every deploy

We need to either: (a) keep payments in the monolith and add module-level
isolation, or (b) extract payments into a service owned by the payment team.

## Decision
Extract payments into a standalone service (`payments-svc`) owned by the
payment team. Communicate with the monolith via synchronous REST for now;
migrate to async events for non-critical paths in Q3.

## Consequences

### Positive
- Independent deploy cadence for payment fixes (currently blocked by monolith deploy window)
- PCI scope reduced to the new service; monolith can be de-scoped from PCI audits
- Clearer ownership: payment team owns payment code end-to-end
- Can scale payment service independently during peak (holiday sales)

### Negative
- One more service to operate (deploy, monitor, on-call rotation)
- Distributed transactions: order creation + payment capture are now across services
  → use saga pattern with compensating actions (see ADR-003 when written)
- Network calls add latency (estimated +20ms p95 for the order flow)
- Schema coordination: payments-svc owns its schema, but order service needs to
  reference payment IDs — versioned contracts required

### Neutral
- Payment team will need to learn their own deploy pipeline (currently monolith's)
- On-call expansion: payment team joins the global rotation

## Alternatives Considered

**Keep in monolith, add module isolation (feature flags + deploy gates)**
- Rejected: Doesn't solve PCI scope or deploy independence
- Considered: Lower operational cost, no distributed transaction complexity
- Would have been acceptable if PCI scope weren't the driving factor

**Extract as a library (shared package)**
- Rejected: Doesn't solve deploy independence or PCI scope; just moves the code
- Considered: Lowest operational cost
- Not a real alternative — it's the current state, repackaged

**Extract to a managed payment provider (Stripe, Adyen)**
- Rejected: We already use Stripe for processing; the question is about our
  integration code, not the processor
- Considered: Offloads more to vendor
- Re-evaluate if Stripe offers a product that replaces our integration entirely

## References
- Strangler fig pattern: docs/architecture/strangler-fig.md
- PCI scope reduction analysis: docs/compliance/pci-scope-q4-2024.md
- Saga pattern: https://learn.microsoft.com/en-us/azure/architecture/reference-architectures/saga/saga
- Incident log Q4 2024: docs/incidents/Q4-2024-summary.md
```

## Naming and storage

```
docs/
└── adr/
    ├── 0001-use-postgresql-database.md
    ├── 0002-extract-payments-service.md
    ├── 0003-event-sourcing-for-audit.md
    ├── 0004-deprecate-legacy-users-api.md
    └── README.md   # index with titles, statuses, last-updated dates
```

Numbering is sequential and never reused. A superseded ADR stays in place with `Status: Superseded by ADR-0007` and a one-line note; the new ADR links back.

## ADR README index

```markdown
# Architecture Decision Records

| # | Title | Status | Date |
|---|-------|--------|------|
| 0001 | Use PostgreSQL for primary database | Accepted | 2025-01-15 |
| 0002 | Extract payments into separate service | Accepted | 2025-02-01 |
| 0003 | Event sourcing for audit trail | Proposed | 2025-02-20 |
| 0004 | Deprecate legacy users-api | Superseded by 0006 | 2025-03-01 |
| 0005 | Adopt OpenTelemetry for tracing | Accepted | 2025-03-10 |
| 0006 | Migrate users-api to users-v2 (GraphQL) | Accepted | 2025-03-15 |
```

## Writing discipline

- **Steelman the alternatives.** A reader should finish "Alternatives Considered" thinking the rejected options were reasonable. A weakly-argued rejection signals a weakly-considered decision.
- **Name the negative consequences.** An ADR with no negatives is suspicious — every real decision trades something off. If you can't name a negative, you haven't thought hard enough.
- **Cite the trigger.** "Context" should explain *why now*. A decision without a trigger is a solution looking for a problem.
- **Date and author.** Future readers will want to know who to ask and when this was made. Stale ADRs are still useful; undated ADRs are archaeology.
- **Update, don't rewrite.** If the decision evolves, write a new ADR that supersedes the old one. Don't silently edit the old ADR to match current reality — that erases the decision history.

## When ADRs become anti-patterns

- Writing an ADR for every trivial choice ("ADR-0042: We use 2 spaces in JSON"). Reserve ADRs for decisions with real trade-offs.
- ADRs that are really RFCs in disguise. An RFC proposes; an ADR records. If the decision isn't made yet, it's an RFC, not an ADR.
- ADR graveyards — 200 ADRs, none superseded, half the systems described no longer exist. Prune (mark Deprecated/Superseded) regularly.
- ADRs as gatekeeping — requiring an ADR for every PR. The ADR is for architectural decisions, not code changes.

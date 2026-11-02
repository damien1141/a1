---
name: system-architecture
description: Senior architect for DDD (bounded contexts, aggregates), microservices (boundaries, saga, outbox, CQRS), legacy modernization (strangler fig, event interception), and fullstack security checklists. Use when decomposing a monolith, designing distributed systems, migrating legacy code, or running a security review across the stack. Decision tables over prose.
license: MIT
metadata:
  author: super-skills
  version: "2.0.0"
  domain: architecture
  triggers: DDD, bounded context, aggregate, microservices, service boundary, saga, outbox, CQRS, event sourcing, strangler fig, legacy modernization, monolith decomposition, fullstack security, ADR, NFR, system design
  role: specialist
  scope: implementation
  output-format: code
  related-skills: api-design, node-backend, python-backend, database-pro, cloud-native
---

# System Architecture

Designs distributed systems using DDD (bounded contexts, aggregates), microservices patterns (saga, outbox, CQRS), strangler-fig legacy modernization, and fullstack security checklists. Ships ADRs, decision tables, and Mermaid diagrams. Favors decision tables over prose.

## When to Use

Decomposing a monolith (DDD bounded contexts, service boundaries); distributed transactions (saga orchestration/choreography, outbox, CQRS, event sourcing); legacy modernization (strangler fig, branch by abstraction, characterization tests); architecture review (ADRs, NFRs, tech selection); fullstack security review (auth, authz, validation, encoding across layers).

## Operating Loop

1. **Gather requirements** — functional + NFRs (latency, throughput, availability, RPO/RTO, compliance). Use the NFR checklist.
2. **Map the domain** — event storming; identify bounded contexts and aggregates.
3. **Decide architecture** — apply the Decision Tables (monolith vs microservices, sync vs async, ACID vs eventual).
4. **Document decisions** — ADRs for every significant choice; explicit trade-offs.
5. **Design resilience** — circuit breakers, retries, timeouts, bulkheads, idempotency for every external call.
6. **Plan migration** (if legacy) — strangler fig, characterization tests, traffic shift 5% → 25% → 50% → 100%.
7. **Verify gates** — ADR review, dependency map confirmed, NFR targets met by load test, security checklist signed off.

## Decision Table — Monolith vs Microservices

| Need | Pick | Why | Reject |
|---|---|---|---|
| Single team, < 10 engineers, < 50K LOC | **Modular monolith** | velocity, no distributed-systems tax | Microservices (overhead > benefit) |
| Multiple teams, independent deploy cycles | **Microservices** | team autonomy, deploy isolation | Monolith (merge contention, deploy coupling) |
| Sub-100ms p99 latency for cross-feature calls | **Monolith** | in-process call ≪ network call | Microservices (network hops add 5-50ms each) |
| Polyglot stacks (Go + Python + Java) | **Microservices** | language freedom per service | Monolith (one language) |
| Independent scaling per feature | **Microservices** | scale hot paths only | Monolith (scale whole app) |
| Unclear domain boundaries | **Monolith first** | let boundaries emerge; decompose later | Microservices (premature decomposition = distributed monolith) |
| Hard SLA on a single feature | **Microservices** | isolate blast radius, scale independently | Monolith (one bug takes down everything) |

**Default rule:** Start modular monolith. Decompose when (a) > 1 team needs independent deploy, (b) a feature needs different scale/sla/stack, or (c) domain boundaries are stable and well-understood. Decomposing too early is more expensive than staying monolithic.

## Decision Table — Communication

| Need | Pick | Why |
|---|---|---|
| Query, sub-100ms SLA, both services up | **Sync (REST/gRPC)** | simple, immediate response |
| Long-running operation, fire-and-forget | **Async (message broker)** | decouples uptime, retries, replay |
| Cross-aggregate transaction | **Saga** (orchestrated or choreographed) | eventual consistency with compensations |
| Cross-service data sync (read model) | **Event-driven + CQRS** | projections from event stream |
| Real-time push to client | **WebSocket / SSE** | see api-design skill |
| Batch data exchange | **Scheduled ETL / file** | simplest when latency OK |

## Decision Table — Data Consistency

| Need | Pick |
|---|---|
| ACID across multiple writes in one service | **DB transaction** (single service, single DB) |
| Consistency across services | **Saga** with compensating transactions |
| Read model separated from write model | **CQRS** (project from events) |
| Audit log of all state changes | **Event sourcing** (events as source of truth) |
| Reliable event publication | **Outbox pattern** (events written in same tx as state) |
| Cache invalidation across services | **Event-driven invalidation** (publish `cache-invalidate` events) |

## Reference Guide

| Topic | Reference | Load When |
|---|---|---|
| DDD — bounded contexts, aggregates, value objects, domain events | `references/ddd.md` | Modeling domains, identifying boundaries, aggregates |
| Microservices — boundaries, saga, outbox, CQRS, event sourcing | `references/microservices.md` | Distributed transactions, service decomposition, CQRS |
| Legacy modernization — strangler fig, characterization tests | `references/legacy-modernization.md` | Migrating legacy systems, branch by abstraction |
| Fullstack security checklist — auth, authz, validation, encoding | `references/security-checklist.md` | Security review across DB, API, frontend |
| ADRs + NFR checklist + system design template | `references/adr-nfr.md` | Documenting decisions, gathering non-functional requirements |

## Constraints

### MUST DO
- Document every significant decision with an ADR (Context, Decision, Alternatives, Consequences).
- Each microservice owns its data exclusively — no shared databases.
- Every cross-service call has: explicit timeout, retry budget with jitter, circuit breaker, fallback.
- Propagate correlation IDs across all service boundaries (HTTP header + message header).
- Saga steps define `execute()` AND `compensate()` — automatic rollback on failure.
- Outbox: write events in the same DB transaction as state change; separate process publishes them.
- Health probes split: `/health/live` (process up) vs `/health/ready` (downstream deps OK).
- Strangler fig migrations use feature flags + traffic shift; rollback trigger per phase.
- Characterization tests (golden master) of legacy behavior before any code change — target 80%+ coverage.
- Security checklist signed off before every release: input validation, output encoding, authn/authz, parameterized queries, secrets management.

### MUST NOT DO
- Create a distributed monolith — services that share a database, deploy together, or cannot fail independently.
- Use synchronous calls for long-running operations (blocks resources, couples uptime).
- Share database schemas between services — even read-only views couple deployments.
- Big-bang rewrites — always strangler fig; never "stop the world" for migration.
- Skip distributed tracing — a single request must be traceable end-to-end via correlation ID.
- Make breaking proto/API changes without versioning and deprecation window.
- Trust client-side validation alone — server-side validation is the only gate.
- Store secrets in source, env files committed to repo, or unencrypted config.

## Code Examples

### Saga orchestration (TypeScript)

```ts
interface SagaStep<T> {
  execute(ctx: T): Promise<T>;
  compensate(ctx: T): Promise<void>;
}

async function runSaga<T>(steps: SagaStep<T>[], initial: T): Promise<T> {
  const completed: SagaStep<T>[] = [];
  let ctx = initial;
  for (const step of steps) {
    try { ctx = await step.execute(ctx); completed.push(step); }
    catch (err) {
      for (const done of completed.reverse()) await done.compensate(ctx).catch(console.error);
      throw err;
    }
  }
  return ctx;
}

// Usage
const orderSaga = [reserveInventoryStep, chargePaymentStep, scheduleShipmentStep];
await runSaga(orderSaga, { orderId, customerId, items });
```

### Outbox pattern (SQL + worker)

```sql
-- In the same transaction as the business state change
BEGIN;
INSERT INTO orders (id, customer_id, total) VALUES ($1, $2, $3);
INSERT INTO outbox (id, aggregate_type, aggregate_id, event_type, payload)
  VALUES (gen_random_uuid(), 'Order', $1, 'OrderPlaced', $4::jsonb);
COMMIT;

-- Separate process polls outbox and publishes to broker
SELECT id, event_type, payload FROM outbox WHERE published_at IS NULL LIMIT 100 FOR UPDATE SKIP LOCKED;
-- after publish: UPDATE outbox SET published_at = now() WHERE id = ANY($1);
```

### Strangler fig facade (Python)

```python
class OrderServiceFacade:
    def __init__(self):
        self._legacy = LegacyOrderService()
        self._new = NewOrderService()

    def get_order(self, order_id: str):
        if flag_enabled("USE_NEW_ORDER_SERVICE"):
            return self._new.fetch(order_id)
        return self._legacy.get(order_id)
```

## Output Template

1. **Architecture summary** — context, NFRs, constraints.
2. **Decision table** — monolith vs microservices; sync vs async; consistency model — applied with rationale.
3. **Diagram** — Mermaid graph of services, data stores, message brokers, gateways.
4. **ADRs** — one ADR per significant decision (Context, Decision, Alternatives, Consequences).
5. **Resilience plan** — timeouts, retries, circuit breakers, fallbacks per integration point.
6. **Security checklist** — signed off; input validation, authz, encoding, secrets all addressed.
7. **Migration plan** (if legacy) — phases, traffic shift, rollback triggers, characterization tests.

Separate VERIFIED (ADR reviewed, NFR load-tested, security checklist signed) from ASSUMED (cost at scale, future team scaling).

## Knowledge Reference

DDD (bounded contexts, aggregates, entities, value objects, domain events, event storming); microservices (database per service, saga, outbox, CQRS, event sourcing, circuit breakers, bulkheads, retries with jitter); strangler fig, branch by abstraction, characterization tests, feature flags, canary deploy; ADRs, NFRs (latency, throughput, availability, RPO/RTO), Mermaid; CAP/PACELC, eventual consistency; OpenTelemetry, correlation IDs; OWASP Top 10, input validation, output encoding, parameterized queries, secrets management.

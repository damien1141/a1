# Microservices — Boundaries, Saga, Outbox, CQRS, Event Sourcing

Distributed systems patterns: service boundaries, inter-service communication, distributed transactions (saga), reliable event publication (outbox), and read/write separation (CQRS / event sourcing).

## Database per service

Each service owns its data exclusively. No shared databases, no shared schemas, no shared tables.

| Pattern | Implementation | When |
|---|---|---|
| Separate instances | One Postgres per service | Production; full isolation |
| Separate schemas | One Postgres, separate schemas | Dev/test; lower cost |
| Separate databases on one server | Logical separation | Small scale; same DB engine |

Rules: other services access data only via the owning service's API (no direct DB reads); service chooses its own DB tech; shared DB = distributed monolith (coupled deployments, no independent scaling).

## Service boundaries

Boundaries align with bounded contexts (see `references/ddd.md`). A service is the deployable unit; a context is the model boundary. Ideally one service per context.

| Signal | Likely boundary |
|---|---|
| Different team ownership / scale / SLA / deployment cadence | Separate service |
| Different data consistency needs | Separate service |
| Domain event boundary | Likely separate service |
| Same transaction needed across entities | Same service (same aggregate, ideally) |

Avoid: **chatty services** (10+ cross-service calls per user request → re-bundle); **shared entity IDs that change together** → same aggregate, same service; **CRUD-only services** ("UserService" with just CRUD on User) → premature decomposition.

## Communication patterns

### Sync (REST / gRPC)

```python
@circuit(failure_threshold=5, recovery_timeout=30)
async def get_user(user_id: str) -> dict:
    async with httpx.AsyncClient(timeout=2.0) as client:
        res = await client.get(f"{USER_SVC}/users/{user_id}",
                               headers={"X-Correlation-ID": get_correlation_id()})
        res.raise_for_status()
        return res.json()
```

Rules: timeout ≤ 2s for user-facing requests, ≤ 5s for background jobs; retry with exponential backoff + jitter; circuit breaker opens after N failures, half-open after cooldown; fallback = cached data / default / graceful degradation — never propagate failure to user unmodified.

### Async (message broker)

```python
# Producer
async with AIOKafkaProducer(bootstrap_servers=KAFKA) as p:
    await p.send_and_wait("order-events", key=event["order_id"].encode(),
        value=json.dumps(event).encode(),
        headers=[("x-correlation-id", correlation_id.encode())])

# Consumer — idempotent, group-based
consumer = AIOKafkaConsumer("order-events", bootstrap_servers=KAFKA, group_id="shipping-service")
async for msg in consumer:
    await handle_order_placed(json.loads(msg.value))   # MUST be idempotent
    await consumer.commit()                             # commit AFTER success
```

Rules: consumers MUST be idempotent (same event twice → same outcome); use consumer groups for horizontal scale (one event → one consumer in group); commit offset AFTER successful processing (at-least-once); propagate correlation ID in message headers.

## Saga pattern — distributed transactions

A saga is a sequence of local transactions. Each step publishes an event or invokes the next step. On failure, compensating transactions roll back prior steps.

### Orchestration (centralized)

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

// Usage: order creation saga
const orderSaga = [
  reserveInventoryStep,   // calls inventory service, on fail → no-op
  chargePaymentStep,      // calls payment service, on fail → release inventory
  scheduleShipmentStep,   // calls shipping service, on fail → refund payment + release inventory
];
await runSaga(orderSaga, { orderId, customerId, items });
```

### Choreography (decentralized)

Each service subscribes to events and emits events. No central orchestrator.

```
OrderService → OrderPlaced → InventoryService → StockReserved → PaymentService → PaymentCharged → ShippingService → ShipmentScheduled → OrderService (complete)
On failure: PaymentFailed → InventoryService (release stock) + OrderService (cancel)
```

### Orchestration vs choreography

| Criterion | Orchestration | Choreography |
|---|---|---|
| Visibility of flow | High (one place to see all steps) | Low (scattered across services) |
| Coupling | Orchestrator knows all services | Services know only events |
| Adding a step | Edit orchestrator | Add a new consumer; ensure ordering |
| Failure handling | Explicit in orchestrator | Implicit in event subscriptions |
| Best for | Complex sagas (3+ steps) | Simple flows (2-3 steps) |
| Risk | Orchestrator becomes god service | Event spaghetti; hard to trace |

Default: orchestration for complex sagas; choreography for simple ones. Document the flow either way.

### Compensation rules

- Compensation is **not** rollback in the ACID sense. It is business-level undo.
- Compensation must be idempotent (may be called multiple times).
- Compensation can fail — retry with backoff; if it stays failed, escalate (alert + manual intervention).
- Some actions are non-compensable (e.g., sending an email) — design the saga so non-compensable steps run last.

## Outbox pattern — reliable event publication

Problem: you update the DB and publish an event. If the publish fails, the state change is committed but the event is lost. If you publish first and the DB commit fails, downstream services see a phantom event.

Solution: write the event to an `outbox` table in the **same transaction** as the state change. A separate process reads from outbox and publishes to the broker.

```sql
-- Same transaction as business state change
BEGIN;
UPDATE orders SET status = 'submitted' WHERE id = $1;
INSERT INTO outbox (id, aggregate_type, aggregate_id, event_type, payload, created_at)
  VALUES (gen_random_uuid(), 'Order', $1, 'OrderPlaced', $2::jsonb, now());
COMMIT;

-- Separate worker — poll + publish + mark (FOR UPDATE SKIP LOCKED = concurrent workers)
SELECT id, event_type, payload FROM outbox WHERE published_at IS NULL
  ORDER BY created_at LIMIT 100 FOR UPDATE SKIP LOCKED;
-- after publish: UPDATE outbox SET published_at = now() WHERE id = ANY($1);
```

`FOR UPDATE SKIP LOCKED` lets multiple workers run concurrently without contention.

Alternatives to polling: **Change Data Capture (CDC)** — Debezium reads the Postgres WAL and streams changes to Kafka (no outbox table; events derived from WAL); **Logical replication** — Postgres logical replication slot → custom consumer.

## CQRS — separate read and write models

Write side: commands → aggregate → events → event store. Read side: projections from events → materialized views.

```
[Command] → [Handler] → [Aggregate] → [Event Store]
                                      ↓ [Event Bus] → [Projection] → [Read Model] ← [Query]
```

### When CQRS

- Read and write have very different shapes (write one order; read a dashboard of 1000 orders joined across 5 tables).
- Read load >> write load (or vice versa) — scale them independently.
- Multiple read models (admin, customer, analytics).
- Audit log required (event sourcing gives you this for free).

### When NOT CQRS

- Simple CRUD — the read/write split doubles your code for no benefit.
- Team can't handle eventual consistency between write and read models.
- Read model can be a DB view — no need for a separate projection.

### Projection example

```python
# Project OrderPlaced events into a denormalized read model
async def handle_order_placed(event: OrderPlaced):
    await db.execute("""
        INSERT INTO order_view (order_id, customer_id, total, status, created_at)
        VALUES ($1, $2, $3, 'submitted', $4)
        ON CONFLICT (order_id) DO UPDATE SET status = 'submitted', total = $3
    """, event.order_id, event.customer_id, event.total.amount, event.occurred_at)
```

## Event sourcing

Store events as the source of truth; current state is a projection of events.

```sql
CREATE TABLE event_store (
    id UUID PRIMARY KEY,
    aggregate_type TEXT NOT NULL, aggregate_id UUID NOT NULL,
    event_type TEXT NOT NULL, payload JSONB NOT NULL,
    version INT NOT NULL, occurred_at TIMESTAMP NOT NULL DEFAULT now(),
    UNIQUE (aggregate_id, version)               -- optimistic concurrency
);
```

Rules: each event has a `version`; events are immutable (append compensating events, never edit); snapshots optimize loading large streams (save full state every N events).

**When to use:** audit log required (regulated industries); time-travel queries (what was the state at T?); complex domain with many state transitions; multiple downstream projections. **When NOT:** simple CRUD domain (overhead >> benefit); GDPR right-to-be-forgotten conflicts with immutable log (needs crypto-shredding); team unfamiliar — operational complexity is high.

## Resilience patterns

### Circuit breaker

```python
from circuitbreaker import circuit

@circuit(failure_threshold=5, recovery_timeout=30, expected_exception=TimeoutError)
async def call_inventory(order_id: str):
    return await httpx.AsyncClient(timeout=2.0).get(f"{INVENTORY}/stock/{order_id}")
```

States: closed (normal) → open (fail fast after N failures) → half-open (probe one request after cooldown). Configure per downstream service.

### Retry with jitter

```python
async def retry_with_jitter(fn, attempts=3, base_delay=0.5, max_delay=10.0):
    for attempt in range(attempts):
        try: return await fn()
        except Exception:
            if attempt == attempts - 1: raise
            delay = min(max_delay, base_delay * (2 ** attempt)) * (0.5 + random.random())
            await asyncio.sleep(delay)
```

Always include jitter; without it, retrying clients stampede on recovery.

### Bulkhead

Limit concurrent calls per downstream so one slow service doesn't exhaust your connection pool:

```python
semaphore = asyncio.Semaphore(10)              # max 10 concurrent calls to inventory
async def call_inventory(order_id):
    async with semaphore:
        return await httpx.AsyncClient(timeout=2.0).get(...)
```

## Observability

- **Distributed tracing:** OpenTelemetry; trace context propagated via `traceparent` header (W3C).
- **Correlation IDs:** user-facing requests get a UUID; propagate to all downstream calls and message headers; include in logs.
- **Metrics:** RED (Rate, Errors, Duration) per endpoint; USE (Utilization, Saturation, Errors) per resource.
- **Logs:** structured (JSON), include correlation ID, service name, version; ship to centralized store (ELK, Loki, CloudWatch).

```python
# FastAPI middleware — propagate correlation ID
class CorrelationIdMiddleware(BaseHTTPMiddleware):
    async def dispatch(self, request, call_next):
        cid = request.headers.get("X-Correlation-ID") or str(uuid.uuid4())
        request.state.correlation_id = cid
        response = await call_next(request)
        response.headers["X-Correlation-ID"] = cid
        return response
```

## Verification gates

- Each service deploys independently; deploying one doesn't require deploying others.
- A single failed service degrades gracefully (fallbacks work; users see degradation, not 500s).
- Distributed trace shows one user request spanning all services it touched.
- Saga compensation tested: simulate failure at each step; verify prior steps roll back.
- Outbox publisher keeps `published_at IS NULL` rows below threshold (no event leakage).
- Load test: p99 latency under target; no cascading failures when a downstream is killed.

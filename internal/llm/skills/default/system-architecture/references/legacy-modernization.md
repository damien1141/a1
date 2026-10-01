# Legacy Modernization — Strangler Fig, Branch by Abstraction, Characterization Tests

Incremental migration of legacy systems. Core principle: **never rewrite from scratch**. Always strangler fig — wrap the legacy, route traffic to new code feature by feature, retire the legacy once new code handles 100% of traffic for one release cycle.

## Core workflow

1. **Assess system** — codebase, dependencies, risks, business constraints. Produce dependency map + risk register before touching code.
2. **Plan migration** — incremental roadmap with explicit rollback per phase. Each phase: rollback trigger + owner.
3. **Build safety net** — characterization tests of existing behavior (golden master). Target 80%+ coverage on paths you will touch.
4. **Migrate incrementally** — strangler fig + feature flags. Traffic shift 5% → 25% → 50% → 100%; verify error rate + latency at each step.
5. **Validate & iterate** — full test suite green; monitoring dashboards within baseline; new code proven stable at 100% traffic for one release cycle before retiring legacy path.

## Strangler fig pattern

Build a new system alongside the legacy. A facade routes each request to legacy or new code based on a feature flag. Migrate one route/feature at a time; once 100% traffic is on new code for a release cycle, retire the legacy route.

```python
# facade.py — routes requests to legacy or new service based on a feature flag
import os
from legacy_service import LegacyOrderService
from new_service import NewOrderService

class OrderServiceFacade:
    def __init__(self):
        self._legacy = LegacyOrderService()
        self._new = NewOrderService()

    def get_order(self, order_id: str):
        if flag_enabled("USE_NEW_ORDER_SERVICE"):
            try:
                return self._new.fetch(order_id)
            except Exception as e:
                log.error("new service failed", exc_info=e)
                if flag_enabled("FALLBACK_TO_LEGACY"):
                    return self._legacy.get(order_id)
                raise
        return self._legacy.get(order_id)
```

### Traffic shift strategy

| Phase | Traffic on new | Rollback trigger |
|---|---|---|
| 0 — Dark launch | 0% (shadow traffic only) | N/A; compare outputs |
| 1 — Canary | 5% | Error rate > 1% or p99 latency > 2× baseline |
| 2 — Early adopter | 25% | Error rate > 0.5% or p99 latency > 1.5× baseline |
| 3 — Majority | 50% | Any error rate increase |
| 4 — Pre-cutover | 100% | Same as 3 |
| 5 — Retire legacy | 100% (legacy removed) | N/A |

Shadow traffic (phase 0): send a copy of every request to the new service, compare responses, log mismatches. Doesn't affect users; reveals behavior drift before any user is exposed.

## Feature flags

```python
# feature_flags.py — thin wrapper around an env var or config-based flag store
import os

def flag_enabled(flag_name: str, default: bool = False) -> bool:
    return os.getenv(flag_name, str(default)).lower() == "true"

# More sophisticated: LaunchDarkly, Unleash, Flagsmith, custom Redis-backed
```

Rules:
- Flags are temporary — every flag has a removal ticket.
- Flags are not config — flag for migration; config for permanent toggles (rate limits, feature tiers).
- Flags support percentage rollout, not just on/off (use a hash of user_id modulo 100).
- Always log which path a request took (for debugging and audit).

## Branch by abstraction

When refactoring within a single codebase, create an abstraction layer that has two implementations (old + new). Swap callers to the new implementation one at a time; remove the old implementation once nothing uses it.

```python
# Before: 30 callers use PaymentGateway.charge() directly
# Step 1: Introduce interface
class PaymentGateway(Protocol):
    def charge(self, amount: Money, customer_id: str) -> ChargeResult: ...

# Step 2: LegacyPaymentGateway implements it (delegates to old code)
class LegacyPaymentGateway:
    def charge(self, amount, customer_id):
        return old_charge(amount, customer_id)

# Step 3: NewPaymentGateway implements it (new code)
class NewPaymentGateway:
    def charge(self, amount, customer_id):
        return stripe.charge(amount, customer_id)

# Step 4: Factory returns old or new based on flag
def payment_gateway() -> PaymentGateway:
    return NewPaymentGateway() if flag_enabled("USE_NEW_PAYMENT") else LegacyPaymentGateway()

# Step 5: Migrate callers one at a time
def checkout(order):
    gateway = payment_gateway()       # was: gateway = LegacyPaymentGateway()
    return gateway.charge(order.total, order.customer_id)
```

The abstraction makes the swap atomic per caller; the flag makes it reversible.

## Characterization tests (golden master)

Tests that pin existing behavior — they don't verify correctness, they verify *that the behavior doesn't change*.

```python
# test_characterization_orders.py
import pytest
from legacy_service import LegacyOrderService

service = LegacyOrderService()

@pytest.mark.parametrize("order_id,expected_status", [
    ("ORD-001", "SHIPPED"),
    ("ORD-002", "PENDING"),
    ("ORD-003", "CANCELLED"),
    ("ORD-004", "SHIPPED"),
    # ... capture from production traffic or DB snapshot
])
def test_order_status_golden_master(order_id, expected_status):
    """Fail loudly if legacy behavior changes unexpectedly."""
    result = service.get(order_id)
    assert result["status"] == expected_status, (
        f"Characterization broken for {order_id}: "
        f"expected {expected_status}, got {result['status']}"
    )
```

For complex outputs (full API responses, PDFs, generated images), use snapshot/approval testing:

```python
# Using pytest-snapshot
def test_order_response_snapshot(snapshot, client):
    res = client.get("/orders/ORD-001")
    snapshot.snapshot_match(res.json())
```

Run the suite against the legacy system BEFORE refactoring. Suite must be green. Then refactor; suite must remain green.

## Migration strategies by concern

### Database migration

| Need | Pattern |
|---|---|
| Schema change | Expand + contract (add new column → dual-write → backfill → switch reads → drop old) |
| Database engine change | Dual-write to both → backfill → switch reads → stop writes to old → retire old |
| Sharding | Consistent hash → dual-write new shard → backfill → switch reads → retire old shard |

```sql
-- Expand: add new column
ALTER TABLE orders ADD COLUMN customer_uuid UUID;

-- Backfill: populate from old column
UPDATE orders SET customer_uuid = (
    SELECT id FROM customers WHERE customers.old_id = orders.customer_id
);

-- Switch reads: deploy code that reads customer_uuid
-- Switch writes: deploy code that writes both (dual-write window)
-- Contract: after dual-write window + verification, drop old column
ALTER TABLE orders DROP COLUMN customer_id;
```

### API migration

| Need | Pattern |
|---|---|
| New endpoint shape | Version in URI (`/v2/users`); old returns `Deprecation: true` + `Sunset: <date>` |
| Field rename | Add new field alongside old; populate both; switch clients to new; drop old after deprecation window |
| Auth scheme change | Accept both old + new during transition; log which scheme was used; alert on old usage past threshold |
| Protocol change (REST → gRPC) | Strangler fig: new endpoints on gRPC, legacy REST stays; route per feature |

### UI migration

| Need | Pattern |
|---|---|
| Framework rewrite (jQuery → React) | Mount new app on a subpath; migrate one page at a time |
| Design system change | Component-by-component swap with feature flag; both styles coexist |
| Backend-for-frontend (BFF) | Introduce BFF layer between legacy API and new UI; BFF talks to legacy; migrate BFF to new backend at leisure |

### Framework / language migration

| Need | Pattern |
|---|---|
| Framework upgrade (Django 3 → 5) | One minor version at a time; run deprecation warnings as errors |
| Language upgrade (Python 3.8 → 3.12) | Run `pyupgrade` + `ruff`; fix syntax errors; verify deps support new version |
| Rewrite (Java → Go) | Strangler fig + ACL; new services in Go; legacy Java keeps running until retired |

## Anti-patterns

- **Big-bang rewrite.** Two years of parallel maintenance sounds expensive; a stalled big-bang rewrite is more expensive.
- **No characterization tests.** You will break behavior you didn't know existed.
- **No rollback plan.** "We'll just fix forward" is not a rollback plan.
- **No traffic shift.** Going straight to 100% on day one is a big-bang in disguise.
- **Retiring legacy too early.** Keep the legacy path alive for at least one release cycle at 100% new-code traffic. If something breaks, you can fall back.
- **Drift between old and new.** If new code produces different output for the same input, the comparison test catches it. Fix the drift before increasing traffic.

## Common pitfalls

- **External integrations.** The legacy system has 50 integrations you don't know about. Document them before touching code: `grep -rn "import legacy" .`, scan network logs for outbound calls, ask the team.
- **Hidden state.** The legacy system writes to a file on disk that no one mentioned. Find it before migrating.
- **Performance characteristics.** The legacy system was slow but consistent. The new system is fast on average but has tail latency spikes. Load test before cutover.
- **Time zones and date formats.** The legacy system stores dates as `MM/DD/YYYY` strings in some columns and ISO-8601 in others. Normalize during migration.
- **Encoding.** Latin-1 vs UTF-8 — verify before migrating text data.

## Verification gates

- Characterization test suite green on legacy system BEFORE refactoring.
- Same suite green on new system (or any failures documented as intentional changes).
- Feature flag rollout: error rate, latency, and business metrics within baseline at each traffic increment.
- Rollback tested: flip flag back to legacy, verify traffic returns to legacy path within seconds.
- New code runs at 100% traffic for one release cycle (typically 1-4 weeks) before legacy path is removed.
- All external integrations documented and verified against new system.
- Audit: `grep -rn "TODO\|FIXME\|HACK"` reviewed; technical debt from migration has tickets.

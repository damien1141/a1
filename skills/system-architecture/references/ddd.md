# Domain-Driven Design — Bounded Contexts, Aggregates, Value Objects

DDD is a modeling discipline: align code structure with business domain. The tactical patterns (entities, value objects, aggregates) and the strategic patterns (bounded contexts, context maps) constrain how you decompose a system. Use DDD before microservices — services emerge from contexts, not the other way around.

## Strategic design — bounded contexts

A bounded context is a linguistic and model boundary. Inside it, terms have one meaning. Outside, the same term may mean something else.

Example: "Product" means different things in different contexts:
- **Catalog context** — name, description, photos, categories.
- **Pricing context** — price tiers, discounts, currency.
- **Inventory context** — SKU, stock level, warehouse location.
- **Shipping context** — dimensions, weight, hazmat class.

Trying to model all of these in one `Product` class produces a god object. Instead, four contexts, four `Product` models, each with only the fields that context needs.

### Identifying contexts

| Signal | Meaning |
|---|---|
| Same word, different meanings | Multiple contexts |
| Different teams own different parts | Likely different contexts |
| Different life cycles | Different contexts |
| Different consistency requirements | Different contexts |
| Different scaling / SLA needs | Different contexts |

Techniques:
- **Event storming** — post-its of domain events on a wall; cluster by causality; the clusters are bounded contexts.
- **Narrative analysis** — read user stories; underline nouns; group by domain meaning.
- **Team topology** — Conway's Law: if a team owns a thing end-to-end, it's probably a context.

### Context map (relationships)

| Relationship | Pattern | Example |
|---|---|---|
| **Shared Kernel** | Two contexts share a small model (risky; only when teams collaborate tightly) | `User` shared between Auth and Profile |
| **Customer-Supplier** | Upstream (supplier) serves downstream (customer); upstream commits to API stability | Inventory → Catalog |
| **Conformist** | Downstream conforms to upstream's model without influence | Catalog conforms to external PIM system |
| **Anti-Corruption Layer (ACL)** | Downstream translates upstream's model into its own | Adapter wraps legacy API |
| **Open Host Service** | Upstream exposes a public API/protocol for many consumers | REST API |
| **Published Language** | Standardized exchange format (e.g., OpenAPI, protobuf) | Order event schema |

Default to **Customer-Supplier** with explicit API contracts. Use ACL when consuming a legacy or third-party system.

## Tactical design — entities, value objects, aggregates

### Entity

A domain object with identity that persists over time. Two entities are equal if their IDs match, even if other fields differ.

```python
from dataclasses import dataclass
from uuid import UUID

@dataclass
class Customer:
    id: UUID
    email: str
    name: str

    def __eq__(self, other): return isinstance(other, Customer) and self.id == other.id
    def __hash__(self): return hash(self.id)
```

Rules:
- Identity is immutable; a `Customer` that changes its ID is a different customer.
- ID is usually a UUID (not auto-increment integer — security, sharding).
- Mutations go through methods (`customer.change_email(...)`), not direct attribute assignment.

### Value object

A domain object defined by its values, not identity. Two value objects with the same values are interchangeable.

```python
from dataclasses import dataclass
from decimal import Decimal

@dataclass(frozen=True)
class Money:
    amount: Decimal
    currency: str

    def __post_init__(self):
        if self.amount < 0: raise ValueError("Money cannot be negative")
        if len(self.currency) != 3: raise ValueError("ISO 4217 currency code")

    def add(self, other: "Money") -> "Money":
        if self.currency != other.currency: raise ValueError("currency mismatch")
        return Money(self.amount + other.amount, self.currency)
```

Rules:
- Immutable (frozen dataclass).
- Validated at construction — bad data cannot exist.
- Compared by value, not identity.
- Prefer value objects over primitives: `Money` over `Decimal`, `EmailAddress` over `str`, `CustomerId` over `UUID`.

### Aggregate

A cluster of entities + value objects treated as a single consistency boundary. One entity is the **aggregate root**; all access goes through it.

```python
@dataclass
class Order:                                    # aggregate root
    id: UUID
    customer_id: UUID
    items: list[OrderItem]                      # entities, accessed only via Order
    status: OrderStatus
    total: Money

    def add_item(self, sku: str, qty: int, price: Money) -> None:
        if self.status != OrderStatus.DRAFT: raise ValueError("cannot modify submitted order")
        if qty <= 0: raise ValueError("quantity must be positive")
        existing = next((i for i in self.items if i.sku == sku), None)
        if existing: existing.increase_quantity(qty)
        else: self.items.append(OrderItem(sku=sku, quantity=qty, price=price))
        self._recalculate_total()

    def submit(self) -> None:
        if not self.items: raise ValueError("cannot submit empty order")
        if self.status != OrderStatus.DRAFT: raise ValueError("already submitted")
        self.status = OrderStatus.SUBMITTED

    def _recalculate_total(self) -> None:
        self.total = sum((i.price.multiply(i.quantity) for i in self.items), Money(0, "USD"))
```

Rules:
- **One root per aggregate.** External code holds a reference only to the root, never to inner entities.
- **All mutations through the root.** The root enforces invariants (e.g., can't add items to a submitted order).
- **Transaction boundary = aggregate.** One transaction loads, modifies, saves one aggregate. Cross-aggregate consistency uses eventual consistency + domain events.
- **References between aggregates by ID, not object reference.** `Order.customer_id: UUID`, not `Order.customer: Customer`. This prevents lazy-loading chains and accidental transaction boundary expansion.
- **Keep aggregates small.** A 50-entity aggregate is a smell — likely two aggregates that should be separated.

### Aggregate design heuristics

| Question | If yes |
|---|---|
| Must these entities be consistent in the same transaction? | Same aggregate |
| Can this entity exist independently? | Separate aggregate, reference by ID |
| Is the aggregate loaded as a unit? | Same aggregate |
| Are there many entities (10+)? | Likely too big — split |
| Does changing one require locking the other? | Same aggregate |

### Domain events

Things that happened in the domain that other contexts may care about.

```python
@dataclass(frozen=True)
class OrderPlaced:
    order_id: UUID
    customer_id: UUID
    total: Money
    occurred_at: datetime

# In the aggregate method
def submit(self) -> OrderPlaced:
    # ... validation ...
    self.status = OrderStatus.SUBMITTED
    return OrderPlaced(order_id=self.id, customer_id=self.customer_id, total=self.total, occurred_at=datetime.utcnow())
```

Rules:
- Events are immutable and past-tense (`OrderPlaced`, not `PlaceOrder`).
- Events carry only the data other contexts need — not the full aggregate.
- Publish via outbox pattern (same transaction as state change).
- Subscribers are eventual consistency — do not block the request on them.

## Ubiquitous language

The code uses the same terms the domain experts use. If the expert says "fulfillment request" but the code says "shipment," one of them is wrong — usually the code.

| Domain term | Code symbol |
|---|---|
| Customer | `Customer` class |
| Place order | `Order.place()` method |
| Order placed | `OrderPlaced` event |
| Out of stock | `OutOfStockError` exception |
| Refund | `Payment.refund()` method |

If the term is ambiguous, pick one meaning per context. Don't make `Order` mean both "shopping cart" and "confirmed purchase" in the same context — split into `Cart` and `Order`.

## Repository

```python
class OrderRepository:
    async def get(self, order_id: UUID) -> Order | None: ...
    async def save(self, order: Order) -> None: ...        # whole aggregate
    async def find_by_customer(self, customer_id: UUID) -> list[Order]: ...
```

Rules:
- One repository per aggregate root.
- Repository works in terms of aggregates, not tables. Saving an `Order` persists the order + its items + any new events in one transaction.
- Never expose queryable internals (`.items().where(...)`); use named query methods.
- For complex reads (lists, dashboards), use a separate read model — see CQRS in `references/microservices.md`.

## Layered architecture

```
[ Presentation ]   controllers / resolvers / DTOs
       ↓
[ Application ]    use cases / orchestration / transaction scripts
       ↓
[    Domain    ]   aggregates / entities / value objects / domain events
       ↓
[ Infrastructure ] repositories / message bus / external API clients
```

- Presentation depends on Application; Application depends on Domain; Domain depends on nothing (just standard library).
- Infrastructure implements interfaces defined in Domain (Dependency Inversion).
- Domain code has no imports from frameworks (no `@Entity` JPA, no `@dataclass` SQLAlchemy — those are infrastructure concerns).

## Verification gates

- Domain terms match expert language (review with a domain expert).
- Each aggregate fits in one transaction (no cross-aggregate locks).
- No object references between aggregates — only IDs.
- Every mutation on an aggregate goes through a method on the root.
- Domain events published via outbox (same tx as state change).
- Repository tests pass for each aggregate (load → mutate → save → reload).

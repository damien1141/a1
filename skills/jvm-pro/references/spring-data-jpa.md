# Spring Data JPA

## Repository basics

```java
public interface ProductRepository extends JpaRepository<Product, Long> {
    List<Product> findByNameContainingIgnoreCase(String name);

    @Query("SELECT p FROM Product p WHERE p.category.id = :catId")
    Page<Product> findByCategory(@Param("catId") Long catId, Pageable page);
}
```

Spring generates the implementation. Method name → query:
- `findBy<Field>` → `WHERE field = ?`
- `findBy<Field>Containing` → `WHERE field LIKE %?%`
- `findBy<Field>OrderBy<Field2>Desc` → `ORDER BY field2 DESC`
- `findBy<Field>And<Field2>` → `WHERE field = ? AND field2 = ?`

## N+1 prevention

```java
// BAD — N+1: one query per product.category access
List<Product> products = repo.findAll();
products.forEach(p -> log.info(p.getCategory().getName()));  // query per product

// GOOD — JOIN FETCH in a custom query
@Query("SELECT p FROM Product p JOIN FETCH p.category")
List<Product> findAllWithCategory();

// GOOD — EntityGraph (declarative)
@EntityGraph(attributePaths = "category")
List<Product> findAll();
```

## Projections (DTOs in queries)

```java
// Constructor expression — best for read-only
@Query("""
    SELECT new com.example.ProductSummary(p.id, p.name, p.price, c.name)
    FROM Product p JOIN p.category c
    WHERE p.category.id = :catId
    """)
Page<ProductSummary> findSummaries(@Param("catId") Long catId, Pageable page);

public record ProductSummary(Long id, String name, BigDecimal price, String categoryName) {}
```

Projections avoid pulling full entities — only the columns you need.

## Interface projections (alternative)

```java
public interface ProductView {
    Long getId();
    String getName();
    BigDecimal getPrice();
}

@Query("SELECT p.id AS id, p.name AS name, p.price AS price FROM Product p")
List<ProductView> findAllAsView();
```

Slightly slower than constructor expressions (proxy overhead) but more flexible.

## Transactions

```java
@Service
public class OrderService {
    @Transactional
    public Order place(OrderRequest req) {
        // multiple writes — atomic
        var order = new Order(req.customerId());
        orderRepo.save(order);
        for (var line : req.lines()) {
            lineRepo.save(new OrderLine(order.getId(), line));
        }
        return order;
    }

    @Transactional(readOnly = true)
    public OrderDto find(long id) {
        return orderRepo.findById(id)
            .map(OrderDto::from)
            .orElseThrow(() -> new EntityNotFoundException("order " + id));
    }
}
```

- `readOnly = true` enables Hibernate optimizations (no dirty checking, no flush)
- Default propagation `REQUIRED` joins an existing tx or starts a new one
- Use `REQUIRES_NEW` only when you need an independent tx (audit log, e.g.)

## Hibernate tuning

```yaml
spring:
  jpa:
    open-in-view: false       # CRITICAL — disables the anti-pattern
    hibernate:
      ddl-auto: validate      # never update in prod
    properties:
      hibernate:
        jdbc:
          batch_size: 50
          batch_versioned_data: true
        order_inserts: true
        order_updates: true
        default_batch_fetch_size: 25
```

- `open-in-view: false` — disables lazy loading in controllers (forces explicit fetching)
- `ddl-auto: validate` — Flyway/Liquibase manages schema; Hibernate only checks
- `batch_size` + `order_inserts` — batch INSERTs/UPDATEs (10-50x faster for bulk writes)
- `default_batch_fetch_size` — batch lazy loads (avoids N+1 even with lazy associations)

## Pagination

```java
Page<Product> page = repo.findByCategory(catId, PageRequest.of(0, 20, Sort.by("name")));
List<Product> content = page.getContent();
int totalPages = page.getTotalPages();
long total = page.getTotalElements();
```

For large offsets, use keyset pagination (`.where(p.id < :lastId).orderBy(p.id.desc())`) — `OFFSET 1000000` is slow.

## Flyway migrations

```
src/main/resources/db/migration/
├── V1__initial_schema.sql
├── V2__add_products_category_fk.sql
└── V3__add_products_name_index.sql
```

```sql
-- V1__initial_schema.sql
CREATE TABLE products (
    id BIGSERIAL PRIMARY KEY,
    name VARCHAR(200) NOT NULL,
    price NUMERIC(10, 2) NOT NULL,
    category_id BIGINT REFERENCES categories(id)
);
CREATE INDEX idx_products_name ON products(name);
```

- Versions are sequential; never edit a published migration
- Use `V<n>__description.sql` naming
- For complex refactors, split into multiple migrations

## Common pitfalls

- `repo.findAll()` then iterating associations — N+1; use `JOIN FETCH` or projections
- `open-in-view: true` (Spring default) — masks N+1 by lazy-loading in controllers; disable
- `ddl-auto: update` in production — unpredictable schema drift; use `validate` + migrations
- `@Transactional` on private methods — no effect (proxy)
- `@Transactional` self-invocation — bypasses proxy; split into separate beans
- Lazy initialization in tests — `LazyInitializationException` when session closed; use `JOIN FETCH` or `@Transactional(readOnly = true)` on test
- `Optional<T>` in entity fields — not supported; use `@OneToOne(optional = false)` etc.
- Storing `List<entity>` in HTTP session — detached entities; re-attach with `merge` or reload

# SQL Fundamentals — Query Patterns, CTEs, Window Functions, Dialects

Set-based thinking, common query patterns, CTEs, window functions, recursive queries, and dialect differences (Postgres vs MySQL vs SQL Server). The foundation that the other database-pro references build on.

## Set-based thinking

SQL operates on sets, not rows. The single biggest performance mistake is row-by-row processing in a loop. Always ask: "can I express this as one set operation?"

### Row-by-row (bad)

```python
# Python — N+1 query anti-pattern
for user in users:
    count = db.execute("SELECT COUNT(*) FROM orders WHERE user_id = ?", (user.id,))
    user.order_count = count
```

### Set-based (good)

```sql
-- One query, one round-trip
SELECT u.id, u.email, COUNT(o.id) AS order_count
FROM users u
LEFT JOIN orders o ON o.user_id = u.id
GROUP BY u.id, u.email;
```

## SELECT fundamentals

```sql
-- Always list columns explicitly in production code
SELECT id, email, name, created_at
FROM users
WHERE status = 'active'
  AND created_at >= '2024-01-01'
ORDER BY created_at DESC
LIMIT 20;

-- Aliases for readability
SELECT u.id, u.email, COUNT(o.id) AS order_count
FROM users AS u
LEFT JOIN orders AS o ON o.user_id = u.id
WHERE u.status = 'active'
GROUP BY u.id, u.email
HAVING COUNT(o.id) > 0           -- filter on aggregate (HAVING, not WHERE)
ORDER BY order_count DESC;
```

Order of execution (logical): `FROM → WHERE → GROUP BY → HAVING → SELECT → DISTINCT → ORDER BY → LIMIT`. Understanding this prevents "can't use alias in WHERE" confusion.

## JOIN types

| JOIN | Returns |
|---|---|
| `INNER JOIN` | Rows matching in both tables (intersection) |
| `LEFT JOIN` | All rows from left + matching from right (NULL if no match) |
| `RIGHT JOIN` | All rows from right + matching from left (rare; usually rewrite as LEFT) |
| `FULL OUTER JOIN` | All rows from both (NULL where no match) |
| `CROSS JOIN` | Cartesian product (every left × every right) |
| `SELF JOIN` | Table joined to itself (alias required) |

```sql
-- Find users with no orders (LEFT JOIN + NULL check)
SELECT u.id, u.email
FROM users u
LEFT JOIN orders o ON o.user_id = u.id
WHERE o.id IS NULL;

-- Self-join: find employees with the same manager
SELECT a.name, b.name
FROM employees a, employees b
WHERE a.manager_id = b.manager_id AND a.id < b.id;
```

## Subqueries vs CTEs vs JOINs

Three ways to express the same query; CTEs win for readability, JOINs for performance, subqueries only when nothing else works.

```sql
-- Subquery (works, hard to read)
SELECT id, email FROM users
WHERE id IN (SELECT user_id FROM orders WHERE total > 100);

-- CTE (readable; modern planners optimize equally well)
WITH big_spenders AS (
  SELECT user_id FROM orders WHERE total > 100
)
SELECT u.id, u.email FROM users u
JOIN big_spenders bs ON bs.user_id = u.id;

-- JOIN (simplest, often fastest)
SELECT DISTINCT u.id, u.email
FROM users u
JOIN orders o ON o.user_id = u.id
WHERE o.total > 100;
```

## CTE patterns

```sql
-- Chain multiple CTEs for complex transformations
WITH
  active_users AS (
    SELECT id, email FROM users WHERE status = 'active'
  ),
  user_order_counts AS (
    SELECT user_id, COUNT(*) AS order_count
    FROM orders
    GROUP BY user_id
  ),
  ranked AS (
    SELECT
      au.id, au.email,
      COALESCE(uoc.order_count, 0) AS orders,
      RANK() OVER (ORDER BY COALESCE(uoc.order_count, 0) DESC) AS rank
    FROM active_users au
    LEFT JOIN user_order_counts uoc ON uoc.user_id = au.id
  )
SELECT id, email, orders, rank FROM ranked WHERE rank <= 10;
```

Postgres 12+ inlines CTEs by default (so performance matches subquery). MySQL 8+ supports CTEs. SQL Server has long supported them.

## Recursive CTEs — tree traversal

```sql
-- Find all descendants of an org node
WITH RECURSIVE descendants AS (
  -- Anchor: starting node
  SELECT id, name, parent_id, 0 AS depth
  FROM org_chart WHERE id = $1

  UNION ALL

  -- Recurse: children of previous level
  SELECT c.id, c.name, c.parent_id, d.depth + 1
  FROM org_chart c
  JOIN descendants d ON c.parent_id = d.id
  WHERE d.depth < 10                           -- safety limit
)
SELECT id, name, depth FROM descendants ORDER BY depth, name;
```

Use for: org charts, category trees, threaded comments, BOM (bill of materials) explosions. Watch for infinite loops on cyclic data — add a depth limit.

## Window functions

Compute a value across a set of rows *related to* the current row, without collapsing them (unlike `GROUP BY`).

```sql
-- Row number within partition
SELECT
  customer_id,
  order_id,
  order_date,
  ROW_NUMBER() OVER (PARTITION BY customer_id ORDER BY order_date DESC) AS rn
FROM orders;

-- Latest order per customer (filter the row number = 1)
WITH ranked AS (
  SELECT *,
    ROW_NUMBER() OVER (PARTITION BY customer_id ORDER BY order_date DESC) AS rn
  FROM orders
)
SELECT * FROM ranked WHERE rn = 1;

-- Running total and moving average
SELECT
  date,
  revenue,
  SUM(revenue) OVER (ORDER BY date ROWS BETWEEN UNBOUNDED PRECEDING AND CURRENT ROW) AS running_total,
  AVG(revenue) OVER (ORDER BY date ROWS BETWEEN 6 PRECEDING AND CURRENT ROW) AS seven_day_avg
FROM daily_sales;

-- Rank with ties; LAG/LEAD for period-over-period
SELECT
  product_id,
  sale_date,
  amount,
  RANK() OVER (PARTITION BY product_id ORDER BY amount DESC) AS rank,
  LAG(amount, 1) OVER (PARTITION BY product_id ORDER BY sale_date) AS prev_amount,
  amount - LAG(amount, 1) OVER (PARTITION BY product_id ORDER BY sale_date) AS delta
FROM sales;
```

Window function family:
- `ROW_NUMBER()` — unique rank; no ties.
- `RANK()` — tied rows get same rank; next rank skips (1, 2, 2, 4).
- `DENSE_RANK()` — tied rows get same rank; next rank doesn't skip (1, 2, 2, 3).
- `LAG(col, n)` / `LEAD(col, n)` — value from n rows before/after.
- `FIRST_VALUE(col)` / `LAST_VALUE(col)` — first/last in frame.
- `NTILE(n)` — divide into n buckets.
- `SUM/AVG/COUNT/MIN/MAX OVER (...)` — aggregate over frame.

## Aggregation patterns

```sql
-- GROUP BY + HAVING
SELECT customer_id, COUNT(*) AS orders, SUM(total) AS revenue
FROM orders
WHERE created_at >= '2024-01-01'
GROUP BY customer_id
HAVING COUNT(*) > 5 AND SUM(total) > 1000
ORDER BY revenue DESC;

-- ROLLUP (subtotals + grand total)
SELECT
  COALESCE(country, 'ALL') AS country,
  COALESCE(product, 'ALL') AS product,
  SUM(quantity) AS total
FROM sales
GROUP BY ROLLUP (country, product);

-- CUBE (all combination subtotals)
SELECT country, product, SUM(quantity)
FROM sales GROUP BY CUBE (country, product);

-- GROUPING SETS (specific combinations)
SELECT country, product, SUM(quantity)
FROM sales
GROUP BY GROUPING SETS ((country, product), (country), ());
```

## EXISTS vs IN vs JOIN

For existence checks, `EXISTS` is usually fastest (short-circuits on first match):

```sql
-- Best — short-circuits
SELECT * FROM users u
WHERE EXISTS (SELECT 1 FROM orders o WHERE o.user_id = u.id);

-- OK — planner may rewrite to EXISTS anyway
SELECT * FROM users u
WHERE u.id IN (SELECT user_id FROM orders);

-- OK for small inner set
SELECT * FROM users u
WHERE u.id IN (1, 2, 3, 4, 5);

-- Bad — full join when you only need existence
SELECT DISTINCT u.* FROM users u JOIN orders o ON o.user_id = u.id;
```

## NULL handling

`NULL` is unknown, not zero or empty string. Comparisons with `NULL` yield `NULL` (treated as false in WHERE).

```sql
-- WRONG — never matches NULL
SELECT * FROM users WHERE status != 'deleted';

-- RIGHT
SELECT * FROM users WHERE status != 'deleted' OR status IS NULL;
-- or
SELECT * FROM users WHERE status IS DISTINCT FROM 'deleted';

-- NULL-aware aggregates
SELECT COUNT(*) AS total,        -- counts all rows
       COUNT(email) AS with_email, -- counts non-NULL emails
       AVG(amount) AS avg          -- ignores NULL amounts
FROM orders;

-- COALESCE for defaults
SELECT COALESCE(nickname, username, email) AS display_name FROM users;

-- NULLIF to avoid divide-by-zero
SELECT total / NULLIF(count, 0) FROM ...;  -- returns NULL instead of error
```

## UPSERT

```sql
-- PostgreSQL
INSERT INTO users (id, email, name)
VALUES (1, 'a@b.com', 'Alice')
ON CONFLICT (id) DO UPDATE
  SET email = EXCLUDED.email, name = EXCLUDED.name
  WHERE users.name IS DISTINCT FROM EXCLUDED.name;

-- MySQL
INSERT INTO users (id, email, name) VALUES (1, 'a@b.com', 'Alice')
ON DUPLICATE KEY UPDATE email = VALUES(email), name = VALUES(name);

-- SQLite
INSERT INTO users (id, email, name) VALUES (1, 'a@b.com', 'Alice')
ON CONFLICT(id) DO UPDATE SET email = excluded.email, name = excluded.name;
```

## Dialect differences

| Feature | PostgreSQL | MySQL | SQLite |
|---|---|---|---|
| Booleans | `BOOLEAN` (true/false) | `TINYINT(1)` (1/0) | INTEGER (1/0) |
| String concat | `'a' \|\| 'b'` | `CONCAT('a','b')` | `'a' \|\| 'b'` |
| Auto-increment | `SERIAL` or `IDENTITY` | `AUTO_INCREMENT` | `INTEGER PRIMARY KEY` |
| Return on insert | `INSERT ... RETURNING *` | Not supported (need `LAST_INSERT_ID()`) | `INSERT ... RETURNING *` (3.35+) |
| UPSERT | `ON CONFLICT DO UPDATE` | `ON DUPLICATE KEY UPDATE` | `ON CONFLICT DO UPDATE` |
| JSON type | `JSONB` (indexed) | `JSON` | `TEXT` + `json_extract()` |
| CTE | `WITH` (inlinable) | `WITH` (8.0+) | `WITH` |
| Window functions | Yes | Yes (8.0+) | Yes (3.25+) |
| Generated columns | Yes (`GENERATED ALWAYS AS`) | Yes | Yes |
| Partial index | Yes | No (8.0+) | Yes |
| Materialized views | Yes | No (use tables) | No |

## Set operations

```sql
-- UNION — distinct rows from both
SELECT id, email FROM customers
UNION
SELECT id, email FROM vendors;

-- UNION ALL — keeps duplicates (faster; use when no dups expected)
SELECT id FROM active_users
UNION ALL
SELECT id FROM archived_users;

-- INTERSECT — rows in both
SELECT user_id FROM orders
INTERSECT
SELECT user_id FROM returns;

-- EXCEPT (MINUS in Oracle) — rows in first but not second
SELECT user_id FROM orders
EXCEPT
SELECT user_id FROM returns;
```

## Verification gates

- Query returns expected rows (manual sanity check on small data).
- `EXPLAIN (ANALYZE, BUFFERS)` shows index usage, not Seq Scan on large tables.
- Query runs in < 100ms for OLTP; < 1s for analytical.
- Tested with both empty and production-scale data.
- Edge cases: NULL inputs, empty result sets, max-length strings, concurrent writes.

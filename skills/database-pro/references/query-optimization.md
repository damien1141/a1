# Query Optimization + Index Strategy

Slow query diagnosis, index design (covering, partial, multi-column), and before/after optimization patterns. Always capture `EXPLAIN (ANALYZE, BUFFERS)` baseline first; never optimize without measurement.

## Workflow

1. **Identify slow queries** — `pg_stat_statements` (Postgres), slow query log (MySQL), `EXPLAIN QUERY PLAN` (SQLite).
2. **Capture baseline** — `EXPLAIN (ANALYZE, BUFFERS)` before any change.
3. **Diagnose** — match pattern (Seq Scan, Nested Loop with large outer, stale stats, sort spill, low cache hit).
4. **Design fix** — index strategy, query rewrite, config change. One change at a time.
5. **Verify** — re-run `EXPLAIN`; confirm index used, cost/time dropped.
6. **Monitor** — `pg_stat_user_indexes` to confirm the index is actually used in production.

## Identify slow queries

```sql
-- PostgreSQL: top slow queries via pg_stat_statements
SELECT
  query,
  calls,
  round(total_exec_time::numeric, 2)  AS total_ms,
  round(mean_exec_time::numeric, 2)   AS mean_ms,
  round(stddev_exec_time::numeric, 2) AS stddev_ms,
  rows
FROM pg_stat_statements
ORDER BY total_exec_time DESC
LIMIT 20;

-- Find queries with seq scans on large tables
SELECT schemaname, tablename, seq_scan, seq_tup_read, idx_scan
FROM pg_stat_user_tables
WHERE seq_scan > 0
  AND seq_tup_read > 10000                -- significant volume
ORDER BY seq_tup_read DESC;

-- Find unused indexes (write amplification for no benefit)
SELECT schemaname, tablename, indexname, idx_scan, idx_tup_read, idx_tup_fetch
FROM pg_stat_user_indexes
WHERE idx_scan = 0
  AND indexname NOT LIKE '%_pkey'         -- exclude PKs
ORDER BY pg_size_pretty(pg_relation_size(schemaname || '.' || indexname)) DESC;
```

```sql
-- MySQL: top slow queries
SELECT * FROM performance_schema.events_statements_summary_by_digest
ORDER BY SUM_TIMER_WAIT DESC LIMIT 20;

EXPLAIN FORMAT=JSON
SELECT * FROM orders WHERE status = 'pending' AND created_at > NOW() - INTERVAL 7 DAY;
```

## Capture baseline

```sql
EXPLAIN (ANALYZE, BUFFERS, FORMAT TEXT)
SELECT o.id, c.name, o.total
FROM orders o
JOIN customers c ON c.id = o.customer_id
WHERE o.status = 'pending'
  AND o.created_at > now() - interval '7 days'
ORDER BY o.created_at DESC
LIMIT 50;
```

Note: `Execution Time`, `cost=`, `Buffers: shared hit=X read=Y`, and which scan/join types appear.

## Index strategies

### Single-column index

```sql
CREATE INDEX idx_users_email ON users(email);

-- When to use: WHERE filter on one column, no other useful columns
```

### Multi-column (composite) index

```sql
-- Order matters: equality first, then range, then sort
CREATE INDEX idx_orders_status_created
ON orders(status, created_at);

-- Supports:
-- WHERE status = 'pending'
-- WHERE status = 'pending' AND created_at > '2024-01-01'
-- WHERE status = 'pending' ORDER BY created_at

-- Does NOT support (status not in WHERE):
-- WHERE created_at > '2024-01-01'  → Seq Scan or separate index
```

Column order rules:
1. **Equality before range** — `WHERE type = 'click' AND ts > ...` → `(type, ts)`.
2. **High selectivity first** — `WHERE user_id = 5 AND status = 'pending'` → `(user_id, status)`.
3. **Match `ORDER BY`** — index satisfies sort if order matches.
4. **Don't index low-cardinality alone** — `WHERE is_active = true` → use partial index.

### Covering index (INCLUDE)

```sql
-- INCLUDE columns are stored in the index but not used for ordering/filtering
CREATE INDEX idx_orders_status_created_covering
ON orders(status, created_at)
INCLUDE (customer_id, total_amount);

-- Index-only scan — heap not accessed
EXPLAIN ANALYZE
SELECT customer_id, total_amount FROM orders
WHERE status = 'pending' ORDER BY created_at;
-- → "Index Only Scan" — much faster than "Index Scan"
```

Use when a query touches only a few non-filter columns. Trade-off: larger index, slower writes.

### Partial index

```sql
-- Only index rows matching WHERE — smaller, faster, less write amplification
CREATE INDEX idx_orders_pending
ON orders(customer_id)
WHERE status = 'pending';

-- Supports: WHERE status = 'pending' AND customer_id = X
-- Does NOT support: WHERE status = 'shipped' AND customer_id = X
```

Use when queries always filter on a constant subset (e.g., only `pending` orders are queried frequently).

### Unique index

```sql
CREATE UNIQUE INDEX idx_users_email_lower ON users(LOWER(email));
-- Now case-insensitive email uniqueness is enforced at DB level

CREATE UNIQUE INDEX idx_one_active_session_per_user
ON sessions(user_id) WHERE active = true;
-- Enforces "only one active session per user" at DB level
```

### Expression index

```sql
-- Index the result of a function
CREATE INDEX idx_users_lower_email ON users(LOWER(email));
CREATE INDEX idx_events_extracted_type ON events((payload->>'type'));

-- Query must use the same expression
SELECT * FROM users WHERE LOWER(email) = 'alice@example.com';
SELECT * FROM events WHERE payload->>'type' = 'login';
```

### Index type selection

| Query shape | Index type |
|---|---|
| `WHERE col = X` / `WHERE col > X` / `ORDER BY col` | B-tree (default) |
| `WHERE jsonb_col @> '{...}'` | GIN |
| `WHERE array_col @> ARRAY[...]` | GIN |
| `WHERE tsvector_col @@ to_tsquery(...)` | GIN |
| `WHERE geo_col && box` | GiST |
| `WHERE ts BETWEEN ...` on time-series | BRIN |
| `WHERE col = X` (pure equality, no range) | Hash |

## Query rewrites

### Correlated subquery → JOIN

```sql
-- BEFORE: correlated subquery, one execution per row (slow)
SELECT order_id,
       (SELECT SUM(quantity) FROM order_items oi WHERE oi.order_id = o.id) AS item_count
FROM orders o;

-- AFTER: single aggregation join (fast)
SELECT o.order_id, COALESCE(agg.item_count, 0) AS item_count
FROM orders o
LEFT JOIN (
  SELECT order_id, SUM(quantity) AS item_count
  FROM order_items
  GROUP BY order_id
) agg ON agg.order_id = o.id;
```

### N+1 → batched

```python
# BEFORE: N+1 (one query per user)
for user in users:
    orders = db.query("SELECT * FROM orders WHERE user_id = ?", user.id)  # N queries

# AFTER: one query for all users
orders = db.query("SELECT * FROM orders WHERE user_id IN (?, ?, ?)", [u.id for u in users])
orders_by_user = defaultdict(list)
for o in orders: orders_by_user[o.user_id].append(o)
for user in users:
    user.orders = orders_by_user[user.id]
```

### OFFSET pagination → cursor/keyset

```sql
-- BEFORE: OFFSET — gets slower as you page deeper
SELECT * FROM orders ORDER BY created_at DESC LIMIT 20 OFFSET 10000;
-- Postgres must scan + sort 10020 rows to return 20

-- AFTER: keyset — constant time regardless of page depth
-- Index on (created_at, id) makes this O(limit)
SELECT id, customer_id, total, created_at
FROM orders
WHERE (created_at, id) < ($1, $2)       -- cursor decodes to last row's (created_at, id)
ORDER BY created_at DESC, id DESC
LIMIT 21;                                -- fetch 21 to determine has_more
```

### COUNT for existence → EXISTS

```sql
-- BEFORE: counts all matching rows
SELECT CASE WHEN COUNT(*) > 0 THEN 1 ELSE 0 END FROM orders WHERE customer_id = $1;

-- AFTER: short-circuits on first match
SELECT EXISTS (SELECT 1 FROM orders WHERE customer_id = $1);
```

### SELECT * → explicit columns

```sql
-- BEFORE: fetches all columns, can't use covering index
SELECT * FROM orders WHERE customer_id = $1;

-- AFTER: only fetch what's needed; covering index possible
SELECT id, total, status FROM orders WHERE customer_id = $1;
```

### OR → UNION ALL

```sql
-- BEFORE: OR can prevent index usage
SELECT * FROM orders WHERE customer_id = $1 OR status = 'pending';

-- AFTER: each branch can use a different index
SELECT * FROM orders WHERE customer_id = $1
UNION
SELECT * FROM orders WHERE status = 'pending' AND customer_id != $1;
```

### Function on column → generated column + index

```sql
-- BEFORE: function on column prevents index usage
SELECT * FROM users WHERE LOWER(email) = 'alice@example.com';
-- (Index on users(email) is not used; Seq Scan unless expression index exists)

-- AFTER: generated column + index
ALTER TABLE users ADD COLUMN email_lower TEXT GENERATED ALWAYS AS (LOWER(email)) STORED;
CREATE INDEX idx_users_email_lower ON users(email_lower);
SELECT * FROM users WHERE email_lower = 'alice@example.com';
```

## Configuration tuning

```sql
-- Per-session: raise work_mem for big sorts (default 4MB)
SET work_mem = '64MB';
SET maintenance_work_mem = '512MB';   -- for CREATE INDEX, VACUUM

-- Per-database defaults
ALTER DATABASE mydb SET work_mem = '32MB';
```

```ini
# postgresql.conf — production starting points (verify with pgbench)
shared_buffers = 4GB                  # 25% of RAM
effective_cache_size = 12GB           # 75% of RAM
work_mem = 16MB                       # per-sort/hash
maintenance_work_mem = 512MB
random_page_cost = 1.1                # SSD (default 4.0 is for spinning disk)
effective_io_concurrency = 200        # SSD
max_connections = 200                 # use PgBouncer for more
checkpoint_completion_target = 0.9
wal_buffers = 16MB
```

Always benchmark before/after with `pgbench`. Don't cargo-cult.

## Verify the optimization worked

```sql
-- After CREATE INDEX CONCURRENTLY, confirm the index is used
EXPLAIN (ANALYZE, BUFFERS)
SELECT * FROM orders WHERE customer_id = 42 AND status = 'pending';
-- Should show "Index Scan" or "Index Only Scan" on the new index

-- Confirm production usage
SELECT indexname, idx_scan, idx_tup_read, idx_tup_fetch
FROM pg_stat_user_indexes
WHERE relname = 'orders';
-- idx_scan should grow as queries run
```

## Common pitfalls

- **Adding an index without verifying it's used** — write amplification for no benefit. Check `idx_scan` in `pg_stat_user_indexes` after a day.
- **Forgetting `ANALYZE` after bulk load** — planner has stale stats; picks Seq Scan even with index.
- **Type mismatch** — `WHERE id = '42'` (string) vs `WHERE id = 42` (int) — index may not be used if column is `int` and query casts.
- **Function on indexed column** — `WHERE LOWER(email) = '...'` doesn't use index on `email`. Add expression index.
- **`LIKE '%term%'`** — leading wildcard prevents B-tree usage. Use `pg_trgm` GIN index or FTS.
- **`SELECT *`** — prevents index-only scan; always list columns.
- **Multi-statement transactions holding locks** — long transaction blocks VACUUM, causes bloat.
- **`OFFSET` at scale** — use cursor pagination.
- **`COUNT(*)` on large table** — slow; maintain a counter table or use approximations (`pg_stat_user_tables.n_live_tup`).

## Before / after documentation

Always document optimizations:

```markdown
## Optimization: orders query

### Baseline (before)
Query: `SELECT * FROM orders WHERE customer_id = 42 AND status = 'pending'`
EXPLAIN: `Seq Scan on orders (actual time=120ms rows=15)`
Execution time: 145ms

### Change
Added index:
  CREATE INDEX CONCURRENTLY idx_orders_customer_status_pending
    ON orders (customer_id) INCLUDE (total_amount, created_at)
    WHERE status = 'pending';

### After
EXPLAIN: `Index Only Scan on idx_orders_customer_status_pending (actual time=0.2ms rows=15)`
Execution time: 0.4ms

### Side effects
- Write amplification: ~5% slower inserts on orders (measured)
- Index size: 12MB
- Re-evaluate after 6 months: drop if idx_scan remains 0
```

## Verification gates

- `EXPLAIN (ANALYZE, BUFFERS)` baseline captured before optimization.
- After change: re-run `EXPLAIN`; cost and time decreased meaningfully (10× or more).
- Index confirmed used in EXPLAIN; `idx_scan > 0` in `pg_stat_user_indexes` after one day.
- No `Seq Scan` on tables > 100K rows in production queries.
- Write performance hasn't regressed (benchmark with `pgbench`).
- Documentation includes before/after metrics + side effects + re-evaluation plan.

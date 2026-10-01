# PostgreSQL 16 — Indexes, EXPLAIN, Partitioning, JSONB, Advisory Locks, Replication

PostgreSQL-specific deep dive: index types, `EXPLAIN (ANALYZE, BUFFERS)` interpretation, declarative partitioning, JSONB operators and indexing, advisory locks for distributed mutex, VACUUM/autovacuum, streaming/logical replication, and config tuning.

## Index types

| Type | Best for | Operators |
|---|---|---|
| B-tree (default) | Equality, range, sorting | `=`, `<`, `>`, `BETWEEN`, `IN`, `IS NULL`, `ORDER BY` |
| Hash | Equality only | `=` (faster than B-tree for pure equality) |
| GIN | Composite values (arrays, JSONB, full-text) | `@>`, `<@`, `?`, `?|`, `?&`, `@@` (FTS) |
| GiST | Geometric, range, exclusion | `<<`, `&<`, `&&`, `<->` (KNN) |
| BRIN | Large tables, naturally ordered (time-series) | Range queries on physically-ordered data; tiny index |
| SP-GiST | Non-balanced (partition trees, radix trees) | Custom partitioning |

```sql
-- B-tree (default)
CREATE INDEX idx_users_email ON users(email);

-- Partial index — only index rows matching WHERE; smaller, faster
CREATE INDEX idx_orders_pending ON orders(customer_id) WHERE status = 'pending';

-- Covering index — INCLUDE columns for index-only scans
CREATE INDEX idx_orders_status_created
  ON orders(status, created_at) INCLUDE (customer_id, total_amount);

-- Unique constraint as index
CREATE UNIQUE INDEX idx_users_username ON users(username);

-- Multi-column — order matters (most selective first, equality before range)
CREATE INDEX idx_events_type_timestamp ON events(type, timestamp);

-- GIN for JSONB containment queries
CREATE INDEX idx_events_payload ON events USING GIN (payload);

-- BRIN for time-series (1KB per 1M rows vs 20MB B-tree)
CREATE INDEX idx_logs_ts_brin ON logs USING BRIN (created_at);

-- Expression index — index the result of a function
CREATE INDEX idx_users_lower_email ON users(LOWER(email));
SELECT * FROM users WHERE LOWER(email) = 'alice@example.com';
```

### Column order rules

1. **Equality before range** — `WHERE type = 'click' AND timestamp > ...` → `(type, timestamp)`.
2. **High selectivity first** — `WHERE user_id = 5 AND status = 'pending'` → `(user_id, status)`.
3. **Match `ORDER BY`** — index can satisfy sort if order matches.
4. **Covering (`INCLUDE`)** — non-filter columns in `INCLUDE`; index-only scan avoids heap fetch.

### When NOT to index

- Small tables (< 1000 rows) — Seq Scan is faster.
- Write-heavy, read-rarely columns — index maintenance costs > query savings.
- Low-cardinality columns alone (e.g., `is_active`) — partial index on `WHERE is_active = true` is better.
- Columns with high churn + many indexes — write amplification.

## EXPLAIN (ANALYZE, BUFFERS)

```sql
EXPLAIN (ANALYZE, BUFFERS, FORMAT TEXT)
SELECT o.id, c.name
FROM orders o
JOIN customers c ON c.id = o.customer_id
WHERE o.status = 'pending' AND o.created_at > now() - interval '7 days';
```

Output:
```
Nested Loop  (cost=0.85..34.12 rows=10 width=44) (actual time=0.045..0.156 rows=87 loops=1)
  Buffers: shared hit=24
  ->  Index Scan using idx_orders_status_created on orders o  (cost=0.42..17.55 rows=10 width=20) (actual time=0.027..0.082 rows=87 loops=1)
        Index Cond: ((status = 'pending'::text) AND (created_at > (now() - '7 days'::interval)))
        Buffers: shared hit=12
  ->  Index Scan using customers_pkey on customers c  (cost=0.43..1.65 rows=1 width=32) (actual time=0.001..0.001 rows=1 loops=87)
        Index Cond: (id = o.customer_id)
        Buffers: shared hit=12
Planning Time: 0.245 ms
Execution Time: 0.189 ms
```

### Reading the output

| Field | Meaning |
|---|---|
| `cost=X..Y` | Startup cost..total cost (planner's estimate, unitless) |
| `rows=N` | Estimated rows; compare to `(actual rows=M loops=L)` |
| `actual time` | Real ms (only with `ANALYZE`) |
| `loops` | How many times the node ran |
| `Buffers: shared hit=X read=Y` | Cache hits vs disk reads (high `read` = cache miss) |
| `Seq Scan` | Full table scan — bad on large tables |
| `Index Scan` | Index + heap fetch |
| `Index Only Scan` | Index covers all columns (covering index worked) |
| `Bitmap Heap Scan` + `Bitmap Index Scan` | Index builds bitmap, then fetches rows |
| `Hash Join` | Build hash on smaller side, probe larger |
| `Nested Loop` | For each outer row, scan inner — bad if outer is large |
| `Merge Join` | Both inputs sorted; merge |
| `Sort Method: external merge` | Sort spilled to disk — increase `work_mem` |

### Diagnosis patterns

| Pattern | Cause | Fix |
|---|---|---|
| `Seq Scan` on 50M-row table | No index, or index not used | Add index; check column type matches |
| `rows=1` estimate but `actual rows=50000` | Stale statistics | `ANALYZE <table>;` |
| `Buffers: hit=10 read=90000` | Cache miss storm | Increase `shared_buffers`; add covering index |
| `Sort Method: external merge Disk: 50000kB` | Sort spills | `SET work_mem = '64MB';` for session |
| `Nested Loop` with `loops=50000` | N+1 inner scans | Hash Join (add index on inner key) |
| `Index Scan` with high cost | Heap fetch dominates | Add covering index (`INCLUDE`) |
| `Index Cond` not matching `WHERE` | Type mismatch (e.g., `integer` vs `bigint`) | Cast in WHERE to match column type |

## Partitioning (declarative, PG 14+)

```sql
-- Range partition by created_at (time-series)
CREATE TABLE orders (
  id BIGSERIAL,
  customer_id BIGINT NOT NULL,
  total NUMERIC(10,2) NOT NULL,
  created_at TIMESTAMPTZ NOT NULL,
  PRIMARY KEY (id, created_at)        -- partition key must be in PK
) PARTITION BY RANGE (created_at);

CREATE TABLE orders_2024_01 PARTITION OF orders
  FOR VALUES FROM ('2024-01-01') TO ('2024-02-01');
CREATE TABLE orders_2024_02 PARTITION OF orders
  FOR VALUES FROM ('2024-02-01') TO ('2024-03-01');
-- ... one per month ...

-- Default partition for anything not matching (catches mistakes)
CREATE TABLE orders_default PARTITION OF orders DEFAULT;

-- List partition by region
CREATE TABLE users (...) PARTITION BY LIST (region);
CREATE TABLE users_us PARTITION OF users FOR VALUES IN ('US');
CREATE TABLE users_eu PARTITION OF users FOR VALUES IN ('EU', 'UK');

-- Hash partition (for write distribution)
CREATE TABLE events (...) PARTITION BY HASH (user_id);
CREATE TABLE events_0 PARTITION OF events FOR VALUES WITH (modulus 4, remainder 0);
CREATE TABLE events_1 PARTITION OF events FOR VALUES WITH (modulus 4, remainder 1);
-- ... 2, 3 ...
```

When to partition:
- Table > 100M rows or > 100GB.
- Time-series data with monthly/daily retention (drop old partitions instantly: `DROP TABLE orders_2022_01`).
- Hot/cold split (recent on fast disk, old on slow disk via tablespace).

`pg_partman` extension automates partition creation + retention.

## JSONB

Binary JSON; indexable, faster than `json` type.

```sql
CREATE TABLE events (id BIGSERIAL PRIMARY KEY, payload JSONB NOT NULL);

-- Operators
SELECT payload->>'type' FROM events;                    -- text (top-level key)
SELECT payload->'user'->>'name' FROM events;            -- text (nested)
SELECT payload #>> '{user,address,city}' FROM events;   -- text (path)

-- Containment (GIN-indexable)
SELECT * FROM events WHERE payload @> '{"type": "login"}';
SELECT * FROM events WHERE payload @> '{"tags": ["postgres"]}';

-- Key existence
SELECT * FROM events WHERE payload ? 'email';
SELECT * FROM events WHERE payload ?| ARRAY['email','phone'];   -- any
SELECT * FROM events WHERE payload ?& ARRAY['email','phone'];   -- all

-- Modification
UPDATE events SET payload = payload || '{"status":"ok"}'::jsonb;     -- merge (shallow)
UPDATE events SET payload = payload - 'temp_field';                   -- remove key
UPDATE events SET payload = jsonb_set(payload, '{user,email}', '"new@x.com"'::jsonb);
```

Indexing JSONB:
```sql
CREATE INDEX idx_events_payload ON events USING GIN (payload);                  -- supports @>, ?, ?|, ?&
CREATE INDEX idx_events_payload_path ON events USING GIN (payload jsonb_path_ops);  -- smaller, only @>
CREATE INDEX idx_events_type ON events ((payload->>'type'));                     -- B-tree on specific key
```

## Advisory locks — distributed mutex

Cheap (no table), session or transaction scoped. Use for cross-process coordination.

```sql
-- Transaction-scoped — auto-released at COMMIT/ROLLBACK
BEGIN;
SELECT pg_advisory_xact_lock(12345);          -- blocks until acquired
-- ... critical section ...
COMMIT;                                        -- lock released

-- Session-scoped — explicit release
SELECT pg_advisory_lock(12345);
-- ... critical section ...
SELECT pg_advisory_unlock(12345);

-- Try-lock (non-blocking)
SELECT pg_try_advisory_xact_lock(12345);       -- returns true/false

-- Use hashed resource name as the lock key
SELECT pg_advisory_xact_lock(hashtext('order:42'));
```

Use cases: migration coordination, cron job leadership election, idempotency envelope. Use 64-bit ints as keys; consider `hashtext()` to convert string names.

## VACUUM + autovacuum

Postgres uses MVCC — old row versions ("dead tuples") accumulate until vacuumed.

```sql
-- Check dead tuple ratio
SELECT relname, n_live_tup, n_dead_tup,
  round(n_dead_tup::numeric / NULLIF(n_live_tup + n_dead_tup, 0) * 100, 2) AS dead_pct,
  last_autovacuum
FROM pg_stat_user_tables
ORDER BY n_dead_tup DESC LIMIT 20;

-- Manual vacuum with stats
VACUUM (ANALYZE, VERBOSE) orders;

-- Aggressive: reclaim space to OS (locks table)
VACUUM FULL orders;                              -- use only when desperate

-- Tune autovacuum per table (high-churn tables)
ALTER TABLE orders SET (
  autovacuum_vacuum_scale_factor = 0.05,         -- default 0.2 = 20% dead
  autovacuum_analyze_scale_factor = 0.02
);
```

Rules: never disable autovacuum globally; for high-churn tables, lower `scale_factor` (vacuum more often, smaller batches); `VACUUM FULL` locks the table — never run in production during traffic; `pg_repack` extension reclaims space without long locks.

## Replication

### Streaming (physical)

Replica is byte-for-byte copy of primary; async by default (sync mode via `synchronous_commit = on`); read-only; promotes to primary on failover. Use for read scaling, HA, PITR.

```sql
-- On primary
SELECT client_addr, state, sent_lsn, replay_lsn,
       (sent_lsn - replay_lsn) AS lag_bytes
FROM pg_stat_replication;

-- On replica
SELECT now() - pg_last_xact_replay_timestamp() AS lag;
```

### Logical

- Replicates specific tables; replica can have its own schema.
- Supports cross-version, cross-engine replication.
- Writeable on target (use cautiously — conflicts possible).
- Use for: zero-downtime upgrades, partial replication, multi-region writes.

```sql
CREATE PUBLICATION my_pub FOR TABLE orders, customers;
-- On target:
CREATE SUBSCRIPTION my_sub
  CONNECTION 'host=primary dbname=mydb'
  PUBLICATION my_pub;
```

## Configuration tuning

```ini
# postgresql.conf — initial production starting points (measure with pgbench)
shared_buffers = 4GB                  # 25% of RAM
effective_cache_size = 12GB           # 75% of RAM (OS cache + shared_buffers)
work_mem = 64MB                       # per-sort, per-hash; raise for analytical
maintenance_work_mem = 512MB          # VACUUM/CREATE INDEX
max_connections = 200                 # use PgBouncer for more
wal_buffers = 16MB
checkpoint_completion_target = 0.9
random_page_cost = 1.1                # SSD; default 4.0 is for spinning disk
effective_io_concurrency = 200        # SSD; 0 for spinning disk
```

Always measure before/after with `pgbench`. Don't cargo-cult settings from blog posts.

## Verification gates

- `EXPLAIN (ANALYZE, BUFFERS)` baseline captured before any optimization.
- After `CREATE INDEX CONCURRENTLY`: re-run `EXPLAIN`; confirm `Index Scan` / `Index Only Scan`.
- `pg_stat_user_indexes` shows the new index is being used (`idx_scan > 0`).
- No `Seq Scan` on tables > 100K rows in production queries.
- Autovacuum running; `n_dead_tup` not growing unbounded.
- Replication lag < 1s for sync, < 60s for async.
- `pg_stat_statements` enabled; top slow queries triaged weekly.

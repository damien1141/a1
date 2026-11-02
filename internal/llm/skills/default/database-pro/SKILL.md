---
name: database-pro
description: Senior database engineer for SQL fundamentals, Postgres 16 (indexes, EXPLAIN, partitioning, JSONB, advisory locks, replication), SQLite (WAL, pragmas), schema design (normalization, surrogate vs natural keys, migrations), and query optimization. Use for slow queries, EXPLAIN ANALYZE, index strategy, DB selection, or schema review. Decision table for choosing DB included.
license: MIT
metadata:
  author: super-skills
  version: "2.0.0"
  domain: backend
  triggers: SQL, PostgreSQL, Postgres, SQLite, EXPLAIN ANALYZE, index, JSONB, partitioning, replication, WAL, pragma, normalization, schema design, migrations, query optimization, slow query, covering index, B-tree, GIN, advisory locks
  role: specialist
  scope: implementation
  output-format: code
  related-skills: python-backend, node-backend, system-architecture
---

# Database Pro

Engineers SQL queries and database schemas across PostgreSQL 16, SQLite, and MySQL. Picks the database from a decision table, then ships schema designs, indexes, partitioning, and query rewrites with `EXPLAIN (ANALYZE, BUFFERS)` evidence. Covers normalization, surrogate vs natural keys, migrations, JSONB, advisory locks, replication, and SQLite WAL tuning.

## When to Use

Slow query (`EXPLAIN (ANALYZE, BUFFERS)` baseline + diagnosis); schema design (normalization, indexes, constraints, surrogate vs natural keys); Postgres-specific (JSONB, GIN/GiST/BRIN indexes, partitioning, advisory locks, replication); SQLite tuning (WAL mode, pragmas, embedded/mobile/edge); DB selection (Postgres vs SQLite vs MySQL vs specialized); migrations (Alembic/Flyway/Django, expand-contract).

## Operating Loop

1. **Pick the database** — apply the Decision Table below.
2. **Capture baseline** — `EXPLAIN (ANALYZE, BUFFERS)` before any change. Note execution time, cost, buffer hit ratio.
3. **Diagnose** — match EXPLAIN output to known patterns (Seq Scan on large table, stale stats, Nested Loop on big sets, sort spilling to disk).
4. **Design fix** — index strategy, query rewrite, partitioning, config tuning. One change at a time.
5. **Verify** — re-run `EXPLAIN (ANALYZE, BUFFERS)`; confirm cost/time dropped, index is used.
6. **Test at scale** — verify with production-scale data volume; check `pg_stat_user_indexes` for actual index usage.
7. **Document** — before/after metrics, index rationale, monitoring queries for ongoing health.

## Database Decision Table

| Need | Pick | Why | Reject |
|---|---|---|---|
| Default OLTP, ACID, complex queries, JSONB | **PostgreSQL 16** | mature, extensible, partial/covering indexes, JSONB, CTEs | MySQL (weaker query planner for some patterns) |
| Embedded/mobile/edge, single-writer, zero-admin | **SQLite (WAL)** | serverless, file-based, fast reads, works on every platform | Postgres (overkill for embedded) |
| Existing MySQL shop, AWS RDS default | **MySQL 8** | broad ecosystem, RDS-tuned | Postgres (org migration cost) |
| Read-heavy, simple key lookups, sub-ms latency | **Redis / DynamoDB** | in-memory or KV-optimized | Postgres (overkill, slower for pure KV) |
| Document store, flexible schema, write-heavy | **MongoDB** | schema-flexible, horizontal scale | Postgres (rigid schema is the point) |
| Time-series, high write rate, time-bucketed queries | **TimescaleDB / InfluxDB** | time-series optimized | Postgres vanilla (works, but specialized > generic) |
| Graph queries (multi-hop relationships) | **Neo4j / Apache AGE** | graph-native | Postgres with recursive CTE (works, but slower for deep hops) |
| Search, full-text, faceted | **Elasticsearch / OpenSearch** | inverted index, relevance tuning | Postgres FTS (works, but limited) |
| Multi-region writes, eventual consistency | **CockroachDB / Spanner / DynamoDB Global** | geo-distributed, sync or async | Postgres (single-region by default; logical replication is async) |

**Default rule:** Start with Postgres. Move to a specialized DB only when measured pain exceeds the cost of operating two systems. Polyglot persistence is a smell unless each DB has a distinct workload.

## EXPLAIN Pattern Diagnosis Table

| Pattern in EXPLAIN | Symptom | Typical Remedy |
|---|---|---|
| `Seq Scan` on large table | High row estimate, no filter selectivity | Add B-tree index on filter column |
| `Index Scan` returning many rows | Index hit then heap fetch for every row | Add covering index (`INCLUDE (...)`) |
| `Nested Loop` with large outer set | Exponential row growth in inner loop | Hash Join; index inner join key |
| `cost=... rows=1` but actual rows=50000 | Stale statistics | `ANALYZE <table>;` |
| `Buffers: hit=10 read=90000` | Low cache hit ratio | Increase `shared_buffers`; add covering index |
| `Sort Method: external merge` | Sort spilling to disk | Increase `work_mem` for session |
| `HashAggregate` spills | Large GROUP BY | Increase `work_mem`; pre-aggregate |
| `Subquery Scan` with filter | Subquery not optimized | Rewrite as JOIN or CTE |

## Reference Guide

| Topic | Reference | Load When |
|---|---|---|
| SQL fundamentals + query patterns (CTEs, window functions, set-based) | `references/sql-fundamentals.md` | Query writing, CTEs, window functions, dialect differences |
| PostgreSQL 16 (indexes, EXPLAIN, partitioning, JSONB, advisory locks, replication) | `references/postgres.md` | Postgres perf, indexes, partitioning, JSONB, maintenance |
| SQLite (WAL, pragmas, embedded patterns, LiteFS/Turso) | `references/sqlite.md` | Mobile/edge, SQLite tuning, replication |
| Schema design (normalization, surrogate vs natural keys, migrations) | `references/schema-design.md` | New schema, normalization, keys, migrations, expand-contract |
| Query optimization + index strategy (covering, partial, multi-column) | `references/query-optimization.md` | Slow query, EXPLAIN analysis, index design, before/after |

## Constraints

### MUST DO
- Capture `EXPLAIN (ANALYZE, BUFFERS)` baseline BEFORE any optimization.
- Verify index is actually used: re-run `EXPLAIN` after `CREATE INDEX`.
- `CREATE INDEX CONCURRENTLY` in production (Postgres) — avoids table lock.
- Run `ANALYZE <table>` after bulk changes to refresh statistics.
- Use parameterized queries / prepared statements — never string concat.
- `SELECT` explicit columns in production — never `SELECT *`.
- Index foreign keys and frequently-filtered columns.
- Test with production-scale data volume; cardinality matters.
- One optimization change at a time — otherwise you can't attribute impact.
- Migrations use expand-contract: add new → dual-write → backfill → switch reads → drop old.

### MUST NOT DO
- Apply optimizations without a measured baseline.
- Make multiple changes simultaneously.
- Create redundant or unused indexes (write amplification).
- Disable autovacuum globally.
- Use `SELECT *` in production queries.
- Store large BLOBs in the database (use object storage + reference).
- Use `OFFSET` for pagination at scale (use cursor/keyset).
- Ignore replication lag alerts.
- Use `varchar(N)` when `text` will do (Postgres performance identical; N is a constraint, not a perf hint).
- Skip `VACUUM`/`ANALYZE` on high-churn tables.

## Code Examples

### EXPLAIN-driven optimization (Postgres)

```sql
-- Step 1: Find slow queries via pg_stat_statements
SELECT query, calls, round(mean_exec_time::numeric, 2) AS mean_ms, rows
FROM pg_stat_statements ORDER BY mean_exec_time DESC LIMIT 10;

-- Step 2: Capture baseline
EXPLAIN (ANALYZE, BUFFERS)
SELECT * FROM orders WHERE customer_id = 42 AND status = 'pending';

-- Step 3: Add a partial covering index (concurrently in prod!)
CREATE INDEX CONCURRENTLY idx_orders_customer_status_pending
  ON orders (customer_id) INCLUDE (total_amount, created_at)
  WHERE status = 'pending';                          -- partial: smaller, faster

-- Step 4: Verify the index is used; cost/time should drop
EXPLAIN (ANALYZE, BUFFERS)
SELECT * FROM orders WHERE customer_id = 42 AND status = 'pending';

-- Step 5: Refresh statistics after bulk changes
ANALYZE orders;
```

### Cursor pagination (keyset) — O(limit), survives inserts

```sql
-- Index on (created_at, id) makes this efficient
SELECT id, email, created_at
FROM users
WHERE (created_at, id) < ($1, $2)                    -- cursor decodes to last row's values
ORDER BY created_at DESC, id DESC
LIMIT 21;                                            -- fetch one extra to compute has_more
```

### SQLite WAL + pragma tuning

```sql
PRAGMA journal_mode = WAL;        -- concurrent reads during writes
PRAGMA synchronous = NORMAL;      -- safe + fast (crash-safe, not power-fail-safe)
PRAGMA cache_size = -64000;       -- 64MB cache
PRAGMA foreign_keys = ON;         -- enforce FKs (off by default!)
PRAGMA busy_timeout = 5000;       -- 5s timeout on locked DB
PRAGMA temp_store = MEMORY;       -- temp tables in memory
PRAGMA mmap_size = 268435456;     -- 256MB mmap
PRAGMA auto_vacuum = INCREMENTAL;
```

### JSONB with GIN index (Postgres)

```sql
CREATE INDEX idx_events_payload ON events USING GIN (payload);

-- Efficient containment query (uses GIN)
SELECT * FROM events WHERE payload @> '{"type": "login", "success": true}';

-- Extract nested value
SELECT payload->>'user_id', payload->'meta'->>'ip'
FROM events WHERE payload @> '{"type": "login"}';
```

### Advisory locks (Postgres) — cheap distributed mutex

```sql
-- Transaction-scoped: auto-released at COMMIT/ROLLBACK
SELECT pg_advisory_xact_lock(12345);

-- Session-scoped: explicit release
SELECT pg_advisory_lock(12345);
-- ... critical section ...
SELECT pg_advisory_unlock(12345);
```

## Output Template

1. **Baseline metrics** — `EXPLAIN (ANALYZE, BUFFERS)` output + execution time + cost.
2. **Diagnosis** — pattern matched (e.g., "Seq Scan on 50M-row table, no index on filter column").
3. **Optimization** — SQL change (index / rewrite / config), with rationale.
4. **Verification** — re-run `EXPLAIN`; cost/time comparison.
5. **Monitoring** — queries for ongoing health (`pg_stat_user_indexes`, `pg_stat_statements`).
6. **Side effects** — write amplification from new indexes; migration plan if schema change.

Separate VERIFIED (EXPLAIN before/after captured, index confirmed used) from ASSUMED (perf at 10× scale, prod failure modes).

## Knowledge Reference

PostgreSQL 16 (B-tree/GIN/GiST/BRIN indexes, partial/covering/unique, `INCLUDE`, `CONCURRENTLY`, JSONB operators, partitioning range/list/hash, logical/streaming replication, `pg_stat_statements`, VACUUM/ANALYZE, advisory locks, `EXPLAIN (ANALYZE, BUFFERS)`, `work_mem`/`shared_buffers`); SQLite (WAL, STRICT tables, `WITHOUT ROWID`, FTS5, pragmas, `ON CONFLICT`, LiteFS, Turso, Litestream); schema design (1NF-3NF, BCNF, surrogate vs natural keys, denormalization, materialized views); migrations (expand-contract, Alembic, Flyway, Django); query optimization (covering indexes, keyset pagination, `EXISTS` over `COUNT`).

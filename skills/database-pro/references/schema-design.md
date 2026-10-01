# Schema Design — Normalization, Keys, Migrations, Expand-Contract

Schema design rules: normalization (1NF-3NF, BCNF), surrogate vs natural keys, composite keys, constraints, indexes, and the expand-contract migration pattern for zero-downtime schema changes.

## Normalization

| Form | Rule | Example violation |
|---|---|---|
| 1NF | Atomic values; no repeating groups | `users.tags = "a,b,c"` (should be a separate table) |
| 2NF | 1NF + non-key attributes depend on whole key | `OrderItem(product_id, order_id) → product_name` (product_name depends only on product_id) |
| 3NF | 2NF + no transitive dependencies | `User(id, zip, city)` (city depends on zip, not id) |
| BCNF | 3NF + every determinant is a candidate key | Stricter 3NF; rare edge cases |

Normalize by default. Denormalize deliberately when:
- A query runs > 100ms and can't be fixed by indexes.
- A join crosses services (not allowed — see microservices reference).
- Read load >> write load and the denormalized copy can be refreshed from events.

Denormalize via materialized views (Postgres) or projections (CQRS), not by polluting the source tables.

## Keys

### Surrogate keys

Artificial ID, usually UUID or auto-increment integer. No business meaning.

```sql
CREATE TABLE users (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  email TEXT NOT NULL UNIQUE,
  ...
);
```

Pros:
- Stable — never changes even if business key changes.
- Uniform — every table has the same PK type.
- Decoupled — schema changes don't cascade through FKs.
- Sharding-friendly — UUIDs distribute evenly.

Cons:
- Larger (16 bytes UUID vs 4 bytes int) — more index space, more cache pressure.
- UUID v4 random → B-tree fragmentation. Use UUIDv7 (time-ordered) or `IDENTITY` integers.

### Natural keys

Use a real-world identifier as the PK (e.g., `isbn` for books, `icao_code` for airports).

```sql
CREATE TABLE airports (
  icao_code CHAR(4) PRIMARY KEY,
  name TEXT NOT NULL,
  ...
);
```

Pros:
- Smaller (often) and human-meaningful.
- No join needed to display the key.

Cons:
- Can change (rare but painful — `UPDATE` cascades to all FKs).
- May not exist for all entities at insert time.
- Format can change (ISBN-10 → ISBN-13).

Use natural keys when: the identifier is truly immutable and universally agreed (airport codes, country codes). Use surrogate keys otherwise.

### Composite keys

Multi-column primary key.

```sql
CREATE TABLE order_items (
  order_id BIGINT NOT NULL REFERENCES orders(id),
  sku TEXT NOT NULL,
  quantity INT NOT NULL,
  unit_price NUMERIC(10,2) NOT NULL,
  PRIMARY KEY (order_id, sku)
);
```

Pros:
- Natural uniqueness enforced at DB level.
- Smaller than adding a surrogate `id` column for junction tables.

Cons:
- Wider FKs in child tables.
- ORM support varies.

Use composite keys for pure junction tables (`user_roles(user_id, role_id)`). Use surrogate keys for everything else.

## Constraints

Constraints are bulletproof — they're enforced at the DB level, regardless of ORM, raw SQL, or admin tool.

```sql
CREATE TABLE products (
  id BIGSERIAL PRIMARY KEY,
  name TEXT NOT NULL,
  price NUMERIC(10,2) NOT NULL CHECK (price >= 0),
  stock INT NOT NULL DEFAULT 0 CHECK (stock >= 0),
  sku TEXT NOT NULL UNIQUE,
  category TEXT NOT NULL CHECK (category IN ('electronics', 'clothing', 'food')),
  metadata JSONB NOT NULL DEFAULT '{}'::jsonb,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- Composite unique
ALTER TABLE products ADD CONSTRAINT products_name_category_unique
  UNIQUE (name, category);

-- Foreign key with explicit action
ALTER TABLE order_items
  ADD CONSTRAINT order_items_product_fk
  FOREIGN KEY (product_id) REFERENCES products(id)
  ON DELETE RESTRICT;                              -- don't allow delete if referenced

-- Exclusion constraint (no overlapping date ranges for the same resource)
ALTER TABLE bookings ADD CONSTRAINT bookings_no_overlap
  EXCLUDE USING gist (resource_id WITH =, tstzrange(start_time, end_time) WITH &&);
```

Rules:
- `NOT NULL` on every column that shouldn't be null (most of them).
- `CHECK` for value ranges, enums, invariants.
- `UNIQUE` for natural uniqueness (email, SKU).
- `FOREIGN KEY` for referential integrity; pick `ON DELETE` action explicitly (`CASCADE`, `SET NULL`, `RESTRICT`, `NO ACTION`).
- Default to `RESTRICT` (or `NO ACTION`) — prevents accidental mass deletion. Use `CASCADE` only for true child entities (`order_items` cascading from `orders`).

## Indexes (summary — see `references/postgres.md` for depth)

- One index per foreign key (FK columns are join targets).
- One index per `WHERE` filter on a column with > 1000 rows.
- Composite index for multi-column `WHERE`; order matters (equality first, then range).
- Partial index when the query filters on a constant (`WHERE status = 'pending'`).
- Covering index (`INCLUDE`) when the query projects non-filter columns.
- Don't over-index — every index slows writes.

## Naming conventions

Pick one and apply consistently. Common conventions:

| Object | Convention | Example |
|---|---|---|
| Table | snake_case, plural | `order_items`, `users` |
| Column | snake_case | `created_at`, `customer_id` |
| PK | `<table_singular>_pkey` (auto) | `users_pkey` |
| FK | `<table>_<ref_table>_fk` | `orders_customer_fk` |
| Unique | `<table>_<col>_uniq` | `users_email_uniq` |
| Index | `idx_<table>_<cols>` | `idx_orders_customer_status` |
| Junction table | `<a>_<b>` alphabetical | `roles_users` |

Consistency > the specific convention chosen.

## Data types

| Need | Postgres type | Notes |
|---|---|---|
| Boolean | `BOOLEAN` | true/false/NULL; never use INT |
| Integer | `INT` / `BIGINT` / `SMALLINT` | Pick size that fits |
| Auto-increment | `BIGSERIAL` or `BIGINT GENERATED ALWAYS AS IDENTITY` | `IDENTITY` is SQL-standard, preferred |
| Decimal money | `NUMERIC(10,2)` | Never `FLOAT` / `REAL` for money |
| UUID | `UUID` | `gen_random_uuid()` default; not `TEXT` |
| Timestamp | `TIMESTAMPTZ` | Always with TZ; never `TIMESTAMP` without TZ |
| Date | `DATE` | |
| Short text | `TEXT` | Don't use `VARCHAR(N)` for perf (Postgres identical) |
| Bounded text | `VARCHAR(N)` | Use only when N is a real business constraint |
| JSON | `JSONB` | Binary, indexable; never `JSON` (text) |
| Binary | `BYTEA` | Or store in object storage + reference |
| Array | `TEXT[]`, `INT[]` | Use sparingly; consider junction table |
| Enum | `TEXT` + `CHECK` or `CREATE TYPE` | `CREATE TYPE` is stricter but harder to migrate |

## Migrations

### Expand-contract pattern (zero-downtime)

Two-phase deploy. Phase 1 (expand): add new structure, dual-write. Phase 2 (contract): drop old structure after readers switched.

```sql
-- Original schema
CREATE TABLE users (id BIGSERIAL PRIMARY KEY, email TEXT NOT NULL UNIQUE, full_name TEXT);

-- ===== Expand (deploy 1) =====

-- Step 1: Add new columns (nullable, no not-null yet)
ALTER TABLE users ADD COLUMN first_name TEXT;
ALTER TABLE users ADD COLUMN last_name TEXT;

-- Step 2: Backfill existing rows
UPDATE users SET
  first_name = split_part(full_name, ' ', 1),
  last_name  = substring(full_name from position(' ' in full_name) + 1)
WHERE full_name IS NOT NULL AND first_name IS NULL;

-- Step 3: Deploy app code that writes both (dual-write)
-- INSERT INTO users (email, full_name, first_name, last_name) VALUES (...)

-- Step 4: Deploy app code that reads from new columns

-- Step 5: Backfill any rows written between step 2 and step 3 (idempotent)
UPDATE users SET
  first_name = split_part(full_name, ' ', 1),
  last_name  = substring(full_name from position(' ' in full_name) + 1)
WHERE full_name IS NOT NULL AND first_name IS NULL;

-- ===== Contract (deploy 2, after step 4 verified) =====

-- Step 6: Stop writing to full_name (deploy app code change)

-- Step 7: Add NOT NULL to new columns (after verifying no NULLs remain)
ALTER TABLE users ALTER COLUMN first_name SET NOT NULL;
ALTER TABLE users ALTER COLUMN last_name SET NOT NULL;

-- Step 8: Drop old column (after a verification window — typically 1 week)
ALTER TABLE users DROP COLUMN full_name;
```

Rules:
- **Expand phase is backward-compatible.** Old code reads old structure, writes both; new code reads new structure, writes both.
- **Contract phase runs after all readers are on new code.** Drop the old structure.
- **Backfills are idempotent.** Re-running them doesn't break anything.
- **Big backfills run in batches** to avoid locking the table:
  ```sql
  UPDATE users SET first_name = ... WHERE id BETWEEN 1 AND 10000 AND first_name IS NULL;
  -- repeat in batches of 10K until 0 rows updated
  ```
- **Add `NOT NULL` last**, after verifying no NULLs remain.

### Migration tools

| Tool | Stack | Notes |
|---|---|---|
| Alembic | Python + SQLAlchemy | Autogenerate from model diff |
| Flyway | JVM | SQL-based, versioned |
| Liquibase | JVM | XML/YAML/SQL; powerful but verbose |
| Django migrations | Django | Auto-generated from model changes |
| Knex migrations | Node | JS-based, both up and down |
| Atlas | Go | Declarative schema-as-code |
| sqitch | Any | Change-set oriented, no framework lock-in |

### Migration anti-patterns

- **Big-bang migrations** that lock tables for minutes — always batch.
- **Drop column without verification window** — you'll find out a service still reads it.
- **Rename column directly** — Postgres doesn't support rename without breaking old code. Use expand-contract: add new column, dual-write, switch readers, drop old.
- **`ALTER TABLE ... ADD COLUMN ... NOT NULL` without default** on a populated table — fails or locks. Add nullable, backfill, then set NOT NULL.
- **`ALTER TYPE` on Postgres enums** — can require table rewrite. Add new value first, deploy code that uses it, then remove old value (if safe).
- **Trusting autogenerated migrations blindly** — autogen misses check constraints, partial indexes, custom functions. Always review.

## Squashing migrations

After many migrations, the migration history becomes long. Squash periodically:

```bash
# Django
python manage.py squashmigrations app_name 0001 0050  # combine 50 migrations into 1

# Alembic
# Manually: create a new baseline migration reflecting current schema; delete old ones
```

Squash only after all environments have applied the original migrations. Squashed migrations should produce identical schema to the originals.

## Verification gates

- Schema is in 3NF (or denormalization is deliberate + documented).
- Every FK has an explicit `ON DELETE` action.
- Every column is `NOT NULL` unless NULL is semantically meaningful.
- Every CHECK constraint enforced in DB (not just in app code).
- Migrations run cleanly on a fresh DB (`dropdb && createdb && migrate`).
- Expand-contract for breaking changes; deploy 1 (expand) doesn't break old code.
- Backfill tested on production-scale data (estimate time; lock duration measured).
- Migration rollback tested — every migration has a down/undo path (or explicitly marked irreversible).

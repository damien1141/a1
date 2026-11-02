# SQLite — WAL, Pragmas, Embedded Patterns, Replication

SQLite for embedded, mobile, edge, and small-server workloads. Single-file database, zero administration, serverless. Tune with pragmas; use WAL for concurrency; replicate with LiteFS/Turso/Litestream for distribution.

## When SQLite

| Need | Pick |
|---|---|
| Mobile app (iOS, Android) | SQLite (Room on Android, GRDB on iOS) |
| Desktop app local storage | SQLite |
| Edge / serverless function with cold start | SQLite (Cloudflare D1, Turso) |
| Single-writer app (CLI, dev tool) | SQLite |
| Read-heavy app, < 100K writes/day | SQLite (WAL) |
| Multi-process server with > 100 writes/sec | Postgres (SQLite single-writer bottleneck) |
| Multi-region distributed writes | Turso/libSQL or Postgres+CockroachDB |

SQLite is not a replacement for Postgres at scale — it's a different tool. Single-writer architecture means write throughput is bounded by disk I/O on one machine.

## Setup + pragmas

```sql
-- Run once at connection open
PRAGMA journal_mode = WAL;           -- Write-Ahead Logging for concurrent reads + writes
PRAGMA synchronous = NORMAL;         -- Safe + fast; crash-safe, not power-fail-safe
PRAGMA cache_size = -64000;          -- 64MB cache (negative = KB)
PRAGMA foreign_keys = ON;            -- Enforce FKs (OFF by default!)
PRAGMA busy_timeout = 5000;          -- 5s timeout when DB is locked
PRAGMA temp_store = MEMORY;          -- Temp tables + sorts in memory
PRAGMA mmap_size = 268435456;        -- 256MB memory-mapped I/O
PRAGMA auto_vacuum = INCREMENTAL;    -- Reclaim space without full VACUUM
PRAGMA wal_autocheckpoint = 1000;    -- Checkpoint every 1000 pages (~4MB)
```

### Synchronous levels

| Value | Durability | Speed | When |
|---|---|---|---|
| `OFF` | None — data loss on power fail | Fastest | Dev only |
| `NORMAL` | Crash-safe (no corruption); may lose last txn on power fail | Fast | **Default for WAL** |
| `FULL` | Power-fail-safe | Slower | Financial data |
| `EXTRA` | Most durable | Slowest | Overkill usually |

With WAL mode, `NORMAL` is the recommended balance — no corruption risk, only last-transaction-loss on power failure.

### Foreign keys — OFF by default

```sql
PRAGMA foreign_keys = ON;            -- Must set per connection!

-- Or in your driver:
-- Node: await db.pragma('foreign_keys = ON');
-- Python: conn.execute('PRAGMA foreign_keys = ON')
```

Without this pragma, FK constraints are silently ignored. Always enable.

## Schema design

### STRICT tables (3.37+) — type enforcement

```sql
CREATE TABLE users (
    id INTEGER PRIMARY KEY,
    email TEXT NOT NULL UNIQUE,
    name TEXT NOT NULL,
    metadata TEXT DEFAULT '{}',       -- JSON stored as text
    created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
    updated_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now'))
) STRICT;                              -- Rejects wrong-type inserts

-- Without STRICT, SQLite uses type affinity (lenient — any value can go in any column)
```

### WITHOUT ROWID — lookup tables

```sql
-- Normal: implicit rowid, secondary index on key
CREATE TABLE kv (key TEXT PRIMARY KEY, value TEXT);
-- rowid (1, 2, 3, ...) + index on key — two lookups

-- WITHOUT ROWID: PK is the row locator
CREATE TABLE kv (key TEXT PRIMARY KEY, value TEXT) WITHOUT ROWID;
-- single B-tree lookup; faster for point queries; smaller
```

Use `WITHOUT ROWID` for: lookup tables, key-value stores, junction tables. Don't use when you need auto-increment ID or `rowid` aliasing.

### Timestamps

```sql
-- ISO-8601 text (sortable, human-readable)
created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now'))

-- Unix epoch integer (compact, fast arithmetic)
created_at INTEGER NOT NULL DEFAULT (strftime('%s', 'now'))
```

Pick one; stay consistent. ISO-8601 text is more debuggable; integer is slightly faster.

## Query patterns

### UPSERT + RETURNING

```sql
INSERT INTO users (id, email, name)
VALUES (1, 'a@b.com', 'Alice')
ON CONFLICT(id) DO UPDATE SET name = excluded.name
RETURNING id, email, name;            -- 3.35+
```

### JSON functions

```sql
-- Store JSON in TEXT columns; use json_extract for reads
SELECT json_extract(metadata, '$.role') FROM users;

-- Filter on JSON value
SELECT * FROM users WHERE json_extract(metadata, '$.role') = 'admin';

-- Index the expression
CREATE INDEX idx_users_role ON users (json_extract(metadata, '$.role'));

-- Aggregate into JSON
SELECT department, json_group_array(name) AS members
FROM employees GROUP BY department;

-- Expand array
SELECT value FROM json_each('[1, 2, 3]');
```

### Window functions (3.25+)

```sql
SELECT
  product_id,
  sale_date,
  amount,
  ROW_NUMBER() OVER (PARTITION BY product_id ORDER BY sale_date DESC) AS rn,
  SUM(amount) OVER (PARTITION BY product_id ORDER BY sale_date) AS running_total
FROM sales;
```

### FTS5 — full-text search

```sql
CREATE VIRTUAL TABLE articles_fts USING fts5(title, body, tokenize = 'porter');

INSERT INTO articles_fts (title, body) VALUES ('Hello World', '...');

SELECT * FROM articles_fts WHERE articles_fts MATCH 'hello';
SELECT * FROM articles_fts WHERE articles_fts MATCH '"hello world"';  -- phrase
SELECT * FROM articles_fts WHERE articles_fts MATCH 'hello OR world'; -- boolean
SELECT snippet(articles_fts, 1, '<b>', '</b>', '...', 20) FROM articles_fts WHERE articles_fts MATCH 'hello';
```

## Concurrency model

### WAL mode

- Writers append to `*-wal` file; readers see snapshot.
- Multiple readers + one writer concurrently.
- Checkpoint moves WAL contents back to main DB (`wal_autocheckpoint`).
- Readers don't block writers; writers don't block readers.

### Single writer

- Only one write transaction at a time.
- Other writers wait (up to `busy_timeout`) or get `SQLITE_BUSY`.
- For high write throughput, batch writes in a single transaction.

### Multi-process

- Each process opens its own connection.
- WAL allows concurrent reads from multiple processes.
- Writes serialize via file locks.
- Don't put the DB on NFS — file locking is unreliable.

### Transactions for batch writes

```sql
BEGIN;
INSERT INTO logs (msg) VALUES ('a');
INSERT INTO logs (msg) VALUES ('b');
-- ... 1000 more ...
COMMIT;
-- One fsync at COMMIT vs 1000 fsyncs without transaction
```

Without explicit transactions, SQLite auto-commits each statement = one fsync per write = slow.

## Application integration

### Node.js — better-sqlite3 (synchronous, fast)

```javascript
const Database = require('better-sqlite3');
const db = new Database('app.db');
db.pragma('journal_mode = WAL');
db.pragma('foreign_keys = ON');

const insert = db.prepare('INSERT INTO users (email, name) VALUES (?, ?)');
const tx = db.transaction((users) => {
  for (const u of users) insert.run(u.email, u.name);
});
tx([{ email: 'a@b.com', name: 'Alice' }, { email: 'c@d.com', name: 'Carol' }]);
```

### Python — sqlite3 stdlib

```python
import sqlite3
conn = sqlite3.connect('app.db')
conn.execute('PRAGMA journal_mode = WAL')
conn.execute('PRAGMA foreign_keys = ON')
conn.row_factory = sqlite3.Row

conn.execute('INSERT INTO users (email, name) VALUES (?, ?)', ('a@b.com', 'Alice'))
conn.commit()
```

### Python — aiosqlite (async)

```python
import aiosqlite
async with aiosqlite.connect('app.db') as db:
    await db.execute('PRAGMA journal_mode = WAL')
    await db.execute('INSERT INTO users (email, name) VALUES (?, ?)', ('a@b.com', 'Alice'))
    await db.commit()
```

### Go — modernc.org/sqlite (pure Go, no CGO)

```go
import _ "modernc.org/sqlite"
db, _ := sql.Open("sqlite", "app.db?_pragma=journal_mode(WAL)&_pragma=foreign_keys(on)")
```

`mattn/go-sqlite3` requires CGO; `modernc.org/sqlite` is pure Go (easier cross-compilation).

### Bun — bun:sqlite (built-in, fast)

```typescript
import { Database } from 'bun:sqlite';
const db = new Database('app.db');
db.exec('PRAGMA journal_mode = WAL');
const q = db.query('SELECT * FROM users WHERE id = ?');
const user = q.get(1);
```

## Backup + replication

### Online Backup API (in-process)

```python
import sqlite3
src = sqlite3.connect('app.db')
dst = sqlite3.connect('backup.db')
src.backup(dst)
```

Doesn't lock the source; safe for live databases.

### Litestream — continuous S3 replication

```bash
litestream replicate app.db s3://my-bucket/db
litestream restore app.db                    # recover from S3
```

Streams WAL changes to S3 continuously. RPO < 1s. Use for disaster recovery of single-node SQLite.

### LiteFS — distributed SQLite

Multiple nodes share one logical database via WAL replication. Writes go to a primary; reads from any node. Use for multi-region read scaling with single-writer semantics.

### Turso / libSQL — edge SQLite

SQLite-compatible fork with built-in replication to edge locations. Writes go to a primary; edge replicas cache reads locally. Use for serverless/edge apps that need a DB close to users.

## Backups + integrity check

```bash
# Backup via .backup command (online, consistent)
sqlite3 app.db ".backup backup.db"

# Integrity check
sqlite3 app.db "PRAGMA integrity_check;"
sqlite3 app.db "PRAGMA foreign_key_check;"

# Optimize (runs ANALYZE + small vacuum)
sqlite3 app.db "PRAGMA optimize;"
```

Run `PRAGMA integrity_check` after backups and after crashes. Run `PRAGMA optimize` periodically (or set `PRAGMA optimize_on_exit = ON`).

## Common pitfalls

- **Foreign keys silently ignored** — forgot `PRAGMA foreign_keys = ON` per connection.
- **Slow single inserts** — no explicit transaction; each insert fsyncs.
- **DB on NFS** — file locking broken; corrupts under concurrent access.
- **DB on network drive** — same as NFS; use Postgres instead.
- **No `WAL` mode** — default `DELETE` journal mode blocks readers during writes.
- **`VACUUM` while running** — locks the DB; use `PRAGMA auto_vacuum = INCREMENTAL` instead.
- **Huge DB files** — `PRAGMA wal_checkpoint(TRUNCATE)` to reclaim WAL space; consider `INCREMENTAL` auto_vacuum.
- **Cross-platform path issues** — `strftime('%Y-%m-%dT%H:%M:%fZ', 'now')` is UTC; store TZ-aware strings.

## Verification gates

- `PRAGMA journal_mode` returns `wal`.
- `PRAGMA foreign_keys` returns `1` (on).
- `PRAGMA integrity_check` returns `ok`.
- `EXPLAIN QUERY PLAN` shows index usage, not `SCAN` on large tables.
- Load test: concurrent readers + 1 writer; verify no `SQLITE_BUSY` errors (within `busy_timeout`).
- Backup test: restore from backup; integrity check passes.
- For Litestream: `litestream generations` shows recent generations; restore completes within RPO target.

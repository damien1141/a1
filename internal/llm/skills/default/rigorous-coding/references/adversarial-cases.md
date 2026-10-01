# Adversarial Cases

The adversarial matrix: five classes of input that break code which "looks right." Run this matrix on every external call, every parse, every collection access, every boundary.

## The five cases

| Case | Question | Canonical inputs |
|------|----------|------------------|
| **Empty** | What if there is nothing? | `[]`, `""`, `0`, `null`, `undefined`, `{}`, `None`, empty stream |
| **Missing** | What if a required piece is absent? | Required field not in payload; env var unset; header absent; key not in dict |
| **Malformed** | What if the shape is wrong? | Wrong type (`"42"` vs `42`), wrong encoding (latin-1 read as UTF-8), structurally invalid JSON, negative where positive expected |
| **Concurrent** | What if two actors collide? | Two callers racing on shared state; mutation during iteration; partial write visible mid-operation; cache stampede |
| **Large** | What if the scale is hostile? | 10M-row file, 100k-key object, deeply nested recursion, integer overflow, OOM via unbounded allocation |

The order matters for triage: **empty** and **missing** are common and easy to handle. **Malformed** is common and easy to get subtly wrong. **Concurrent** is rarer but catastrophic when it bites. **Large** is the difference between "works in dev" and "works in prod."

## How to apply the matrix

For each input or external call in your change, write a one-line decision per case:

```text
parseUser(raw):
  empty    → return null
  missing  → return null + log (don't crash)
  malformed→ return null (type-check before access)
  concurrent→ snapshot raw into const before reading
  large    → reject if > 1000 keys (DoS guard, throw)
```

If a case "can't happen," write the proof, not just the assertion:

```text
events: list[Event]
  empty    → CAN'T HAPPEN: caller pipeline.py:88 asserts len(events) > 0
             before calling. (Documented; if removed, dedup returns [] safely.)
  missing  → CAN'T HAPPEN: Event is a dataclass with required fields, enforced
             by schema.py:14 at deserialization.
  ...
```

A bare "can't happen" with no proof is an assumption. A "can't happen" with a `file:line` citation is a documented invariant. They are not the same.

## Worked examples

### Example 1 — HTTP handler (TypeScript)

```typescript
// POST /users  body: { name: string, email: string, role?: string }
//
// Adversarial matrix:
//   empty    : body is {} or missing entirely         → 400 "missing fields"
//   missing  : name present, email absent             → 400 "email required"
//   malformed: email is "not-an-email" or role="xxx"  → 400 with field detail
//   concurrent: two POST /users same email at once    → DB unique constraint, 409
//   large    : body > 64KB or name > 1KB              → 413 / 400, reject early
app.post('/users', async (req, res) => {
  const body = req.body;
  if (!body || typeof body !== 'object') return res.status(400).json({ error: 'missing body' });
  const { name, email, role } = body as Record<string, unknown>;

  if (typeof name !== 'string' || name.length === 0) return res.status(400).json({ error: 'name required' });
  if (name.length > 1024) return res.status(400).json({ error: 'name too long' });
  if (typeof email !== 'string' || !/^[^@]+@[^@]+\.[^@]+$/.test(email))
    return res.status(400).json({ error: 'email malformed' });
  if (role !== undefined && !['admin', 'user', 'guest'].includes(role as string))
    return res.status(400).json({ error: 'role invalid' });

  try {
    const user = await users.create({ name, email, role: role ?? 'user' });
    return res.status(201).json(user);
  } catch (e) {
    if (isUniqueViolation(e)) return res.status(409).json({ error: 'email already exists' }); // concurrent
    throw e; // let the global handler 500 it
  }
});
```

Also: enforce `express.json({ limit: '64kb' })` at the middleware layer so the **large** case is rejected before your code runs.

### Example 2 — Config loader (Python)

```python
# load_config(path: Path) -> Config
#
# Adversarial matrix:
#   empty    : file exists but is empty         → return Config() defaults
#   missing  : file does not exist              → return Config() defaults, log once
#   malformed: file is valid YAML but wrong shape → raise ConfigError with file:line
#                                              : file is invalid YAML          → raise with yaml error
#   concurrent: writer is mid-flush when we read → retry once on truncation; fail loudly otherwise
#   large    : file > 1MB                        → reject, log "config unexpectedly large"
def load_config(path: Path) -> Config:
    if not path.exists():
        logger.warning("config missing at %s, using defaults", path)
        return Config()
    if path.stat().st_size > 1_048_576:
        raise ConfigError(f"config file >1MB at {path}, refusing to load")

    raw = path.read_text(encoding="utf-8")          # explicit encoding (malformed UTF-8 raises)
    if not raw.strip():
        return Config()                             # empty

    try:
        data = yaml.safe_load(raw)                  # safe_load, not load (code exec)
    except yaml.YAMLError as e:
        raise ConfigError(f"malformed YAML at {path}: {e}") from e

    if not isinstance(data, dict):
        raise ConfigError(f"config root must be mapping, got {type(data).__name__}")

    return Config.from_dict(data)
```

Note: `yaml.load` (without `safe_load`) is a remote-code-execution vulnerability. The **malformed** case here is not just "ugly data," it's "arbitrary Python."

### Example 3 — Concurrent update (Go)

```go
// UpdateBalance updates a user's balance atomically.
//
// Adversarial matrix:
//   empty    : amount == 0            → no-op, return current balance
//   missing  : user not found         → return ErrUserNotFound (don't create)
//   malformed: amount is NaN/Inf      → reject (math.IsNaN check)
//   concurrent: two updates race      → use DB-level row lock / UPDATE ... RETURNING
//   large    : amount overflows int64 → reject; balance is int64 cents
func (s *Store) UpdateBalance(ctx context.Context, userID int64, amount int64) (int64, error) {
    if amount == 0 {
        return s.GetBalance(ctx, userID)
    }
    if math.IsNaN(float64(amount)) { // int64 can't be NaN, but document the invariant
        return 0, errors.New("amount must be finite")
    }

    // Concurrent: rely on the DB transaction + row lock.
    tx, err := s.db.BeginTx(ctx, nil)
    if err != nil { return 0, err }
    defer tx.Rollback()

    var newBalance int64
    err = tx.QueryRowContext(ctx,
        `UPDATE users SET balance = balance + $1 WHERE id = $2 RETURNING balance`,
        amount, userID,
    ).Scan(&newBalance)
    if errors.Is(err, sql.ErrNoRows) {
        return 0, ErrUserNotFound // missing
    }
    if err != nil { return 0, err }

    // Large: overflow guard — DB int64 won't overflow, but check anyway.
    if (amount > 0 && newBalance < 0) || (amount < 0 && newBalance > 0) {
        return 0, ErrOverflow
    }
    return newBalance, tx.Commit()
}
```

### Example 4 — Large input streaming (Rust)

```rust
// count_lines(path) -> u64
//
// Adversarial matrix:
//   empty    : file is empty           → 0
//   missing  : file does not exist     → IoError
//   malformed: file is not UTF-8       → use bytes, count b'\n', don't decode
//   concurrent: file truncated mid-read → IoError, surface it
//   large    : 100GB file              → stream, do not read into memory
fn count_lines(path: &Path) -> io::Result<u64> {
    let mut file = File::open(path)?;            // missing → IoError
    let mut buf = [0u8; 64 * 1024];
    let mut count: u64 = 0;
    loop {
        let n = file.read(&mut buf)?;            // streaming, O(1) memory
        if n == 0 { break; }                     // empty file → 0 iterations → 0
        count += buf[..n].iter().filter(|&&b| b == b'\n').count() as u64;
    }
    Ok(count)
}
```

Reading 100GB into memory "works in dev" with the 1KB test file. The **large** case is the difference between dev and prod.

## Beyond the matrix — wrong-but-plausible

Some bugs don't crash; they return the wrong answer confidently. Add these to your red-team pass:

| Failure mode | Example | Mitigation |
|--------------|---------|------------|
| Off-by-one | `range(len(x))` vs `range(len(x)-1)` | Property test: round-trip |
| Timezone-naive | `datetime.now()` stored as UTC | Use `datetime.now(tz=UTC)`; assert tz-aware |
| Locale-dependent parsing | `float("1,5")` in DE vs US | Parse with explicit locale or fixed regex |
| Silent type coercion | `[] == 0` is `false` but `[] == ![]` is `true` in JS | Strict equality (`===`), type guards |
| Integer division | `5 / 2 == 2` in Python 2 / Go | Use float where fractional, document where int |
| Float equality | `0.1 + 0.2 != 0.3` | Epsilon compare or use Decimal/Rational |
| Cache staleness | Stale cache returns "deleted" record | Version keys or use TTL + invalidation |
| Time-of-check vs time-of-use | Check then act, race in between | Atomic check-and-act (DB constraint, lock) |
| Default-true booleans | `if (config.featureEnabled)` when field absent | Explicit tri-state: `undefined` ≠ `false` |

## Self-review checklist before commit

Run this on the diff. Each ✗ is a gate-3 failure.

```text
[ ] Every external call (HTTP, DB, file, subprocess) handles empty input explicitly.
[ ] Every required field access has a missing-case branch or a proven invariant.
[ ] Every parse uses a strict parser (not eval, not yaml.load, not JSON.parse of untrusted input without size cap).
[ ] Shared mutable state has either: a lock, an atomic op, immutability, or a documented single-writer contract.
[ ] Unbounded inputs (lists, streams, strings) have an explicit cap or streaming strategy.
[ ] No "this can't happen" without a file:line proof.
[ ] At least one test per case (5 cases = 5 tests minimum for critical paths).
```

If you cannot check a box, either fix the code or add an entry to your ASSUMED list in the exit report.

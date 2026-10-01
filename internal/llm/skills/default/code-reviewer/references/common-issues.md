# Common Code Issues

A catalog of recurring issues found in code review, with bad/good patterns and impact.

## N+1 Query

```typescript
// ❌ N+1 — one query per post
const posts = await Post.findAll();
for (const post of posts) {
  post.author = await User.findById(post.authorId);   // N queries!
}

// ✅ Single query with join / include
const posts = await Post.findAll({ include: [User] });

// ✅ Or batch load
const posts = await Post.findAll();
const authorIds = posts.map(p => p.authorId);
const authors = await User.findByIds(authorIds);
```

```python
# Django
users = User.objects.prefetch_related('orders').all()  # 2 queries total
# instead of:
for user in User.objects.all():                        # N+1
    user.orders.count()
```

**Impact:** ~100 extra DB queries per request with current approach.

## Missing Error Handling

```typescript
// ❌ Unhandled rejection
const data = await fetch('/api/data').then(r => r.json());

// ✅ Proper handling
try {
  const response = await fetch('/api/data');
  if (!response.ok) throw new Error(`HTTP ${response.status}`);
  const data = await response.json();
} catch (error) {
  logger.error('Failed to fetch data', { error });
  throw new DataFetchError('Could not load data');
}
```

## Magic Numbers / Strings

```typescript
// ❌
if (user.age >= 18) { ... }
setTimeout(fn, 86400000);
if (status === 3) { ... }

// ✅
const MINIMUM_AGE = 18;
const ONE_DAY_MS = 24 * 60 * 60 * 1000;
const ORDER_STATUS_SHIPPED = 3;
if (user.age >= MINIMUM_AGE) { ... }
if (status === ORDER_STATUS_SHIPPED) { ... }
```

## Deep Nesting

```typescript
// ❌
if (user) {
  if (user.isActive) {
    if (user.hasPermission) {
      doSomething();
    }
  }
}

// ✅ Early returns / guard clauses
if (!user || !user.isActive || !user.hasPermission) return;
doSomething();
```

## God Functions

```typescript
// ❌ Does too much
async function processOrder(order) {
  // validate, check inventory, process payment,
  // send email, update database, log analytics
}

// ✅ Single responsibility
async function processOrder(order) {
  await validateOrder(order);
  await reserveInventory(order);
  await chargePayment(order);
  await sendConfirmation(order);
}
```

## Mutable Shared State

```typescript
// ❌ Shared mutable
const config = { debug: false };
function enableDebug() { config.debug = true; }

// ✅ Immutable pattern
function createConfig(overrides = {}) {
  return Object.freeze({ debug: false, ...overrides });
}
```

## Missing Null Checks

```typescript
// ❌ Unsafe access
const name = user.profile.name;

// ✅ Optional chaining + default
const name = user?.profile?.name ?? 'Unknown';

// ✅ Guard clause
if (!user?.profile) return 'Unknown';
return user.profile.name;
```

## Synchronous File Operations (Node)

```typescript
// ❌ Blocks event loop
const data = fs.readFileSync('file.txt');

// ✅ Non-blocking
const data = await fs.promises.readFile('file.txt');
```

## Closure-in-Loop

```typescript
// ❌ All callbacks use i = 5
for (var i = 0; i < 5; i++) {
  setTimeout(() => console.log(i), 100);
}

// ✅ Block-scoped let
for (let i = 0; i < 5; i++) {
  setTimeout(() => console.log(i), 100);
}
```

## React Stale State

```typescript
// ❌ count is stale in the interval closure
const [count, setCount] = useState(0);
useEffect(() => {
  setInterval(() => setCount(count + 1), 1000);   // always 1
}, []);

// ✅ Functional update
useEffect(() => {
  const id = setInterval(() => setCount(c => c + 1), 1000);
  return () => clearInterval(id);
}, []);
```

## Type Coercion

```typescript
// ❌ == does silent coercion
if (value == "") { ... }   // true for 0, "0", null, undefined, false

// ✅ === strict equality
if (value === "") { ... }
```

## Silent Exception Swallow

```python
# ❌ Hides bugs
try:
    process(data)
except Exception:
    pass

# ✅ Catch specific, re-raise or log
try:
    process(data)
except (ValueError, KeyError) as e:
    logger.warning("processing failed", exc_info=True)
    raise ProcessingError(str(e)) from e
```

## Untyped Public APIs

```python
# ❌
def get_user(id):
    return db.query(id)

# ✅
def get_user(user_id: str) -> User | None:
    return db.query(User, user_id)
```

## Hardcoded URLs / Config

```typescript
// ❌
const API_URL = 'https://api.prod.example.com/v1';

// ✅
const API_URL = process.env.API_URL ?? 'http://localhost:3000';
```

## Inconsistent Error Models

```typescript
// ❌ Sometimes returns null, sometimes throws, sometimes { error }
function getUser(id) {
  if (!id) return null;
  if (db.error) return { error: 'db' };
  return user;
}

// ✅ One error model — throw typed exceptions
class NotFoundError extends Error {}
function getUser(id: string): User {
  if (!id) throw new ValueError('id required');
  const user = db.find(id);
  if (!user) throw new NotFoundError(id);
  return user;
}
```

## Quick Reference

| Issue | Impact | Fix |
|---|---|---|
| N+1 queries | Performance | Eager load or batch |
| Missing error handling | Reliability | try/catch + logging |
| Magic numbers | Maintainability | Named constants |
| Deep nesting | Readability | Early returns |
| God functions | Testability | Single responsibility |
| Mutable shared state | Bugs | Immutable patterns |
| Missing null checks | Crashes | Optional chaining |
| Sync file ops (Node) | Performance | Async |
| Closure-in-loop | Wrong values | `let` or IIFE |
| React stale state | Wrong values | Functional update |
| Type coercion | Unexpected behaviour | `===` |
| Silent swallow | Hidden bugs | Specific catch + re-raise |
| Untyped APIs | Runtime errors | Annotate |
| Hardcoded config | No env portability | Env vars |
| Inconsistent error model | Caller confusion | One typed exception hierarchy |

## Detection Patterns (grep recipes)

```bash
# N+1 candidates — query inside loop
rg "for .* in .*:.*\b(find|get|query|fetch)" --type py --type ts

# Magic numbers
rg "\b\d{3,}\b" src/ --type py -g '!*_test.py'

# Sync I/O in Node
rg "readFileSync|writeFileSync" src/ --type ts

# Silent swallow
rg "except.*:\s*$" -A 1 --type py

# Hardcoded URLs
rg "https?://[a-z0-9.-]+\.(com|org|net|io)" src/

# Console.log left in production
rg "console\.(log|debug)" src/ --type ts
```

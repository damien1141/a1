# Bug Taxonomies

A field guide to common bug patterns. Recognise the symptom, jump to the likely cause, apply the fix pattern. Always confirm with a hypothesis test before committing — these are starting points, not certainties.

## Pattern Recognition Table

| Pattern | Symptom | Likely cause | First check |
|---|---|---|---|
| Race condition | Intermittent failures; passes locally, fails in CI | Missing `await`, async timing, shared mutable state | Audit `async`/`await`; check for shared globals |
| Off-by-one | Missing first/last item; index out of bounds | `<` vs `<=`; 0 vs 1 indexing; array bounds | Loop bounds; array index after `.length` |
| Null deref | "undefined is not a function/property" | Missing null check; async value not yet loaded | Optional chaining; loading-state guards |
| Memory leak | Growing memory; slowdown over time | Uncleaned listeners, intervals, subscriptions, caches | `useEffect` cleanup; `clearInterval`; LRU bounds |
| Infinite loop | Hang; "Maximum call stack" | Missing base case; while-true without exit | Recursion base case; loop condition |
| Cache invalidation | Stale data; "I changed it but it didn't update" | Missing cache key; no invalidation on write; TTL too long | Cache key includes all inputs; invalidate on writes |
| N+1 queries | Slow with more data; many DB round-trips | Fetching in a loop | Eager load / batch / join |
| Type coercion | Unexpected behaviour; "0 == ''" is true | `==` instead of `===`; implicit conversion | Strict equality; explicit type checks |
| Closure capture | Loop variable always final value | `var` instead of `let`; React state in stale closure | `let` for loop vars; functional state updates |
| Stale state | Old value used in callback | React state closure capture | Functional update `setState(prev => ...)` |
| Heisenbug | Bug disappears under debugger | Timing-sensitive; debugger changes timing | Add logging instead of breakpoints; run with `--repeat-each=10` |
| Integer overflow | Wraps to negative; "suddenly -2 billion" | No bounds check; wrong int type | Use bigint; check before arithmetic |
| Floating point | "0.1 + 0.2 !== 0.3" | Binary floating point | Use integer cents; `Decimal`; epsilon compare |
| Timezone | Works locally, breaks at midnight UTC | Local time vs UTC; DST | Always UTC internally; convert at the boundary |
| Encoding | "â€™" instead of "'" | UTF-8 decoded as Latin-1 | Set encoding explicitly; serve UTF-8 |

## Race Condition

```typescript
// ❌ Race — data not yet loaded when read
let data;
fetchData().then(result => { data = result; });
console.log(data);                              // undefined — promise hasn't resolved

// ✅ Await the result
const data = await fetchData();
console.log(data);
```

```python
# ❌ Race — two threads mutating shared state
counter = 0
def increment():
    global counter
    for _ in range(1000):
        counter += 1                            # not atomic

# ✅ Lock
from threading import Lock
counter = 0
lock = Lock()
def increment():
    global counter
    for _ in range(1000):
        with lock:
            counter += 1
```

**Detection:**
- Go: `go test -race ./...`
- Rust: cargo test (borrow checker catches at compile time; use `loom` for async)
- Java: `-XX:+UseThreadSanitizer`
- Python: run under `pytest-repeat` with `-n auto` (pytest-xdist)
- JS: ` --detectRace` (experimental); or audit `async`/`await` chains

## Off-by-One

```typescript
// ❌ Skips last element
for (let i = 0; i < array.length - 1; i++) { }

// ✅ Include last element
for (let i = 0; i < array.length; i++) { }

// ❌ Array index out of bounds
const last = array[array.length];              // undefined

// ✅ Correct index
const last = array[array.length - 1];
```

```python
# ❌ Wrong range — misses the endpoint
for i in range(1, len(items)):                 # skips items[0]
    process(items[i])

# ✅ Inclusive of both ends
for i in range(len(items)):                    # all items
    process(items[i])

# ❌ Slicing off-by-one
items[1:]                                       # skips first
items[:-1]                                      # skips last

# ✅ Be explicit
items[0:]                                       # all
items[:]                                        # all (copy)
```

## Null / Undefined Dereference

```typescript
// ❌ Crashes if user is null
const name = user.profile.name;

// ✅ Optional chaining + default
const name = user?.profile?.name ?? 'Unknown';

// ✅ Guard clause
if (!user?.profile) return 'Unknown';
return user.profile.name;
```

```python
# ❌ Crashes if user is None
name = user.profile.name

# ✅ Explicit None check
name = user.profile.name if user and user.profile else "Unknown"

# ✅ getattr with default
name = getattr(getattr(user, "profile", None), "name", "Unknown")
```

**Loading-state guard (React):**
```typescript
// ❌ Crashes while query loading — data is undefined
function UserList({ data }) {
  return data.users.map(u => <li>{u.name}</li>);
}

// ✅ Guard loading and empty states
function UserList({ data, loading, error }) {
  if (loading) return <Spinner />;
  if (error) return <ErrorState error={error} />;
  if (!data?.users?.length) return <EmptyState />;
  return data.users.map(u => <li key={u.id}>{u.name}</li>);
}
```

## Memory Leak

```typescript
// ❌ Listener never removed
useEffect(() => {
  window.addEventListener('resize', handleResize);
}, []);                                         // missing cleanup

// ✅ Cleanup function
useEffect(() => {
  window.addEventListener('resize', handleResize);
  return () => window.removeEventListener('resize', handleResize);
}, []);

// ❌ Interval never cleared
setInterval(pollData, 1000);

// ✅ Store and clear
const id = setInterval(pollData, 1000);
return () => clearInterval(id);
```

```python
# ❌ Cache grows unbounded
cache = {}
def get(key):
    if key not in cache:
        cache[key] = fetch(key)
    return cache[key]

# ✅ Bounded LRU
from functools import lru_cache

@lru_cache(maxsize=1024)
def get(key):
    return fetch(key)
```

**Detection:**
- Node: `process.memoryUsage()` snapshots; `--inspect` → Memory tab → Heap snapshot diff
- Python: `tracemalloc`; `memray`
- Go: `pprof heap`
- Rust: `valgrind`; `dhat`
- Browser: Chrome DevTools → Memory → Heap snapshot

## Infinite Loop

```typescript
// ❌ No base case
function factorial(n) {
  return n * factorial(n - 1);                  // runs forever for n < 1
}

// ✅ Base case
function factorial(n) {
  if (n <= 1) return 1;
  return n * factorial(n - 1);
}
```

```python
# ❌ While-true with no exit
while True:
    process()

# ✅ Explicit exit condition
while queue:
    process(queue.pop(0))
```

**Detection:** "Maximum call stack size exceeded" (recursion); timeout / hang (loop). Add iteration counters as a safety net in production loops.

## Cache Invalidation

"There are only two hard things in computer science: cache invalidation and naming things."

```typescript
// ❌ Cache key doesn't include user → cross-user leak
const cache = new Map<string, User>();
function getUser(id: string): User {
  if (cache.has(id)) return cache.get(id);
  const user = fetchUser(id);
  cache.set(id, user);
  return user;
}
// User A logs in, caches user 1. User B logs in, gets user 1 from cache. 💥

// ✅ Cache key includes the scope
function getUser(userId: string, requesterId: string): User {
  const key = `${requesterId}:${userId}`;
  // ...
}

// ❌ No invalidation on write
function updateUser(id, patch) {
  return db.update(id, patch);                  // cache still holds old value
}

// ✅ Invalidate on write
function updateUser(id, patch) {
  const updated = db.update(id, patch);
  cache.delete(id);
  return updated;
}
```

Cache invalidation strategies: TTL (simple, eventually consistent), write-through (always consistent, slower), write-behind (fast, risk of loss), event-driven (invalidate on change events).

## Closure Capture / Stale State

```typescript
// ❌ All callbacks print 5 — `var` is function-scoped
for (var i = 0; i < 5; i++) {
  setTimeout(() => console.log(i), 100);
}

// ✅ `let` is block-scoped
for (let i = 0; i < 5; i++) {
  setTimeout(() => console.log(i), 100);
}

// ✅ Or capture via IIFE
for (var i = 0; i < 5; i++) {
  ((j) => setTimeout(() => console.log(j), 100))(i);
}
```

```typescript
// ❌ count is stale in the interval closure
const [count, setCount] = useState(0);
useEffect(() => {
  setInterval(() => setCount(count + 1), 1000);   // always 1
}, []);

// ✅ Functional update — reads the latest value
useEffect(() => {
  const id = setInterval(() => setCount(c => c + 1), 1000);
  return () => clearInterval(id);
}, []);
```

## Type Coercion

```typescript
// ❌ == does silent coercion
0 == ""           // true
0 == "0"          // true
"" == "0"         // false
null == undefined // true

// ✅ === is strict
0 === ""          // false
0 === "0"         // false
null === undefined // false
```

## N+1 Queries

```typescript
// ❌ N queries per post
const posts = await Post.findAll();
for (const post of posts) {
  post.author = await User.findById(post.authorId);
}

// ✅ Single query with join
const posts = await Post.findAll({ include: [User] });

// ✅ Or batch
const authorIds = posts.map(p => p.authorId);
const authors = await User.findByIds(authorIds);
```

## Heisenbug (Disappears Under Debugger)

The debugger changes timing, which masks the race. Don't reach for breakpoints; reach for logs.

```typescript
// ❌ Bug disappears when you set a breakpoint
await page.click('#submit');
debugger;                                       // breakpoint gives the network time to finish
await expect(page.getByText('Saved')).toBeVisible();   // passes

// ✅ Add logging; don't pause
await page.click('#submit');
console.log('[test] clicked at', Date.now());
const response = await page.waitForResponse('**/api/save');
console.log('[test] response at', Date.now(), response.status());
await expect(page.getByText('Saved')).toBeVisible();
```

Run the test 10× to confirm stability: `npx playwright test --repeat-each=10`.

## Floating Point

```typescript
0.1 + 0.2 === 0.3           // false — it's 0.30000000000000004

// ✅ Use integer cents
const totalCents = 199 + 299;                  // 498 cents = $4.98

// ✅ Or epsilon compare
function approxEqual(a, b, eps = 1e-9) {
  return Math.abs(a - b) < eps;
}

// ✅ Or use a decimal library
import { Decimal } from 'decimal.js';
new Decimal('0.1').plus('0.2').equals('0.3');  // true
```

## Timezone

```python
# ❌ Local time — breaks at midnight UTC, on DST change
from datetime import datetime
now = datetime.now()                           # local

# ✅ UTC internally; convert at the boundary
from datetime import datetime, timezone
now = datetime.now(timezone.utc)               # always UTC
# Display: convert to user's tz
from zoneinfo import ZoneInfo
display = now.astimezone(ZoneInfo("America/New_York"))
```

Never store local time. Never compare naive datetimes across machines.

## Quick Reference

| Symptom | First check |
|---|---|
| "undefined is not..." | Null check missing; loading state not guarded |
| Intermittent failure | Race condition; test order dependence |
| Wrong value in callback | Closure capture / stale state |
| Gets slower over time | Memory leak; N+1 queries |
| Off by one item | Loop bounds; array index after `.length` |
| Type mismatch | `==` vs `===`; coercion |
| Hang / freeze | Infinite loop; deadlock |
| "Stale" data | Cache invalidation; React state closure |
| Works locally, fails CI | Timezone; encoding; race condition; env var |
| Disappears under debugger | Heisenbug — use logs, not breakpoints |
| Floating point "wrong" | Binary FP; use integer cents or Decimal |

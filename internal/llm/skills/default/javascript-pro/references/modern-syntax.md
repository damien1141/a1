# Modern JavaScript Syntax (ES2024+)

Vanilla JS baseline: Node 20+ and evergreen browsers. The features below ship natively — no transpiler required. Use them.

## ES2024 highlights

### `Object.groupBy` / `Map.groupBy`
```js
const orders = [
  { id: 1, status: 'shipped' },
  { id: 2, status: 'pending' },
  { id: 3, status: 'shipped' },
];

// Object.groupBy → null-prototype object keyed by string
const byStatus = Object.groupBy(orders, (o) => o.status);
// byStatus.shipped === [order1, order3]; byStatus.pending === [order2]
// Note: result has NO Object.prototype — `byStatus.toString` is undefined

// Map.groupBy → Map keyed by anything (objects, numbers, etc.)
const byCount = Map.groupBy(orders, (o) => o.items.length);
for (const [count, group] of byCount) console.log(count, group.length);
```
Prefer `Map.groupBy` when keys are non-strings or you need iteration order guaranteed to insertion order. `Object.groupBy` is fine when keys are strings and you want dot access.

### `Promise.withResolvers`
Extract `resolve`/`reject` without the IIFE pattern:
```js
// Old:
const p = new Promise((resolve, reject) => { /* ... */ });

// ES2024:
const { promise, resolve, reject } = Promise.withResolvers();
stream.on('data', (chunk) => chunks.push(chunk));
stream.on('end', () => resolve(chunks));
stream.on('error', reject);
// External code can resolve/reject out-of-band, which the constructor form makes awkward.
return promise;
```
Useful for bridging event-emitter APIs to promises, lazy cancellation, and tests.

### `using` / `await using` (Explicit Resource Management)
```js
class TempFile {
  #path;
  constructor(name) { this.#path = `/tmp/${name}`; }
  static async open(name) {
    const t = new TempFile(name);
    await fs.writeFile(t.#path, '');
    return t;
  }
  async [Symbol.asyncDispose]() { await fs.rm(this.#path, { force: true }); }
}

{
  await using f = await TempFile.open('work');
  await f.write('hello');
} // f[Symbol.asyncDispose]() runs now — even if write() threw

// Sync variant: Symbol.dispose + `using` (no `await`)
class Mutex {
  [Symbol.dispose]() { this.release(); }
}
using lock = mutex.acquire();
```
Pair with `DisposableStack`/`AsyncDisposableStack` to aggregate teardown in order.

### `Array.fromAsync`
Convert an async iterable (async generator, stream, paginated fetch) to an array:
```js
async function* gen() { for (let i = 0; i < 3; i++) yield i; }
const arr = await Array.fromAsync(gen()); // [0, 1, 2]

// Also applies a map-like await fn:
const urls = ['/a', '/b'];
const bodies = await Array.fromAsync(urls, async (u) => fetch(u).then(r => r.text()));
```

### Well-formed Unicode strings (`String.prototype.isWellFormed` / `toWellFormed`)
```js
const lone = 'a\uD800b';      // lone surrogate
lone.isWellFormed();           // false
lone.toWellFormed();           // 'a\uFFFDb' (replacement char)
```
Use before `encodeURIComponent`, `fetch` bodies, or any cross-boundary encode that throws on lone surrogates.

### `String.prototype` extras
- `String.raw` (template) — already common.
- ES2023 `String.prototype.normalize` is the canonical NFC fix for comparisons.

## ES2023 — non-mutating Array methods

```js
const xs = [3, 1, 2];

xs.toSorted((a, b) => a - b);   // [1, 2, 3] — xs unchanged
xs.toReversed();                 // [2, 1, 3] — xs unchanged
xs.toSpliced(1, 1, 9);           // [3, 9, 2] — xs unchanged
xs.with(0, 99);                  // [99, 1, 2] — xs unchanged

// Last-element search
[1, 2, 3, 4].findLast((n) => n % 2 === 0);        // 4
[1, 2, 3, 4].findLastIndex((n) => n % 2 === 0);   // 3

// Negative indexing
xs.at(-1);                       // 2  — replaces xs[xs.length - 1]
```
Rule of thumb: in pure transforms use the `to*` family; reserve in-place `sort`/`reverse`/`splice` for when you intentionally mutate a local buffer.

## ES2022

### Private fields and methods
```js
class TokenBucket {
  #tokens = 0;
  #capacity;
  #refillRate;
  constructor(capacity, refillRatePerSec) {
    this.#capacity = capacity;
    this.#refillRate = refillRatePerSec;
  }
  #refill(now) { /* ... */ }
  take(n = 1) {
    this.#refill(Date.now());
    if (this.#tokens < n) return false;
    this.#tokens -= n;
    return true;
  }
  get available() { return this.#tokens; } // public getter over private field
}
```
Private fields are NOT accessible via `this['#x']` and are not inherited like normal props. Use `.#field in obj` to feature-detect.

### `Object.hasOwn` (replaces `obj.hasOwnProperty`)
```js
Object.hasOwn(obj, 'key');       // safe even if obj has null prototype
obj.hasOwnProperty('key');        // unsafe — can be shadowed or absent
Object.prototype.hasOwnProperty.call(obj, 'key'); // verbose
```

### `Error.cause`
```js
try {
  await db.query(sql);
} catch (err) {
  throw new Error(`query failed: ${sql}`, { cause: err });
}
// Logger can walk err.cause.cause... for the full chain.
```

### Top-level await (ESM only)
```js
// config.js (ESM)
export const config = await fetch('/config.json').then(r => r.json());
// Consumers block on this module's evaluation; that's the contract.
```
Use only for app startup. In libraries, expose an async `init()` so consumers control timing.

### `at()` on Array, String, TypedArray
```js
'hello'.at(-1);                 // 'o'
[10, 20, 30].at(-2);            // 20
```

## ES2021 — logical assignment & combinators

```js
config.timeout ??= 5_000;       // set only if nullish (NOT if 0)
user.profile &&= sanitize(user.profile);  // set only if truthy
cache.hits ||= 0;               // set only if falsy (treat 0 as miss — careful)

// Promise.any → first fulfilled; throws AggregateError if all reject
const fastest = await Promise.any([
  fetch('https://mirror-a/data'),
  fetch('https://mirror-b/data'),
]);

// replaceAll
text.replaceAll('\t', '  ');
text.replaceAll(/[aeiou]/g, '*');

// WeakRef + FinalizationRegistry (see references/performance-and-memory.md)
```

## ES2020 — daily drivers

```js
// Optional chaining
const city = user?.address?.city;
const first = items?.[0];
const result = api?.fetch?.();
delete user?.temp?.cache;

// Nullish coalescing (use ?? not || for 0/''/false)
const port = config.port ?? 3000;
const name = user.name ?? 'Anonymous';
```

## Numeric separators and BigInt

```js
const billion = 1_000_000_000;
const mask    = 0xFF_EC_DE_5E;
const huge    = 9_007_199_254_740_991n;

2n ** 64n;          // 18446744073709551616n
BigInt(123) + 456n; // 579n  — mixed Number/BigInt throws; explicit cast required
10n / 3n;           // 3n    — BigInt division truncates
```

## Iterator helpers (Stage 3, shipping in Node 22+ & Chrome 122+)

```js
// Iterator.prototype.map / filter / take / drop / toArray
function* naturals() { let i = 1; while (true) yield i++; }

const result = naturals()
  .map((x) => x * 2)
  .filter((x) => x > 4)
  .take(3)
  .toArray();           // [6, 8, 10]

// Lazy — does NOT buffer the infinite generator
```
Verify target runtime supports it; otherwise fall back to `Array.fromAsync(gen(), fn)`.

## Quick reference

| Feature | ES | Syntax |
|---|---|---|
| `Object.groupBy` / `Map.groupBy` | 2024 | `Object.groupBy(arr, fn)` |
| `Promise.withResolvers` | 2024 | `const { promise, resolve, reject } = Promise.withResolvers()` |
| `using` / `await using` | 2024 | `await using x = open();` |
| `Array.fromAsync` | 2024 | `await Array.fromAsync(asyncIterable)` |
| `String.isWellFormed` / `toWellFormed` | 2024 | `s.isWellFormed()` |
| `findLast` / `findLastIndex` | 2023 | `arr.findLast(fn)` |
| `toSorted` / `toReversed` / `toSpliced` / `with` | 2023 | `arr.toSorted(cmp)` |
| `#private` fields | 2022 | `#x = 0` |
| `Object.hasOwn` | 2022 | `Object.hasOwn(o, 'k')` |
| `Error.cause` | 2022 | `new Error('m', { cause: err })` |
| Top-level await | 2022 | `await x` at module top |
| `at()` | 2022 | `arr.at(-1)` |
| `||=` / `&&=` / `??=` | 2021 | `x ??= y` |
| `Promise.any` + `AggregateError` | 2021 | `await Promise.any([...])` |
| `replaceAll` | 2021 | `s.replaceAll('a', 'b')` |
| `?.` / `??` | 2020 | `o?.p ?? d` |
| `structuredClone` | 2022 (platform) | `structuredClone(obj)` |

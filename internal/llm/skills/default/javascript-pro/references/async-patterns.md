# Async Patterns

Vanilla JS async baseline: `async`/`await` everywhere, explicit `try/catch`, every fetch/worker/long task takes an `AbortSignal`. Never leave a rejection unhandled.

## The four combinators — pick by intent

| Combinator | Settles when | Rejects when | Use |
|---|---|---|---|
| `Promise.all` | all fulfilled | first rejection | Parallel, fail-fast (you can't proceed without everything) |
| `Promise.allSettled` | all settled | never | Parallel, gather every outcome (best-effort fan-out) |
| `Promise.any` | first fulfilled | all reject → `AggregateError` | Race for the fastest success (mirror racing) |
| `Promise.race` | first settles | first rejects | Hard timeout, first-wins (success or failure) |

```js
// Fail-fast: need every result or none
const [user, posts] = await Promise.all([
  fetch('/api/user').then(r => r.json()),
  fetch('/api/posts').then(r => r.json()),
]);

// Best-effort: log failures, keep successes
const results = await Promise.allSettled(urls.map((u) => fetch(u).then(r => r.json())));
const ok = results.filter((r) => r.status === 'fulfilled').map((r) => r.value);
const failures = results.filter((r) => r.status === 'rejected').map((r) => r.reason);

// Race for fastest success
try {
  const fastest = await Promise.any(mirrors.map((u) => fetch(u)));
} catch (err) {
  if (!(err instanceof AggregateError)) throw err;
  console.error('all mirrors failed', err.errors);
}

// Hard timeout
const timeout = new Promise((_, reject) =>
  setTimeout(() => reject(new Error('timeout')), 5_000),
);
const res = await Promise.race([fetch(url), timeout]);
```

## `Promise.withResolvers` (ES2024) — the bridge pattern

When you need to hand `resolve`/`reject` to external code (event emitters, streams, callbacks), stop writing the IIFE:
```js
function readChunk(stream) {
  const { promise, resolve, reject } = Promise.withResolvers();
  stream.once('data', resolve);
  stream.once('error', reject);
  stream.once('end', () => resolve(null));
  return promise.finally(() => {
    stream.off('data', resolve);
    stream.off('error', reject);
    stream.off('end', resolve);
  });
}
```

## `AbortController` / `AbortSignal` — cancellation is mandatory

Every fetch, worker, long task, and event listener should accept an optional `signal`. Never invent custom cancel tokens.

```js
async function loadAll(urls, signal) {
  // One abort propagates to every fetch
  const responses = await Promise.all(
    urls.map((u) => fetch(u, { signal })),
  );
  return Promise.all(responses.map((r) => r.json()));
}

// Caller:
const controller = new AbortController();
buttonCancel.onclick = () => controller.abort();
const data = await loadAll(urls, controller.signal);
```

### Composing signals
```js
// Combine external + timeout into one signal
function withTimeout(signal, ms) {
  const controller = new AbortController();
  const t = setTimeout(() => controller.abort(new DOMException('timeout', 'TimeoutError')), ms);
  signal?.addEventListener('abort', () => controller.abort(signal.reason), { once: true });
  controller.signal.addEventListener('abort', () => clearTimeout(t), { once: true });
  return controller.signal;
}

// AbortSignal.any (Node 20+, Chrome 116+) — merge multiple signals
const merged = AbortSignal.any([userSignal, timeoutSignal]);
```

### Listener teardown via signal
```js
function observe(target, type, fn, signal) {
  target.addEventListener(type, fn, { signal });
  // signal.abort() removes the listener — no bookkeeping
}
const ac = new AbortController();
observe(document, 'click', handler, ac.signal);
ac.abort(); // listener gone
```

### Abort reason
```js
controller.abort(new Error('user navigated away'));
try { await fetch(url, { signal }); }
catch (err) {
  if (err.name === 'AbortError') {
    console.log('reason:', err.message); // 'user navigated away'
  }
}
```

## Async iterators and `for await...of`

```js
// Async generator: lazy pagination
async function* paginate(baseUrl, signal) {
  let url = baseUrl;
  while (url) {
    const { items, next } = await fetch(url, { signal }).then(r => r.json());
    yield* items;
    url = next ?? null;
  }
}

const ac = new AbortController();
for await (const item of paginate('/api/items', ac.signal)) {
  render(item);
  if (enough) { ac.abort(); break; }
}
```

### Async generator with retry/backoff
```js
async function* streamWithRetry(source, retries = 3) {
  for (let attempt = 0; attempt < retries; attempt++) {
    try {
      for await (const chunk of source()) yield chunk;
      return;
    } catch (err) {
      if (attempt === retries - 1) throw err;
      await new Promise((r) => setTimeout(r, 2 ** attempt * 500));
    }
  }
}
```

## Top-level await (ESM only)

```js
// app.js (ESM)
const config = await fetch('/config.json').then(r => r.json());
start(config);
```
Rules:
- Only in ESM (`"type": "module"` or `.mjs`)
- Blocks every importer until settled — use only for app startup, not libraries (expose `async init()` instead)
- Cannot be used inside `try/catch` at the top level — wrap in an async IIFE if you need to catch

## Error handling — the rules

1. Every `await` that can reject must be inside a `try`/`catch` OR the enclosing function's caller must handle it.
2. Re-throw after logging unless the caller signed up for a sentinel (`null`/`undefined`):
```js
async function getUser(id) {
  try {
    const r = await fetch(`/api/users/${id}`);
    if (!r.ok) throw new ApiError(r.status, await r.text());
    return await r.json();
  } catch (err) {
    log.error('getUser failed', { id, err });
    throw err; // caller decides
  }
}
```
3. Use typed error classes + `Error.cause`:
```js
class ApiError extends Error {
  constructor(status, message, { cause } = {}) {
    super(message, { cause });
    this.name = 'ApiError';
    this.status = status;
  }
}
```
4. `AggregateError` from `Promise.any` carries `.errors` (array of all rejection reasons). Walk it; do not stringify it.
5. Never `catch (err) {}` empty. If you genuinely want to swallow, comment why: `catch { /* expected: ECONNRESET during shutdown */ }`.

## Unhandled-rejection hygiene

```js
// Last-resort safety net — should never fire in correct code
process.on('unhandledRejection', (reason, promise) => {
  log.error('unhandledRejection', reason);
  // Choose: exit (strict) or continue (resilient). Document the choice.
  process.exit(1);
});

// Browser equivalent:
window.addEventListener('unhandledrejection', (event) => {
  log.error('unhandledrejection', event.reason);
  event.preventDefault(); // suppress console noise — only if you log it
});
```
Goal: zero `unhandledRejection` events in normal operation. If one fires, fix the missing `await`/`.catch()`.

## Concurrent fan-out with a queue (avoid hammering backends)

```js
class Pool {
  #concurrency;
  #active = 0;
  #queue = [];
  constructor(concurrency) { this.#concurrency = concurrency; }
  run(fn) {
    return new Promise((resolve, reject) => {
      const task = () => {
        this.#active++;
        fn()
          .then(resolve, reject)
          .finally(() => {
            this.#active--;
            this.#next();
          });
      };
      if (this.#active < this.#concurrency) task();
      else this.#queue.push(task);
    });
  }
  #next() { const t = this.#queue.shift(); if (t) t(); }
}

const pool = new Pool(5);
const pages = await Promise.all(
  urls.map((u) => pool.run(() => fetch(u).then(r => r.json()))),
);
```

## Event loop: microtasks vs macrotasks

```js
console.log('1 sync');
setTimeout(() => console.log('5 macrotask'));
Promise.resolve().then(() => console.log('3 microtask'));
queueMicrotask(() => console.log('4 microtask'));
console.log('2 sync');
// Output: 1, 2, 3, 4, 5
```
Rules:
- Microtasks (Promise callbacks, `queueMicrotask`) drain before the next macrotask — they can starve I/O.
- Never do CPU-heavy work synchronously; chunk with `await new Promise(r => setTimeout(r, 0))` or move to a Worker.
- `requestAnimationFrame` is the browser's frame-aligned macrotask — use for visual work.

## Streaming fetch

```js
async function* lineStream(url, signal) {
  const res = await fetch(url, { signal });
  if (!res.ok) throw new Error(`HTTP ${res.status}`);
  const reader = res.body.pipeThrough(new TextDecoderStream()).getReader();
  let buf = '';
  while (true) {
    const { done, value } = await reader.read();
    if (done) break;
    buf += value;
    let idx;
    while ((idx = buf.indexOf('\n')) >= 0) {
      yield buf.slice(0, idx);
      buf = buf.slice(idx + 1);
    }
  }
  if (buf) yield buf;
}

for await (const line of lineStream('/api/log')) {
  if (line.startsWith('ERROR')) alert(line);
}
```

## Quick reference

| Pattern | When | Example |
|---|---|---|
| `Promise.all` | Parallel, fail-fast | `await Promise.all([p1, p2])` |
| `Promise.allSettled` | Parallel, gather all outcomes | `await Promise.allSettled([p1, p2])` |
| `Promise.any` | First to succeed | `await Promise.any([p1, p2])` |
| `Promise.race` | First to settle (timeout) | `await Promise.race([p, timeout])` |
| `Promise.withResolvers` | Hand resolve/reject outward | `const { promise, resolve } = Promise.withResolvers()` |
| `AbortController` | Cancellation | `fetch(url, { signal })` |
| `AbortSignal.any` | Merge signals | `AbortSignal.any([a, b])` |
| `async function*` | Lazy async sequence | `for await (const x of gen())` |
| `for await...of` | Consume async iterable | `for await (const chunk of stream)` |
| Top-level await | App startup, ESM only | `const cfg = await loadCfg()` |
| `AggregateError` | `Promise.any` all-rejected | `err.errors` |

# Performance & Memory

Two enemies: jank (main-thread blocking) and leaks (memory that grows until the tab/process dies). Vanilla JS gives you the tools to avoid both — use them deliberately.

## Layout thrashing — the #1 main-thread sin

Every read of computed geometry (`offsetWidth`, `getBoundingClientRect`, `scrollTop`, `getComputedStyle`) can force the browser to flush pending style changes (reflow). Interleaving reads and writes is the killer:

```js
// BAD — N reflows
items.forEach((el) => {
  const w = el.offsetWidth;            // read → forces layout
  el.style.width = `${w + 10}px`;      // write → invalidates layout
});

// GOOD — batch reads, then batch writes
const widths = items.map((el) => el.offsetWidth);  // all reads first
items.forEach((el, i) => {
  el.style.width = `${widths[i] + 10}px`;          // all writes
});
```
Better: prefer CSS (`transform`, `opacity`, `flex-grow`) over JS-set geometry.

## Animate only `transform` and `opacity`

```css
.card { transition: transform 200ms cubic-bezier(0.16, 1, 0.3, 1); }
.card.lifting { transform: translateY(-4px); }
```
`transform` and `opacity` are compositor-only — they skip layout and paint. Animating `top`/`left`/`width`/`height`/`box-shadow` triggers layout or paint and will jank on low-end devices.

## `requestAnimationFrame` — align with the frame budget

```js
let last = 0;
function loop(t) {
  if (t - last < 16) { requestAnimationFrame(loop); return; }
  update(t - last);
  last = t;
  requestAnimationFrame(loop);
}
requestAnimationFrame(loop);
```
- 16ms budget for 60fps. Anything longer drops a frame.
- Batch visual updates inside one rAF; never `setTimeout(visualWork)`.
- Cancel: `cancelAnimationFrame(handle)`.

## `requestIdleCallback` — non-urgent background work

```js
function workLarge(deadline) {
  while (deadline.timeRemaining() > 5 && queue.length) {
    processItem(queue.shift());
  }
  if (queue.length) requestIdleCallback(workLarge);
}
requestIdleCallback(workLarge, { timeout: 2_000 });

// Cancel: cancelIdleCallback(handle)
```
For hydrating analytics, prefetching, deferred parsing. Safari lacks native support — feature-detect and fall back to `setTimeout(0)`.

## Off-main-thread — Workers for CPU-heavy work

```js
const worker = new Worker(new URL('./heavy.js', import.meta.url), { type: 'module' });
const hash = await dispatch(worker, 'sha256', largeBytes);
```
Move crypto, JSON parsing of large payloads, image manipulation, big sorts — anything >5ms of CPU. See `references/browser-apis.md` and `references/node-essentials.md` for the full Worker pattern.

## Memory leaks — the four sources

### 1. Detached DOM nodes held by closures
```js
// BAD — `button` is referenced forever, even after removed from DOM
function setup() {
  const button = document.createElement('button');
  bigContainer.append(button);
  button.addEventListener('click', () => console.log(button.value));
  // Later: button.remove() — but the listener closure still references it.
}
```
Fix: use `AbortSignal` so removal also kills the listener:
```js
const ac = new AbortController();
button.addEventListener('click', () => console.log(button.value), { signal: ac.signal });
button.remove();
ac.abort(); // closure is unreachable; button + its descendants can be GC'd
```

### 2. Listeners without removal
```js
// BAD
window.addEventListener('resize', handler); // never removed

// GOOD
const ac = new AbortController();
window.addEventListener('resize', handler, { signal: ac.signal });
// On unmount:
ac.abort();
```

### 3. Timers
```js
const id = setInterval(tick, 1_000);
// On unmount:
clearInterval(id);
```
Pair every `setInterval`/`setTimeout` with a `clear*` in the same teardown path. Use `AbortSignal` + `signal.addEventListener('abort', () => clearInterval(id))` to centralize.

### 4. Observers / Workers / Subscriptions
```js
const io = new IntersectionObserver(cb);
io.observe(el);
// On unmount:
io.disconnect();

const ws = new WebSocket(url);
// On unmount:
ws.close();

const sub = eventBus.on('x', fn);
// On unmount:
sub.unsubscribe(); // or eventBus.off('x', fn)
```

## `WeakRef` and `FinalizationRegistry`

Use sparingly — GC is not deterministic, so `FinalizationRegistry` callbacks may never fire, or fire much later than you expect. Good for caches where a miss is recoverable.

```js
class WeakCache {
  #map = new Map();
  set(key, value) { this.#map.set(key, new WeakRef(value)); }
  get(key) { return this.#map.get(key)?.deref(); }
}

const registry = new FinalizationRegistry((held) => {
  console.log(`cleanup ${held}`);
  // Release external resources — close file handles, free native buffers, etc.
});

class Resource {
  constructor(id) {
    this.id = id;
    registry.register(this, id, this); // 3rd arg = unregister token
  }
  dispose() {
    registry.unregister(this); // explicit cleanup — no callback fires
  }
}
```
Never rely on `FinalizationRegistry` for correctness — only as a best-effort backstop.

## `structuredClone` — the right way to deep-copy

```js
const copy = structuredClone(original);
```
- Handles `Date`, `RegExp`, `Map`, `Set`, `ArrayBuffer`, `TypedArray`, cyclic refs, `Blob`, `File`.
- Does NOT copy functions or DOM nodes (throws).
- Replaces `JSON.parse(JSON.stringify(x))` (which loses `Date`/`Map`/`Set`/`undefined` and throws on cycles).
- Use `postMessage(value)` to a same-origin Worker for the same effect — `structuredClone` is the direct call.

## Avoiding deep reactivity in hot loops

For transforms over large arrays, use plain objects and `Array` methods — do not wrap each element in a Proxy (frameworks do this for reactivity; vanilla JS doesn't need it):
```js
// Fast
const totals = items.map((i) => i.qty * i.price).reduce((a, b) => a + b, 0);

// Slow if `items` is wrapped per-element in a Proxy
let total = 0;
for (const i of items) total += i.qty * i.price; // every read traps
```
If you need reactivity for a small set, scope it to the smallest possible surface.

## Chunking CPU work

```js
async function processLarge(items) {
  const out = [];
  const CHUNK = 200;
  for (let i = 0; i < items.length; i += CHUNK) {
    for (let j = i; j < Math.min(i + CHUNK, items.length); j++) {
      out.push(transform(items[j]));
    }
    // Yield to the event loop so the UI can paint
    await new Promise((r) => setTimeout(r, 0));
  }
  return out;
}
```
For really heavy work, move it to a Worker instead of chunking on the main thread.

## Measuring

```js
// Marks and measures
performance.mark('start');
await doWork();
performance.mark('end');
performance.measure('work', 'start', 'end');
const [m] = performance.getEntriesByName('work');
console.log(m.duration);

// PerformanceObserver for Web Vitals
new PerformanceObserver((list) => {
  for (const e of list.getEntries()) {
    if (e.entryType === 'largest-contentful-paint') console.log('LCP', e.startTime);
    if (e.entryType === 'layout-shift' && !e.hadRecentInput) console.log('CLS', e.value);
  }
}).observe({ entryTypes: ['largest-contentful-paint', 'layout-shift'] });
```

## Quick reference

| Need | Tool |
|---|---|
| Avoid reflow | batch reads before writes; use CSS transforms |
| 60fps animation | `requestAnimationFrame`; animate `transform`/`opacity` |
| Background work | `requestIdleCallback` (fallback `setTimeout(0)`) |
| CPU-heavy | Web Worker / `worker_threads` |
| Listener teardown | `AbortSignal` |
| Timer cleanup | `clearInterval`/`clearTimeout` paired with teardown |
| Deep copy | `structuredClone(x)` |
| Weak cache | `WeakRef` + `FinalizationRegistry` (best-effort) |
| Measure | `performance.mark`/`measure`, `PerformanceObserver` |

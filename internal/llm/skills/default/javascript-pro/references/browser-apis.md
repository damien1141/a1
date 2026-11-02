# Browser APIs

Baseline: evergreen browsers (Chrome/Edge 120+, Firefox 120+, Safari 17+). Use these natively — no polyfills for the patterns below.

## Fetch

### Basic + JSON
```js
async function getJSON(url, { signal } = {}) {
  const res = await fetch(url, { signal });
  if (!res.ok) throw new Error(`HTTP ${res.status}`);
  return res.json();
}
```

### POST/PUT/PATCH with JSON
```js
async function postJSON(url, body, { signal } = {}) {
  const res = await fetch(url, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(body),
    signal,
  });
  if (!res.ok) throw new Error(`HTTP ${res.status}`);
  return res.json();
}
```

### Streaming a response body
```js
async function* chunks(url, signal) {
  const res = await fetch(url, { signal });
  if (!res.ok || !res.body) throw new Error(`HTTP ${res.status}`);
  const reader = res.body.pipeThrough(new TextDecoderStream()).getReader();
  while (true) {
    const { done, value } = await reader.read();
    if (done) return;
    yield value;
  }
}

for await (const chunk of chunks('/api/log')) {
  console.log(chunk);
}
```

### Uploading with progress
`fetch` cannot observe upload progress. Use `XMLHttpRequest` for that single use case:
```js
function uploadWithProgress(file, url, onProgress) {
  return new Promise((resolve, reject) => {
    const xhr = new XMLHttpRequest();
    xhr.upload.addEventListener('progress', (e) => {
      if (e.lengthComputable) onProgress(e.loaded / e.total);
    });
    xhr.addEventListener('load', () =>
      xhr.status < 400 ? resolve(xhr.response) : reject(new Error(`HTTP ${xhr.status}`)),
    );
    xhr.addEventListener('error', () => reject(new Error('network error')));
    const form = new FormData();
    form.append('file', file);
    xhr.open('POST', url);
    xhr.send(form);
  });
}
```

### Abort
```js
const ac = new AbortController();
setTimeout(() => ac.abort(new Error('timeout')), 5_000);
try { await fetch('/api', { signal: ac.signal }); }
catch (err) { if (err.name !== 'AbortError') throw err; }
```

## Web Workers

```js
// main.js
const worker = new Worker(new URL('./heavy.js', import.meta.url), { type: 'module' });

const pending = new Map();
let nextId = 0;

worker.addEventListener('message', (e) => {
  const { id, result, error } = e.data;
  const { resolve, reject } = pending.get(id);
  pending.delete(id);
  error ? reject(Object.assign(new Error(error.message), error)) : resolve(result);
});

function dispatch(type, payload) {
  const id = nextId++;
  return new Promise((resolve, reject) => {
    pending.set(id, { resolve, reject });
    worker.postMessage({ id, type, payload });
  });
}

const hash = await dispatch('sha256', bytes);
```

```js
// heavy.js
self.addEventListener('message', async (e) => {
  const { id, type, payload } = e.data;
  try {
    const result = await handle(type, payload);
    self.postMessage({ id, result });
  } catch (err) {
    self.postMessage({ id, error: { message: err.message, name: err.name } });
  }
});

async function handle(type, payload) {
  if (type === 'sha256') return new Uint8Array(
    await crypto.subtle.digest('SHA-256', payload),
  );
  throw new Error(`unknown ${type}`);
}
```
Rules:
- Construct with `new URL('./x.js', import.meta.url)` so the bundler sees the dependency.
- Use `{ type: 'module' }` to ship ESM workers.
- Move CPU-heavy work (crypto, image processing, large sorts/hashes) to a worker — never block the main thread >16ms.

## Service Workers

```js
// Register (page)
if ('serviceWorker' in navigator) {
  const reg = await navigator.serviceWorker.register('/sw.js', { type: 'module' });
  reg.addEventListener('updatefound', () => {
    const nw = reg.installing;
    nw.addEventListener('statechange', () => {
      if (nw.state === 'activated') window.location.reload();
    });
  });
}
```

```js
// sw.js
const CACHE = 'app-v1';
const PRECACHE = ['/', '/app.js', '/styles.css'];

self.addEventListener('install', (e) =>
  e.waitUntil(caches.open(CACHE).then((c) => c.addAll(PRECACHE))));

self.addEventListener('activate', (e) =>
  e.waitUntil((async () => {
    const keys = await caches.keys();
    await Promise.all(keys.filter((k) => k !== CACHE).map((k) => caches.delete(k)));
    await self.clients.claim();
  })()));

self.addEventListener('fetch', (e) => {
  if (e.request.method !== 'GET') return;
  e.respondWith((async () => {
    const cached = await caches.match(e.request);
    if (cached) return cached;
    const res = await fetch(e.request);
    if (res.ok && res.type === 'basic') {
      const cache = await caches.open(CACHE);
      cache.put(e.request, res.clone());
    }
    return res;
  })());
});
```
- Use `type: 'module'` SWs in evergreen browsers (Safari 16+).
- Bump `CACHE` name on deploys; `activate` deletes old caches.
- Only cache `GET`. Skip opaque cross-origin responses (`res.type === 'opaque'`).

## Storage

### `localStorage` / `sessionStorage`
```js
localStorage.setItem('theme', 'dark');
const theme = localStorage.getItem('theme') ?? 'light';
```
- Synchronous — blocks the main thread. Keep values small (<50 KB).
- Throws on quota exceeded — wrap writes in `try/catch`.
- Not for sensitive data (tokens). Use cookies with `HttpOnly; Secure; SameSite=Strict` for auth.

### IndexedDB — async, larger, structured
```js
const dbp = new Promise((resolve, reject) => {
  const req = indexedDB.open('app', 2);
  req.onupgradeneeded = () => {
    const db = req.result;
    if (!db.objectStoreNames.contains('kv')) {
      db.createObjectStore('kv'); // key-value, no keyPath
    }
  };
  req.onsuccess = () => resolve(req.result);
  req.onerror = () => reject(req.error);
});

async function kv(action, key, value) {
  const db = await dbp;
  return new Promise((resolve, reject) => {
    const tx = db.transaction('kv', action === 'get' ? 'readonly' : 'readwrite');
    const store = tx.objectStore('kv');
    const req = action === 'get' ? store.get(key) : store.put(value, key);
    req.onsuccess = () => resolve(req.result);
    req.onerror = () => reject(req.error);
  });
}

await kv('set', 'user', { id: 1, name: 'Ada' });
const user = await kv('get', 'user');
```
Prefer the `idb` npm package for non-trivial schemas — it promisifies the API. The above is the raw pattern when you cannot add a dep.

## Observers

### `IntersectionObserver`
```js
const io = new IntersectionObserver(
  (entries, observer) => {
    for (const e of entries) {
      if (!e.isIntersecting) continue;
      const img = e.target;
      img.src = img.dataset.src;
      observer.unobserve(img);
    }
  },
  { rootMargin: '100px', threshold: 0.1 },
);
document.querySelectorAll('img[data-src]').forEach((img) => io.observe(img));
```
Always `unobserve` (or `disconnect()`) after firing — leaked observers retain DOM nodes.

### `ResizeObserver`
```js
const ro = new ResizeObserver((entries) => {
  for (const e of entries) {
    const { width, height } = e.contentRect;
    layout(e.target, width, height);
  }
});
ro.observe(el);
```
Throttle heavy work — `ResizeObserver` can fire many times per frame.

### `MutationObserver`
```js
const mo = new MutationObserver((muts) => {
  for (const m of muts) {
    if (m.type === 'childList') m.addedNodes.forEach(enhance);
  }
});
mo.observe(root, { childList: true, subtree: true });
// later: mo.disconnect();
```

### `PerformanceObserver`
```js
const po = new PerformanceObserver((list) => {
  for (const e of list.getEntries()) console.log(e.name, e.duration);
});
po.observe({ entryTypes: ['measure', 'largest-contentful-paint', 'layout-shift'] });
```

## `BroadcastChannel` — cross-tab messaging

```js
const ch = new BroadcastChannel('app-state');
ch.onmessage = (e) => updateLocal(e.data);
ch.postMessage({ type: 'logout' });

// Other tabs receive the message — useful for syncing auth, theme, cart.
ch.close(); // when no longer needed
```

## Web Sockets

```js
const ws = new WebSocket('wss://api.example.com/realtime');
ws.addEventListener('open', () => ws.send(JSON.stringify({ type: 'subscribe', channel: 'prices' })));
ws.addEventListener('message', (e) => {
  const data = JSON.parse(e.data);
  render(data);
});
ws.addEventListener('close', (e) => {
  if (!e.wasClean) reconnect(); // exponential backoff
});
```
- Use `wss://` (TLS) — `ws://` is blocked by mixed-content rules.
- Reconnect with exponential backoff + jitter.
- Forbid trusting server messages blindly — validate shape before use.

## Web Crypto

```js
// Hash
const digest = new Uint8Array(
  await crypto.subtle.digest('SHA-256', new TextEncoder().encode('hello')),
);

// AES-GCM encrypt/decrypt
async function keyFromPassphrase(pass, salt) {
  const baseKey = await crypto.subtle.importKey(
    'raw', new TextEncoder().encode(pass), 'PBKDF2', false, ['deriveKey'],
  );
  return crypto.subtle.deriveKey(
    { name: 'PBKDF2', salt, iterations: 210_000, hash: 'SHA-256' },
    baseKey, { name: 'AES-GCM', length: 256 }, false, ['encrypt', 'decrypt'],
  );
}

const iv = crypto.getRandomValues(new Uint8Array(12));
const cipher = await crypto.subtle.encrypt(
  { name: 'AES-GCM', iv }, key, new TextEncoder().encode(plaintext),
);
```
- All crypto is async on `crypto.subtle`.
- Never reuse an IV with the same key. Generate fresh per encryption.
- For random bytes: `crypto.getRandomValues(arr)` (sync, secure).

## Streams API

```js
// Compress a fetch response with a transform stream
const res = await fetch('/large');
const compressed = res.body
  .pipeThrough(new CompressionStream('gzip'))
  .pipeThrough(new TextEncoderStream());

// Custom TransformStream
const upper = new TransformStream({
  transform(chunk, controller) {
    controller.enqueue(chunk.toString().toUpperCase());
  },
  flush(controller) {
    controller.enqueue('--END--');
  },
});

await res.body.pipeThrough(upper).pipeTo(writableStream);
```
- `pipeThrough` chains transforms; `pipeTo` consumes.
- Backpressure is automatic — slow consumers pause producers.

## `URL` and `URLSearchParams`

```js
const u = new URL('https://api.example.com/users');
u.pathname = '/users/42';
u.searchParams.set('expand', 'profile');
u.searchParams.append('fields', 'name');
u.hash = '#section';
u.toString(); // https://api.example.com/users/42?expand=profile&fields=name#section

const qs = new URLSearchParams(location.search);
const q = qs.get('q') ?? '';
qs.set('page', '2');
history.replaceState(null, '', `?${qs}`);
```
Never hand-build query strings — `URLSearchParams` handles encoding.

## Quick reference

| API | Use |
|---|---|
| `fetch(url, { signal, body })` | HTTP, streaming body |
| `new Worker(url, { type: 'module' })` | Off-main-thread CPU |
| `navigator.serviceWorker.register` | Offline cache, PWA |
| `localStorage` / `sessionStorage` | Small sync KV |
| IndexedDB (or `idb`) | Large async structured storage |
| `IntersectionObserver` | Lazy load, infinite scroll, scroll reveals |
| `ResizeObserver` | Responsive components |
| `MutationObserver` | DOM tree watch (framework-less enhancement) |
| `PerformanceObserver` | Web Vitals (LCP, CLS) + custom marks |
| `BroadcastChannel` | Cross-tab sync |
| `WebSocket('wss://')` | Bidirectional realtime |
| `crypto.subtle` | Hash, AES-GCM, PBKDF2, signing |
| `CompressionStream` / `DecompressionStream` | gzip/deflate streams |
| `URL` / `URLSearchParams` | URL building + query parsing |

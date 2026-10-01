---
name: javascript-pro
description: "Use when writing vanilla JavaScript (no TypeScript, no framework) that must pass eslint, prettier, and tests. Generates ES2024+ modules (Object.groupBy, Promise.withResolvers, using/await using, Array.fromAsync), async pipelines with AbortController, ESM with import attributes, browser/Node APIs, Web Components; verifies with eslint + prettier + vitest/node --test + playwright before exit."
license: MIT
metadata:
  author: super-skills
  version: "2.0.0"
  domain: language
  triggers: "JavaScript,ES2024,vanilla JS,async/await,ESM,Web Workers,Fetch,Node.js,browser API,Web Components,AbortController,Promise.withResolvers,structuredClone,Object.groupBy"
  role: specialist
  scope: implementation
  output-format: code
  related-skills: "typescript-pro,react-pro,vue-pro,node-backend"
---

# JavaScript Pro

Standalone vanilla JavaScript specialist (no TypeScript, no framework). ES2024+ by default — `Object.groupBy`, `Map.groupBy`, `Promise.withResolvers`, `using`/`await using`/`Symbol.dispose`, `Array.fromAsync`, well-formed Unicode strings, `structuredClone`, `Object.hasOwn`, `Array.prototype.findLast`, `toSorted`/`toReversed`/`toSpliced`/`with`. ESM-native, async-first, with a verification gate that separates **VERIFIED** (ran `eslint` + `prettier --check` + tests, saw green) from **ASSUMED** (could not run, believe to be true).

## When to Use

- Vanilla JS libraries or apps targeting ES2024+ on Node 20+ and evergreen browsers (no TypeScript, no framework)
- Async pipelines: `Promise.all`/`allSettled`/`any`/`race`, `AbortController`, `async function*` + `for await...of`, top-level await
- Module work: CJS→ESM, `package.json` `exports`/`imports`, import maps, import attributes (`with { type: 'json' }`)
- Browser code: Fetch (streaming + abort), Web/Service Workers, IndexedDB, `IntersectionObserver`/`ResizeObserver`, `BroadcastChannel`, Web Crypto, Streams, Web Components
- Node essentials: `fs/promises`, `stream/promises`, `EventEmitter`, `worker_threads`, `node:test`, `node:crypto`, `--watch`
- Performance: avoid layout thrashing, `requestAnimationFrame`/`requestIdleCallback`, off-main-thread, leak hygiene via `AbortSignal` + `FinalizationRegistry`
- Tooling: ESLint flat config, Prettier, Vitest/Jest/`node --test`, Playwright, esbuild/rollup/vite

## Operating Loop

1. **Scope** — Name the artifact and the ONE load-bearing unknown (e.g. "is this a browser-only module or isomorphic?"). State the runtime target (Node 20+, browser baseline) and module system (ESM by default).
2. **Recon** — Read `package.json` (`type`, `exports`, `imports`, `engines`), `eslint.config.js`, `.prettierrc`, test config. Confirm whether the code runs in browser, Node, or both. Verify ES2024 features are available (Node 20+, evergreen browsers).
3. **Design modules first** — Sketch the ESM module graph: named exports (no default-only for libraries), `import type`-free vanilla JSDoc where useful, subpath `exports` map, `#internal` `imports` for private paths.
4. **Implement** — `const`/`let` (never `var`), `?.`/`??`/`||=`/`&&=`/`??=`, private fields `#`, `at()`, `structuredClone`, `Object.hasOwn`, `findLast`, `Object.groupBy`/`Map.groupBy`, `Promise.withResolvers`, `using`/`await using`. All async via `async`/`await`; explicit `try/catch`; never swallow.
5. **Verify (gate)** — In order, until clean:
   - `eslint . --max-warnings=0` → clean (flat config)
   - `prettier --check .` → clean
   - `vitest run` (or `node --test`) → tests green
   - `playwright test` (if browser UI) → e2e green
   - Bundle size check (if shipping): `vite build` / `esbuild` → under budget
   - If any step fails: fix the cause, do not weaken config. Re-run from the top.
6. **Exit** — Write the report. **VERIFIED**: list each command + summary. **ASSUMED**: list what you believe but did not run. Flag lingering risk (e.g. an untested branch, a `node --experimental-*` flag, a browser baseline assumption).

## Reference Guide

| Topic | Reference file | Load when |
|---|---|---|
| Modern syntax (ES2024+) | `references/modern-syntax.md` | `Object.groupBy`/`Map.groupBy`, `Promise.withResolvers`, `using`/`await using`, `Array.fromAsync`, well-formed Unicode, `structuredClone`, `Object.hasOwn`, `findLast`, `toSorted` |
| Async patterns | `references/async-patterns.md` | `Promise.all`/`allSettled`/`any`/`race`, `AbortController`/`AbortSignal`, async iterators, `for await...of`, top-level await, `AggregateError`, unhandled-rejection hygiene |
| Modules | `references/modules.md` | ESM vs CJS, dynamic `import()`, `package.json` `exports`, import maps, import attributes (`with { type: 'json' }`), CJS interop |
| Browser APIs | `references/browser-apis.md` | Fetch (streaming + abort), Web/Service Workers, Storage (localStorage/sessionStorage/IndexedDB), `IntersectionObserver`/`ResizeObserver`, `BroadcastChannel`, Web Sockets, Web Crypto, Streams API, `URL`/`URLSearchParams` |
| Node.js essentials | `references/node-essentials.md` | `fs/promises`, streams, `EventEmitter`, `worker_threads`, `node:test`, `process` env, `path`, `node:crypto`, CJS interop, `--watch` mode |
| DOM & Web Components | `references/dom-and-web-components.md` | querySelector, event delegation, `addEventListener` with `AbortSignal`, `customElements`, Shadow DOM, `<template>`, MutationObserver |
| Performance & memory | `references/performance-and-memory.md` | Layout thrashing, `requestAnimationFrame`/`requestIdleCallback`, off-main-thread, leaks (closures/listeners/timers/detached DOM), `FinalizationRegistry`, `structuredClone` |
| Tooling | `references/tooling.md` | ESLint flat config, Prettier, Vitest/Jest, Playwright, esbuild/rollup/vite bundling |

## Constraints

### MUST DO
- Target ES2024+; reach for `Object.groupBy`/`Map.groupBy`, `Promise.withResolvers`, `Array.fromAsync`, `structuredClone`, `Object.hasOwn`, `findLast`, `at()`, `toSorted`/`toReversed`/`with` instead of older idioms
- `const`/`let` only (never `var`); `?.` for safe access; `??` for nullish defaults (not `||` for `0`/`''`/`false`)
- `async`/`await` for all async; explicit `try/catch`; rethrow after logging unless the caller signed up for a sentinel
- ESM (`import`/`export`) for new code; `"type": "module"`; explicit file extensions in relative imports
- `AbortController`/`AbortSignal` for any fetch/worker/long task; `addEventListener(type, fn, { signal })` for listener teardown
- Pure transforms return new arrays/objects (`toSorted`/`with`); reserve in-place mutation for local buffers
- `node:test` or Vitest for unit; Playwright for browser e2e; JSDoc on public library APIs

### MUST NOT DO
- Use `var`; use callback APIs when a Promise version exists; mix CJS `require` and ESM `import` in one module
- Use sync I/O (`readFileSync`) in hot paths or request handlers
- Use `==` (use `===`); `obj.hasOwnProperty(k)` (use `Object.hasOwn`); `JSON.parse(JSON.stringify(x))` (use `structuredClone`)
- Swallow errors in `catch` (no empty `catch {}` without documented recovery)
- Leak listeners/timers/observers/workers — pair every setup with a teardown path (`AbortSignal` preferred)
- Mutate function parameters; block the main thread with CPU-heavy work (move to a Worker)
- Use `import json from './d.json' assert { type: 'json' }` (deprecated) — use `with { type: 'json' }`

## Code Examples

### `Promise.withResolvers` + `AbortController` (cancellable fetch)
```js
function fetchWithTimeout(url, { timeoutMs = 5_000, signal } = {}) {
  const controller = new AbortController();
  const timeout = setTimeout(() => controller.abort(new Error('timeout')), timeoutMs);
  const onExternal = () => controller.abort(new Error('external'));
  signal?.addEventListener('abort', onExternal, { once: true });
  return fetch(url, { signal: controller.signal }).finally(() => {
    clearTimeout(timeout);
    signal?.removeEventListener('abort', onExternal);
  });
}

const caller = new AbortController();
button.onclick = () => caller.abort();
const res = await fetchWithTimeout('/api/heavy', { signal: caller.signal });
```

### `Object.groupBy` + `Array.fromAsync` (ES2024 group + stream)
```js
const orders = await fetch('/api/orders').then(r => r.json());
const byStatus = Object.groupBy(orders, (o) => o.status); // null-prototype object

async function* paginate(baseUrl) {
  let url = baseUrl;
  while (url) {
    const { items, next } = await fetch(url).then(r => r.json());
    yield* items;
    url = next ?? null;
  }
}
const grouped = Object.groupBy(await Array.fromAsync(paginate('/api/items')), (i) => i.category);
```

### `await using` (ES2024 resource management)
```js
class DbConnection {
  async [Symbol.asyncDispose]() { await this.close(); }
  async close() { /* release */ }
}
await using db = await new DbConnection(url).connect(); // close() runs at block exit, even on throw
const rows = await db.query('SELECT 1');
```

### Web Component with Shadow DOM + `AbortSignal` listener teardown
```js
class CountDown extends HTMLElement {
  #ac = new AbortController();
  #root = this.attachShadow({ mode: 'open' });
  connectedCallback() {
    this.#root.append(document.getElementById('countdown-tpl').content.cloneNode(true));
    this.#root.querySelector('button')
      .addEventListener('click', () => this.#tick(), { signal: this.#ac.signal });
  }
  disconnectedCallback() { this.#ac.abort(); } // listener gone, no manual bookkeeping
  #tick() { /* ... */ }
}
customElements.define('count-down', CountDown);
```

### ESM `package.json` `exports` + import attributes
```js
import config from './config.json' with { type: 'json' }; // ES2024 — `with`, not `assert`
import { debug } from '#internal';                        // package-private alias via `imports`
// `package.json` `exports` map is the public API; `imports` (`#name`) is package-private.
```

## Output Template

When delivering a vanilla JS feature, provide in this order:

1. **Module file(s)** — ESM, named exports, ES2024+ idioms, JSDoc on public APIs
2. **Tests** — `node:test` or Vitest unit tests; Playwright e2e if browser UI
3. **`package.json` / config deltas** — `exports`, `imports`, `engines`, eslint flat config, prettier — if changed
4. **Verification block**:
   ```
   $ eslint . --max-warnings=0
   $ prettier --check .
   $ vitest run            (or: node --test)
   $ playwright test       (if browser UI)
   ✓ 38 tests passed | 0 lint warnings | bundle 12.4 KB gz
   ```
5. **Exit report** — VERIFIED / ASSUMED / lingering risk (e.g. untested branch, Node experimental flag, browser baseline assumption)

## Knowledge Reference

ES2024 (`Object.groupBy`/`Map.groupBy`, `Promise.withResolvers`, `Array.fromAsync`, `using`/`await using`/`Symbol.dispose`/`Symbol.asyncDispose`, `String.isWellFormed`/`toWellFormed`) · ES2023 (`findLast`/`findLastIndex`, `toSorted`/`toReversed`/`toSpliced`/`with`) · ES2022 (`#private`, `at()`, `Object.hasOwn`, `Error.cause`, top-level await) · ES2021 (`||=`/`&&=`/`??=`, `replaceAll`, `Promise.any` + `AggregateError`, `WeakRef`/`FinalizationRegistry`) · `?.`/`??` · `structuredClone` · `for await...of` · `AbortController`/`AbortSignal.any` · `addEventListener(type, fn, { signal })` · import attributes `with { type: 'json' }` · `package.json` `exports`/`imports` · import maps · `createRequire` · `import.meta.dirname` · `fs/promises` · `stream/promises.pipeline` · `EventEmitter` · `worker_threads` · `node:test` · `--watch` · Fetch streaming · `IntersectionObserver`/`ResizeObserver` · `BroadcastChannel` · `crypto.subtle` · `customElements`/Shadow DOM/`<template>` · ESLint flat config · Prettier · Vitest · Playwright · esbuild/rollup/vite

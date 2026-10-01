# Modules

ESM is the default for new code. CommonJS stays supported for interop but should not be the chosen system for greenfield projects.

## ESM basics

```js
// Named exports (preferred for libraries)
export const add = (a, b) => a + b;
export function multiply(a, b) { return a * b; }
export class Calculator {}

// Default export (use sparingly — only one per module)
export default class Database {}

// Re-exports
export { add, multiply } from './math.js';
export * from './utils.js';
export * as helpers from './helpers.js';
```

## Imports

```js
import { add, multiply } from './math.js';
import { add as addition } from './math.js';
import Database from './database.js';        // default
import * as math from './math.js';            // namespace
import Database, { connect } from './db.js';  // mixed default + named
import './polyfill.js';                       // side-effect only
```

**ESM requires explicit file extensions in relative imports** — `./utils.js`, not `./utils`. Bundlers (vite/esbuild) and TypeScript `Bundler` resolution may relax this, but Node ESM does not.

## Dynamic `import()`

```js
// Lazy route loading
const routes = {
  home:   () => import('./pages/home.js'),
  about:  () => import('./pages/about.js'),
};
const { render } = await routes[location.pathname]();
render();

// Feature flag conditional
if (flags.fancyCharts) {
  const { Chart } = await import('./chart-lib.js');
  new Chart(canvas).render(data);
}

// Module cache is automatic — calling import() twice returns the same namespace
```

## `package.json` `exports` — the public API

```json
{
  "name": "@acme/geo",
  "version": "1.0.0",
  "type": "module",
  "main": "./dist/index.js",
  "exports": {
    ".": "./src/index.js",
    "./math": "./src/math.js",
    "./workers/": "./src/workers/",
    "./package.json": "./package.json"
  },
  "imports": {
    "#internal": "./src/internal.js",
    "#config": "./src/config.js"
  },
  "engines": { "node": ">=20" }
}
```
Rules:
- `exports` defines the **only** paths consumers can import. Anything not listed is private.
- Always expose `./package.json` — tooling needs it.
- Subpath patterns end with `/` and match a directory prefix.
- `imports` (`#name`) are package-private aliases — use them instead of relative `../../` paths.

### Conditional exports
```json
{
  "exports": {
    ".": {
      "browser": "./dist/browser.js",
      "node": "./dist/node.js",
      "default": "./dist/index.js"
    }
  }
}
```
Order matters: most-specific first. Common keys: `import`, `require`, `node`, `browser`, `default`. Avoid `development`/`production` — bundlers handle env differently.

## Import attributes (ES2024+) — `with { type: 'json' }`

```js
// JSON modules
import config from './config.json' with { type: 'json' };

// CSS modules (Chrome 122+, behind flag in some browsers)
import styles from './theme.css' with { type: 'css' };

// WebAssembly (Node 22+, Chrome 120+)
import { fn } from './math.wasm' with { type: 'wasm' };
```
- The old `assert { type: 'json' }` syntax is **deprecated** and will be removed. Use `with`.
- JSON imports are read-only — `config.x = 1` throws in strict mode.
- Use import attributes instead of `fs.readFile` for static JSON in app code; `fs.readFile` is still right for dynamic paths.

## Import maps (browser)

```html
<script type="importmap">
{
  "imports": {
    "lit":         "https://esm.sh/lit@3",
    "lit/":        "https://esm.sh/lit@3/",
    "utils/":      "/src/utils/",
    "#config":     "/src/config.js"
  },
  "scopes": {
    "/src/legacy/": {
      "lit": "https://esm.sh/lit@2"
    }
  }
}
</script>

<script type="module">
  import { html } from 'lit';
  import { h } from 'utils/h.js';
</script>
```
- Maps bare specifiers (`lit`) and trailing-slash prefixes (`lit/`, `utils/`).
- `scopes` override for specific paths — useful for version pinning legacy code.
- One import map per document; declare before any `<script type="module">`.

## CJS interop

```js
// ESM importing CJS — default export is module.exports
import cjs from './legacy.cjs';
// Named imports from CJS work in Node (statically analyzed) but are NOT guaranteed
// by the spec; bundle with esbuild/vite if you need them reliably.

// ESM needing `require`:
import { createRequire } from 'module';
const require = createRequire(import.meta.url);
const fs = require('fs'); // synchronous — sparingly

// __dirname / __filename in ESM:
import { fileURLToPath } from 'node:url';
import { dirname } from 'node:path';
const __filename = fileURLToPath(import.meta.url);
const __dirname = dirname(__filename);
```

## `import.meta`

```js
import.meta.url;                   // file:// URL of current module
import.meta.dirname;               // Node 20.11+ — __dirname equivalent
import.meta.filename;              // Node 20.11+ — __filename equivalent
import.meta.resolve('./other.js'); // sync, absolute URL — Node 20.6+
```

## Detecting "is this module the entry?"

```js
// Node 20+
import { isMain } from './is-main.js';
if (import.meta.url === `file://${process.argv[1]}`) {
  main();
}
```

## Circular dependencies

ESM handles cycles via live bindings, but they are still a design smell. The hoisted `const` you import may be `undefined` at module-eval time:

```js
// a.js
import { b } from './b.js';
export const a = 'A';
export function useB() { return b; }  // OK if called after init

// b.js
import { a } from './a.js';
export const b = 'B';
export function useA() { return a; }  // OK if called after init
```
Rule: only import **functions** from a cycle, never **values**. Functions defer the binding read to call-time. Better: refactor to a third module, or use dependency injection.

## Tree shaking — write side-effect-free code

```js
// Pure — bundler can drop unused exports
export const add = (a, b) => a + b;

// Side effect — prevents dead-code elimination of the whole module
console.log('module loaded');
export const add = (a, b) => a + b;
```
```json
// package.json
{
  "sideEffects": false
  // or: "sideEffects": ["*.css", "./src/polyfill.js"]
}
```
`sideEffects: false` lets esbuild/rollup/vite prune anything unreferenced. Set `true` (default) or list the files with side effects if your library has them.

## Top-level await rules (recap)

- ESM only
- Blocks importers
- No `try/catch` around it at the top level — wrap in an IIFE if needed:
```js
try {
  const cfg = await loadConfig();
  export default cfg;
} catch (err) {
  // SyntaxError — await at top level can't be in try/catch
}

// Correct:
const cfg = await (async () => {
  try { return await loadConfig(); }
  catch { return fallbackConfig; }
})();
export default cfg;
```

## Quick reference

| Need | Use |
|---|---|
| Default module system | ESM (`"type": "module"`) |
| Public API surface | `package.json` `exports` |
| Internal aliases | `package.json` `imports` (`#name`) |
| Lazy load | `await import('./x.js')` |
| Load JSON statically | `import x from './x.json' with { type: 'json' }` |
| Browser bare specifiers | `<script type="importmap">` |
| CJS in ESM | `import x from './legacy.cjs'` or `createRequire` |
| `__dirname` in ESM | `import.meta.dirname` (Node 20.11+) |
| Tree-shake | `sideEffects: false` + pure exports |

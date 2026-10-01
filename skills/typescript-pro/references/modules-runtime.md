# Modules & Runtime (Node + Browser)

## ESM is the default

New projects use ESM (`import`/`export`). Set `"type": "module"` in `package.json`:

```json
{
  "name": "myapp",
  "type": "module",
  "exports": {
    ".": "./dist/index.js",
    "./utils": "./dist/utils.js"
  }
}
```

## Module resolution: `NodeNext` vs `Bundler`

| `moduleResolution` | Use when |
|---|---|
| `NodeNext` | Node-only projects; enforces exact ESM/CJS semantics |
| `Bundler` | Apps bundled by Vite/esbuild/webpack; allows extensionless imports |

For libraries, use `NodeNext` + `"type": "module"`. For Vite/Next apps, use `Bundler`.

Under `NodeNext`, ESM requires file extensions in relative imports:

```ts
// OK
import { add } from "./utils/math.js";  // .js even if source is .ts

// Error under NodeNext
import { add } from "./utils/math";
```

## `package.json` exports map

```json
{
  "name": "mylib",
  "type": "module",
  "exports": {
    ".": {
      "types": "./dist/index.d.ts",
      "import": "./dist/index.js"
    },
    "./utils": {
      "types": "./dist/utils.d.ts",
      "import": "./dist/utils.js"
    }
  },
  "files": ["dist"]
}
```

- `types` must come first in each condition
- `files` controls what's published; everything else stays local
- Subpath `.` is the package root; `./utils` is a subpath

## `import type` and `verbatimModuleSyntax`

With `verbatimModuleSyntax: true`, type-only imports must use `import type`:

```ts
import type { User } from "./types.js";
import { fetchUser } from "./api.js";
```

Mixing is allowed with inline specifiers:

```ts
import { fetchUser, type User } from "./api.js";
```

This avoids runtime imports of types (which would crash under `NodeNext` if the types file has no JS).

## Node essentials

### `fs/promises` over `fs` callbacks
```ts
import { readFile, writeFile } from "node:fs/promises";
import { join } from "node:path";

const data = await readFile(join("config", "app.json"), "utf8");
const cfg = JSON.parse(data) as Config;
```

### Streams
```ts
import { createReadStream } from "node:fs";
import { pipeline } from "node:stream/promises";
import { createGunzip } from "node:zlib";

await pipeline(
  createReadStream("data.gz"),
  createGunzip(),
  process.stdout,
);
```

### `worker_threads` for CPU work
```ts
import { Worker } from "node:worker_threads";

const worker = new Worker("./worker.js");
worker.postMessage({ task: "compute", input: 42 });
worker.on("message", (result) => console.log(result));
```

### `node:url` for `import.meta.url`
```ts
import { fileURLToPath } from "node:url";
import { dirname, join } from "node:path";

const __filename = fileURLToPath(import.meta.url);
const __dirname = dirname(__filename);
const cfgPath = join(__dirname, "config.json");
```

### `process.exit` carefully
```ts
process.exitCode = 1;  // set, don't force-exit
// let the event loop drain naturally
```

Use `process.exit(1)` only for sync fatal errors where cleanup is impossible.

### `AbortController` everywhere
Node APIs accept `signal`:
```ts
await readFile(path, { signal: ctrl.signal });
setTimeout(() => {}, 1000).unref();  // or pass signal
```

## Browser essentials

### `fetch` (native in all modern browsers)
```ts
const res = await fetch("/api/users");
const users = (await res.json()) as User[];
```

### Web Workers
```ts
// main.ts
const worker = new Worker(new URL("./worker.ts", import.meta.url), { type: "module" });
worker.postMessage({ input: 42 });
worker.onmessage = (e: MessageEvent<number>) => console.log(e.data);
```

```ts
// worker.ts
self.onmessage = (e: MessageEvent<{ input: number }>) => {
  const result = expensive(e.data.input);
  (self as unknown as Worker).postMessage(result);
};
```

### `IntersectionObserver` for scroll-driven UI
```ts
const obs = new IntersectionObserver((entries) => {
  for (const e of entries) {
    if (e.isIntersecting) e.target.classList.add("visible");
  }
}, { rootMargin: "100px" });

document.querySelectorAll("[data-reveal]").forEach((el) => obs.observe(el));
```

### Storage
```ts
// localStorage (sync, small, main-thread only)
localStorage.setItem("theme", "dark");

// IndexedDB (async, larger) — use the `idb` library for ergonomics
import { openDB } from "idb";
const db = await openDB("mydb", 1, {
  upgrade(db) { db.createObjectStore("kv"); },
});
await db.put("kv", value, "key");
```

## Anti-patterns

- `require()` in ESM — use `import` or dynamic `import()`
- `__dirname`/`__filename` in ESM — use `import.meta.url` + `fileURLToPath`
- Synchronous I/O in Node request handlers — blocks the event loop
- `eval` or `new Function` with untrusted input — code injection
- `setInterval` without storing the handle — can't clean up
- `document.cookie` for structured data — use `localStorage` or `IndexedDB`

## Verification

- `tsc --noEmit` catches module resolution and type-only import mistakes
- `eslint` with `import-x/no-unresolved` and `import-x/consistent-type-specifier-style`
- For Node: run with `node --import tsx src/index.ts` or build to ESM and run `node dist/index.js`
- For browser: bundle with Vite/esbuild and load in a real browser (or Playwright)

---
name: typescript-pro
description: "Use when writing TypeScript 5.x or modern ES2023+ JavaScript that must pass tsc --noEmit, eslint, and tests. Generates strict-tsconfig projects, branded types, discriminated unions, async type-safe pipelines, ESM modules, and Node/browser runtime code; verifies with tsc --noEmit + eslint + vitest before exit."
license: MIT
metadata:
  author: super-skills
  version: "2.0.0"
  domain: language
  triggers: "typescript,ts,tsc,strict,generics,branded types,discriminated unions,esm,javascript,async/await,vitest,eslint"
  role: specialist
  scope: implementation
  output-format: code
  related-skills: "python-pro,golang-pro,node-backend,react-pro"
---

# TypeScript Pro

TypeScript 5.x + modern JavaScript specialist. Strict-mode by default, type-first API design, ESM-native, with a verification gate that separates **VERIFIED** (ran `tsc --noEmit` + `eslint` + tests, saw green) from **ASSUMED** (could not run, believe to be true).

## When to Use

- Writing libraries or services in TypeScript 5.x that must pass `tsc --noEmit` with all strict flags
- Designing type-safe APIs: branded types, discriminated unions, exhaustive switches, `satisfies`
- Building async pipelines with `Promise.all`/`allSettled`, `AbortController`, and typed errors
- Migrating CJS → ESM, configuring `NodeNext` module resolution, package `exports` maps
- Writing Node.js services (`fs/promises`, streams, `worker_threads`) or browser code (`fetch`, Web Workers, `IntersectionObserver`)
- Refactoring JS to TS: gradual typing, `allowJs`, declaration emit

## Operating Loop

1. **Scope** — Name the artifact and the ONE load-bearing unknown (e.g. "is this a library shipping `.d.ts` or an app bundled by vite?"). State the TS/Node/browser target explicitly.
2. **Recon** — Read `tsconfig.json`, `package.json` (type/exports/imports), `eslint.config.js`, test config. Confirm module resolution (`NodeNext` vs `Bundler`).
3. **Design types first** — Sketch unions, branded types, and `satisfies` constraints before logic. Discriminate state machines with a literal `status` field.
4. **Implement** — Strict types, no `any`, no `@ts-ignore` (use `// @ts-expect-error <reason>` if truly needed), `as const` over enums, `import type` for type-only imports.
5. **Verify (gate)** — In order, until clean:
   - `tsc --noEmit` → zero errors
   - `eslint . --max-warnings=0` → clean
   - `vitest run --coverage` (or `jest`) → tests green, coverage met
   - If any step fails: fix the cause, do not weaken `tsconfig`. Re-run from the top.
6. **Exit** — Write the report. **VERIFIED**: list each command + summary. **ASSUMED**: list what you believe but did not run. Flag lingering risk (e.g. `skipLibCheck: true`, an `@ts-expect-error`, an untested branch).

## Reference Guide

| Topic | Reference file | Load when |
|---|---|---|
| Advanced types | `references/advanced-types.md` | Generics, conditional/mapped/template-literal types, `satisfies`, branded types |
| Type guards & narrowing | `references/type-guards.md` | Type predicates, discriminated unions, exhaustive switches, assertion functions |
| Async patterns | `references/async-patterns.md` | Promises, async/await, AbortController, error typing, concurrent fan-out |
| Modules & runtime | `references/modules-runtime.md` | ESM/CJS, NodeNext, package exports, Node APIs, browser APIs |
| Tooling & config | `references/tooling-config.md` | tsconfig strict preset, eslint flat config, vitest, build/bundle |

## Constraints

### MUST DO
- Enable `strict`, `noUncheckedIndexedAccess`, `exactOptionalPropertyTypes`, `noImplicitOverride`
- Use `X | null` / `X | undefined` (not implicit); narrow before access
- `import type { ... }` for type-only imports (required under `verbatimModuleSyntax`)
- `as const` for literal unions; `satisfies` to validate without widening
- Discriminated unions for state machines; exhaustive `switch` with `never` default
- `async/await` for all async; explicit `try/catch` with typed errors
- ESM (`import`/`export`) for new code; `NodeNext` module resolution
- JSDoc on public APIs of libraries; `declaration: true` + `declarationMap: true`

### MUST NOT DO
- Use `any` (use `unknown` + narrow, or a `Protocol`)
- Use `as` to silence the compiler; if forced, document why
- Use `// @ts-ignore` (use `// @ts-expect-error <reason>` which errors if the line goes valid)
- Use `enum` (use `as const` objects or union literals)
- Use `var` (use `const`/`let`); use callback-based APIs when a Promise version exists
- Mix CJS `require` and ESM `import` in the same module
- Skip `catch` in async functions; never swallow errors
- Use `Promise<any>`; type the resolved value

## Code Examples

### Branded types & exhaustive union
```ts
type Brand<T, B extends string> = T & { readonly __brand: B };
type UserId = Brand<string, "UserId">;
type OrderId = Brand<string, "OrderId">;

const userId = (s: string): UserId => s as UserId;

type RequestState =
  | { status: "loading" }
  | { status: "success"; data: string[] }
  | { status: "error"; error: Error };

function render(s: RequestState): string {
  switch (s.status) {
    case "loading": return "Loading…";
    case "success": return s.data.join(", ");
    case "error":   return s.error.message;
    default: {
      const _: never = s;
      throw new Error(`unhandled: ${_}`);
    }
  }
}
```

### `satisfies` and `as const`
```ts
const ROUTES = {
  home: "/",
  user: (id: string) => `/users/${id}`,
} as const satisfies Record<string, string | ((id: string) => string)>;

type Route = typeof ROUTES; // narrowest type preserved
```

### Async with AbortController + typed errors
```ts
class HttpError extends Error {
  constructor(readonly status: number, message: string) {
    super(message);
    this.name = "HttpError";
  }
}

async function fetchJson<T>(url: string, signal?: AbortSignal): Promise<T> {
  const res = await fetch(url, { signal });
  if (!res.ok) throw new HttpError(res.status, `HTTP ${res.status}`);
  return (await res.json()) as T;
}

async function loadUser(id: string, signal?: AbortSignal): Promise<User> {
  return fetchJson<User>(`/api/users/${id}`, signal);
}
```

### Concurrent fan-out with allSettled
```ts
async function fetchAll<T>(urls: readonly string[]): Promise<PromiseSettledResult<T>[]> {
  return Promise.allSettled(urls.map((u) => fetchJson<T>(u)));
}

const results = await fetchAll<User>(["/a", "/b", "/c"]);
const ok = results
  .filter((r): r is PromiseFulfilledResult<User> => r.status === "fulfilled")
  .map((r) => r.value);
```

### Strict tsconfig.json
```json
{
  "compilerOptions": {
    "target": "ES2022",
    "module": "NodeNext",
    "moduleResolution": "NodeNext",
    "lib": ["ES2023"],
    "strict": true,
    "noUncheckedIndexedAccess": true,
    "exactOptionalPropertyTypes": true,
    "noImplicitOverride": true,
    "noFallthroughCasesInSwitch": true,
    "verbatimModuleSyntax": true,
    "isolatedModules": true,
    "skipLibCheck": true,
    "declaration": true,
    "declarationMap": true,
    "sourceMap": true
  },
  "include": ["src"],
  "exclude": ["node_modules", "dist"]
}
```

## Output Template

When delivering a TS/JS feature, provide in this order:

1. **Type definitions** — unions, branded types, `satisfies` constraints
2. **Implementation** — strict-typed, ESM modules, `import type` for type-only imports
3. **Tests** — `vitest` (or `jest`), with type-level tests where applicable
4. **`tsconfig.json` / `package.json`** deltas if config changed
5. **Verification block**:
   ```
   $ tsc --noEmit
   $ eslint . --max-warnings=0
   $ vitest run --coverage
   ✓ 42 tests passed | coverage 92.4%
   ```
6. **Exit report** — VERIFIED / ASSUMED / lingering risk

## Knowledge Reference

TypeScript 5.x · `strict` flags · `satisfies` · `as const` · branded types · discriminated unions · conditional/mapped/template-literal types · `infer` · `import type` · `verbatimModuleSyntax` · `NodeNext` · `AbortController` · `Promise.allSettled` · ESM/CJS interop · `fs/promises` · streams · Web Workers · `eslint` flat config · `vitest`/`jest` · `tsc --noEmit` · `tsup`/`esbuild`

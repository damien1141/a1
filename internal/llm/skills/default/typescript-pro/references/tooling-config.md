# Tooling & Config

## Strict tsconfig.json

```json
{
  "compilerOptions": {
    "target": "ES2022",
    "module": "NodeNext",
    "moduleResolution": "NodeNext",
    "lib": ["ES2023"],
    "strict": true,
    "noUncheckedIndexedAccess": true,
    "noImplicitOverride": true,
    "noFallthroughCasesInSwitch": true,
    "noImplicitReturns": true,
    "exactOptionalPropertyTypes": true,
    "verbatimModuleSyntax": true,
    "isolatedModules": true,
    "skipLibCheck": true,
    "declaration": true,
    "declarationMap": true,
    "sourceMap": true,
    "incremental": true,
    "tsBuildInfoFile": ".tsbuildinfo"
  },
  "include": ["src"],
  "exclude": ["node_modules", "dist"]
}
```

### Flag rationale

| Flag | Why |
|---|---|
| `strict` | Enables `noImplicitAny`, `strictNullChecks`, `strictFunctionTypes`, `strictBindCallApply`, `strictPropertyInitialization`, `alwaysStrict`, `noImplicitThis`, `useUnknownInCatchVariables` |
| `noUncheckedIndexedAccess` | `arr[i]` is `T \| undefined` — forces narrowing |
| `exactOptionalPropertyTypes` | `x?: T` is distinct from `x: T \| undefined` — be deliberate |
| `noImplicitOverride` | `override` keyword required when subclassing |
| `verbatimModuleSyntax` | Forces `import type` for type-only imports |
| `isolatedModules` | Each file compiles independently (required by esbuild/vite) |
| `skipLibCheck` | Skip `.d.ts` checks (faster; common lib bugs are not your problem) |

## Project references (monorepos)

```json
{
  "files": [],
  "references": [
    { "path": "./packages/core" },
    { "path": "./packages/cli" },
    { "path": "./packages/web" }
  ]
}
```

Each sub-project has its own `tsconfig.json` with `composite: true`. Build with `tsc --build`.

## ESLint flat config (9.x+)

```js
// eslint.config.js
import tseslint from "typescript-eslint";
import importPlugin from "eslint-plugin-import-x";

export default tseslint.config(
  tseslint.configs.strictTypeChecked,
  {
    languageOptions: {
      parserOptions: { projectService: true },
    },
    plugins: { "import-x": importPlugin },
    rules: {
      "import-x/no-unresolved": "error",
      "import-x/consistent-type-specifier-style": "warn",
      "@typescript-eslint/no-unused-vars": ["error", { argsIgnorePattern: "^_" }],
    },
  },
  {
    ignores: ["dist/**", "coverage/**"],
  },
);
```

Run: `eslint . --max-warnings=0`.

## Vitest config

```ts
// vitest.config.ts
import { defineConfig } from "vitest/config";

export default defineConfig({
  test: {
    environment: "node",
    coverage: {
      provider: "v8",
      reporter: ["text", "html"],
      thresholds: { lines: 90, branches: 85, functions: 90, statements: 90 },
    },
    include: ["tests/**/*.test.ts"],
  },
});
```

Type-level tests:

```ts
import { describe, it, expectTypeOf } from "vitest";

describe("types", () => {
  it("UserId is branded", () => {
    expectTypeOf<UserId>().toMatchTypeOf<string>();
  });
});
```

## Build

### Libraries — `tsup` or `tsc`
```ts
// tsup.config.ts
import { defineConfig } from "tsup";

export default defineConfig({
  entry: ["src/index.ts"],
  format: ["esm"],
  dts: true,
  clean: true,
  sourcemap: true,
});
```

### Apps — Vite / esbuild
```ts
// vite.config.ts
import { defineConfig } from "vite";

export default defineConfig({
  build: { target: "es2022", sourcemap: true },
});
```

## Node version & `engines`

```json
{
  "engines": { "node": ">=20.0.0" },
  "packageManager": "pnpm@9.0.0"
}
```

Use `packageManager` field (Corepack) to pin the package manager.

## package.json scripts

```json
{
  "scripts": {
    "build": "tsup",
    "check": "tsc --noEmit",
    "lint": "eslint . --max-warnings=0",
    "test": "vitest run",
    "test:watch": "vitest",
    "test:cov": "vitest run --coverage",
    "verify": "npm run check && npm run lint && npm run test:cov"
  }
}
```

The `verify` script is the single source of truth for the verification gate.

## Common pitfalls

- `skipLibCheck: false` — slows builds; only enable when auditing deps
- `target: ESNext` — emits code Node may not support; pin to a known target
- `module: CommonJS` for new projects — use ESM
- Forgetting `"type": "module"` — defaults to CJS, breaks ESM imports
- `verbatimModuleSyntax` without `isolatedModules` — odd interaction; enable both
- Mixing `tsc --build` with `tsup` in the same project — pick one build path

## Verification gate

```bash
tsc --noEmit
eslint . --max-warnings=0
vitest run --coverage
```

All three must pass. If `tsc` passes but `eslint` fails, the gate is red. Re-run all from the top after a fix.

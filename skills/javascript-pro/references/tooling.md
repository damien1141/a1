# Tooling

Vanilla JS toolchain: ESLint (flat config) + Prettier + Vitest (or `node --test`) + Playwright + a bundler (esbuild/rollup/vite). The gate is `eslint . --max-warnings=0 && prettier --check . && vitest run && playwright test`.

## ESLint — flat config (ESLint 9+)

`eslint.config.js` (ESM):
```js
import js from '@eslint/js';
import globals from 'globals';

export default [
  js.configs.recommended,
  {
    languageOptions: {
      ecmaVersion: 2024,
      sourceType: 'module',
      globals: {
        ...globals.browser,
        ...globals.node,
      },
    },
    rules: {
      'no-unused-vars': ['error', { argsIgnorePattern: '^_', varsIgnorePattern: '^_' }],
      'no-console': ['warn', { allow: ['warn', 'error'] }],
      'no-throw-literal': 'error',
      'prefer-const': 'error',
      'eqeqeq': ['error', 'always'],
      'no-var': 'error',
      'object-shorthand': 'error',
      'prefer-arrow-callback': 'error',
      'prefer-template': 'error',
    },
  },
  {
    ignores: ['dist/**', 'node_modules/**', 'coverage/**'],
  },
];
```
- Flat config replaces `.eslintrc.*` (`.eslintrc.json` still works in ESLint 8.x but is removed in 9+).
- `languageOptions.globals` replaces `env: { browser: true }`. Use the `globals` npm package.
- Plugins: import the config array from each plugin and spread it: `import tseslint from 'typescript-eslint'` etc.

Run:
```bash
npx eslint . --max-warnings=0
npx eslint . --fix          # auto-fix
```

### Type-aware JS (optional)
```js
import jsdoc from 'eslint-plugin-jsdoc';
// jsdoc.configs.flatConfigs.recommended
```
Use JSDoc + `eslint-plugin-jsdoc` for library surfaces.

## Prettier

`.prettierrc.json`:
```json
{
  "semi": true,
  "singleQuote": true,
  "trailingComma": "all",
  "printWidth": 100,
  "useTabs": false,
  "tabWidth": 2,
  "arrowParens": "always"
}
```
```bash
npx prettier --check .
npx prettier --write .
```
- Prettier and ESLint overlap on formatting. Use `eslint-config-prettier` to disable conflicting ESLint rules:
```js
import prettier from 'eslint-config-prettier';
export default [/* ... */, prettier];
```

## Vitest

`vitest.config.ts` (or `.js`):
```js
import { defineConfig } from 'vitest/config';

export default defineConfig({
  test: {
    environment: 'node',       // or 'jsdom', 'happy-dom'
    coverage: {
      provider: 'v8',
      reporter: ['text', 'html'],
      thresholds: { lines: 85, functions: 85, branches: 80 },
    },
    include: ['src/**/*.test.{js,mjs}'],
  },
});
```
Test:
```js
import { test, expect, vi } from 'vitest';
import { add } from './math.js';

test('adds', () => {
  expect(add(1, 2)).toBe(3);
});

test('async', async () => {
  await expect(fetchUser(1)).resolves.toMatchObject({ id: 1 });
});

test('mock', () => {
  const fn = vi.fn().mockReturnValue(42);
  use(fn);
  expect(fn).toHaveBeenCalledOnce();
});
```
Run:
```bash
npx vitest run            # one-shot
npx vitest run --coverage
npx vitest watch          # dev
```

### `node --test` — no install required
```js
// math.test.js
import { test } from 'node:test';
import assert from 'node:assert/strict';
import { add } from './math.js';

test('adds', () => {
  assert.equal(add(1, 2), 3);
});
```
```bash
node --test
node --test --watch
node --test --experimental-test-coverage
```
Pick Vitest for browser env (jsdom/happy-dom), mocking, snapshot ergonomics; pick `node --test` for zero-dep pure-Node libraries.

## Playwright

`playwright.config.js`:
```js
import { defineConfig, devices } from '@playwright/test';

export default defineConfig({
  testDir: './e2e',
  fullyParallel: true,
  retries: process.env.CI ? 2 : 0,
  reporter: process.env.CI ? 'github' : 'html',
  use: {
    baseURL: 'http://localhost:4173',
    trace: 'on-first-retry',
  },
  webServer: {
    command: 'vite preview --port 4173',
    url: 'http://localhost:4173',
    reuseExistingServer: !process.env.CI,
  },
  projects: [
    { name: 'chromium', use: devices['Desktop Chrome'] },
    { name: 'firefox',  use: devices['Desktop Firefox'] },
    { name: 'webkit',   use: devices['Desktop Safari'] },
  ],
});
```
Test:
```js
import { test, expect } from '@playwright/test';

test('dropdown opens', async ({ page }) => {
  await page.goto('/');
  await page.getByRole('button', { name: 'Menu' }).click();
  await expect(page.getByRole('menu')).toBeVisible();
  await page.keyboard.press('Escape');
  await expect(page.getByRole('menu')).toBeHidden();
});
```
```bash
npx playwright test
npx playwright test --ui        # interactive mode
npx playwright test --headed
npx playwright test --project=chromium
```
- Always test keyboard nav (Tab, Escape, Enter) and ARIA roles, not just click.
- `--trace=on-first-retry` makes CI failures debuggable.
- Set up `webServer` so a single `playwright test` boots the app.

## Bundlers — pick by use case

### esbuild (fastest, dev servers / libraries)
```bash
esbuild src/index.js --bundle --format=esm --outfile=dist/index.js --minify --sourcemap
```
```js
// build.mjs
import esbuild from 'esbuild';

await esbuild.build({
  entryPoints: ['src/index.js'],
  bundle: true,
  format: 'esm',
  outfile: 'dist/index.js',
  sourcemap: true,
  minify: true,
  target: ['es2022'],
  external: [],            // keep deps external for libraries
});
```

### Rollup (libraries with code-splitting + tree-shaking)
```js
// rollup.config.js
import { nodeResolve } from '@rollup/plugin-node-resolve';
import terser from '@rollup/plugin-terser';

export default {
  input: 'src/index.js',
  output: [
    { file: 'dist/index.mjs', format: 'esm', sourcemap: true },
    { file: 'dist/index.cjs', format: 'cjs', sourcemap: true },
  ],
  plugins: [nodeResolve(), terser()],
  external: [/node:.*/, 'lit'],
};
```

### Vite (apps, full dev server)
```js
// vite.config.js
import { defineConfig } from 'vite';

export default defineConfig({
  build: {
    target: 'es2022',
    sourcemap: true,
    rollupOptions: {
      output: {
        manualChunks: { vendor: ['lit'] },
      },
    },
  },
});
```
Vite = esbuild in dev + Rollup in prod. Use for SPAs, component playgrounds, full apps.

## Bundle size budget

```js
// vite.config.js — fail CI if bundle exceeds budget
import { defineConfig } from 'vite';
import { visualizer } from 'rollup-plugin-visualizer';

export default defineConfig({
  build: {
    rollupOptions: { output: { manualChunks: { vendor: ['lit'] } } },
  },
  plugins: [
    visualizer({ open: false, gzipSize: true, brotliSize: true, filename: 'stats.html' }),
  ],
});
```
- Set a budget (e.g. `< 50 KB` gzip initial JS) and enforce in CI: parse `stats.html` or use `size-limit`.
- Tree-shake: `package.json` `"sideEffects": false`, ESM-only, named exports.
- Code-split routes with dynamic `import()`.

## `package.json` scripts

```json
{
  "scripts": {
    "lint": "eslint . --max-warnings=0",
    "format": "prettier --write .",
    "format:check": "prettier --check .",
    "test": "vitest run",
    "test:watch": "vitest watch",
    "test:e2e": "playwright test",
    "build": "vite build",
    "ci": "npm run lint && npm run format:check && npm run test && npm run build && npm run test:e2e"
  }
}
```

## Quick reference

| Need | Tool |
|---|---|
| Lint | ESLint flat config (`eslint.config.js`) |
| Format | Prettier + `eslint-config-prettier` |
| Unit test (browser env) | Vitest + jsdom/happy-dom |
| Unit test (zero dep) | `node --test` + `node:assert/strict` |
| E2E | Playwright (chromium + firefox + webkit) |
| Bundle library | Rollup (dual ESM/CJS) or esbuild |
| Bundle app | Vite |
| Bundle size | `rollup-plugin-visualizer` + `size-limit` |
| CI gate | `npm run ci` chain of all the above |

# Vite config + Vitest + Vue Test Utils

## Vite config

```ts
// vite.config.ts
import { defineConfig } from 'vite';
import vue from '@vitejs/plugin-vue';
import { fileURLToPath, URL } from 'node:url';

export default defineConfig({
  plugins: [vue()],
  resolve: {
    alias: {
      '@': fileURLToPath(new URL('./src', import.meta.url)),
      '@components': fileURLToPath(new URL('./src/components', import.meta.url)),
      '@composables': fileURLToPath(new URL('./src/composables', import.meta.url)),
      '@stores': fileURLToPath(new URL('./src/stores', import.meta.url)),
    },
  },
  server: {
    port: 5173,
    host: true,
    proxy: {
      '/api': { target: 'http://localhost:3000', changeOrigin: true, rewrite: (p) => p.replace(/^\/api/, '') },
      '/ws': { target: 'ws://localhost:3000', ws: true },
    },
  },
  build: {
    sourcemap: process.env.NODE_ENV === 'production' ? 'hidden' : true,
    chunkSizeWarningLimit: 500,
    rollupOptions: {
      output: {
        manualChunks: {
          vendor: ['vue', 'vue-router', 'pinia'],
          ui: ['quasar', '@quasar/extras'],
        },
      },
    },
  },
});
```

### Auto-import plugins

```ts
import Components from 'unplugin-vue-components/vite';
import AutoImport from 'unplugin-auto-import/vite';

export default defineConfig({
  plugins: [
    vue(),
    Components({ dirs: ['src/components'], dts: 'src/components.d.ts' }),
    AutoImport({
      imports: ['vue', 'vue-router', 'pinia'],
      dts: 'src/auto-imports.d.ts',
      dirs: ['src/composables'],
      vueTemplate: true,
    }),
  ],
});
```

### Environment variables

```ts
// .env
// VITE_API_URL=https://api.example.com
// VITE_APP_TITLE=My App

const apiUrl = import.meta.env.VITE_API_URL;
const isDev = import.meta.env.DEV;

// vite-env.d.ts
/// <reference types="vite/client" />
interface ImportMetaEnv {
  readonly VITE_API_URL: string;
  readonly VITE_APP_TITLE: string;
}
interface ImportMeta { readonly env: ImportMetaEnv }
```

## Code splitting

```ts
import { defineAsyncComponent } from 'vue';

const HeavyChart = defineAsyncComponent(() => import('./HeavyChart.vue'));

const AdminPanel = defineAsyncComponent({
  loader: () => import('./AdminPanel.vue'),
  loadingComponent: LoadingSpinner,
  errorComponent: ErrorDisplay,
  delay: 200,
  timeout: 10_000,
});
```

```ts
// Route-based splitting
const routes = [
  { path: '/dashboard', component: () => import('./views/Dashboard.vue') },
  { path: '/settings',  component: () => import('./views/Settings.vue') },
];
```

```ts
// Manual chunks by package
export default defineConfig({
  build: {
    rollupOptions: {
      output: {
        manualChunks(id) {
          if (id.includes('node_modules')) {
            const pkg = id.split('node_modules/')[1].split('/')[0];
            return `vendor-${pkg}`;
          }
        },
      },
    },
  },
});
```

### Tree-shaking

```ts
// Good: named imports
import { ref, computed, watch } from 'vue';
import { format, parseISO } from 'date-fns';

// Bad: namespace import pulls everything
import * as Vue from 'vue';
```

```json
// package.json
{
  "sideEffects": ["*.css", "*.scss", "*.vue"]
}
```

### Compression

```ts
import viteCompression from 'vite-plugin-compression';

export default defineConfig({
  plugins: [
    viteCompression({ algorithm: 'gzip', ext: '.gz', threshold: 1024 }),
    viteCompression({ algorithm: 'brotliCompress', ext: '.br', threshold: 1024 }),
  ],
});
```

### Bundle analyzer

```ts
import { visualizer } from 'rollup-plugin-visualizer';

export default defineConfig({
  plugins: [visualizer({ filename: 'stats.html', gzipSize: true, brotliSize: true, template: 'treemap' })],
});
```

Run `pnpm build` then open `stats.html`.

## Vitest setup

```ts
// vitest.config.ts
import { defineConfig } from 'vitest/config';
import vue from '@vitejs/plugin-vue';
import { fileURLToPath, URL } from 'node:url';

export default defineConfig({
  plugins: [vue()],
  resolve: { alias: { '@': fileURLToPath(new URL('./src', import.meta.url)) } },
  test: {
    environment: 'jsdom',
    globals: true,
    setupFiles: ['./vitest.setup.ts'],
    coverage: { provider: 'v8', reporter: ['text', 'html'], thresholds: { lines: 80 } },
  },
});
```

```ts
// vitest.setup.ts
import { config } from '@vue/test-utils';
import { vi } from 'vitest';

vi.stubGlobal('fetch', vi.fn());

config.global.stubs = {
  'router-link': { template: '<a><slot /></a>' },
  'router-view': { template: '<div><slot /></div>' },
};
```

## Component tests

```ts
// Button.spec.ts
import { describe, it, expect } from 'vitest';
import { mount } from '@vue/test-utils';
import Button from './Button.vue';

describe('Button', () => {
  it('renders slot content', () => {
    const w = mount(Button, { slots: { default: 'Click me' } });
    expect(w.text()).toBe('Click me');
  });

  it('applies variant class', () => {
    const w = mount(Button, { props: { variant: 'danger' } });
    expect(w.classes()).toContain('btn--danger');
  });

  it('emits click event', async () => {
    const w = mount(Button);
    await w.trigger('click');
    expect(w.emitted('click')).toHaveLength(1);
  });

  it('is disabled when prop is true', () => {
    const w = mount(Button, { props: { disabled: true } });
    expect(w.attributes('disabled')).toBeDefined();
  });
});
```

### Testing `v-model`

```ts
import { mount } from '@vue/test-utils';
import TextInput from './TextInput.vue';

it('emits update:modelValue on input', async () => {
  const w = mount(TextInput, { props: { modelValue: '' } });
  await w.find('input').setValue('new value');
  expect(w.emitted('update:modelValue')).toEqual([['new value']]);
});
```

### Async tests with `flushPromises`

```ts
import { mount, flushPromises } from '@vue/test-utils';
import UserList from './UserList.vue';

it('renders users after fetch', async () => {
  global.fetch = vi.fn().mockResolvedValue({
    ok: true,
    json: () => Promise.resolve([{ id: 1, name: 'Alice' }]),
  });

  const w = mount(UserList);
  await flushPromises();
  expect(w.text()).toContain('Alice');
});
```

### Mocking composables

```ts
import { vi } from 'vitest';
import { ref, computed } from 'vue';
import * as useAuthModule from '@/composables/use-auth';
import Header from './Header.vue';

vi.spyOn(useAuthModule, 'useAuth').mockReturnValue({
  user: ref(null),
  isLoggedIn: computed(() => false),
  login: vi.fn(),
  logout: vi.fn(),
});

const w = mount(Header);
expect(w.find('[data-test="login-btn"]').exists()).toBe(true);
```

### Testing with Pinia

```ts
import { mount } from '@vue/test-utils';
import { createTestingPinia } from '@pinia/testing';
import CartSummary from './CartSummary.vue';
import { useCartStore } from '@/stores/cart';

it('displays cart total', () => {
  const w = mount(CartSummary, {
    global: {
      plugins: [createTestingPinia({
        initialState: { cart: { items: [{ id: '1', name: 'A', price: 100, qty: 2 }] } },
      })],
    },
  });
  expect(w.text()).toContain('$200');
});

it('calls checkout action', async () => {
  const w = mount(CartSummary, { global: { plugins: [createTestingPinia()] } });
  await w.find('[data-test="checkout-btn"]').trigger('click');
  expect(useCartStore().checkout).toHaveBeenCalled();
});
```

### Provide / inject in tests

```ts
import { ref } from 'vue';
const w = mount(ChildComponent, {
  global: { provide: { theme: ref('dark') } },
});
```

## Playwright e2e

```ts
// playwright.config.ts
import { defineConfig, devices } from '@playwright/test';

export default defineConfig({
  testDir: './e2e',
  webServer: { command: 'pnpm dev', url: 'http://localhost:5173', reuseExistingServer: !process.env.CI },
  use: { baseURL: 'http://localhost:5173', trace: 'on-first-retry' },
  projects: [
    { name: 'chromium', use: { ...devices['Desktop Chrome'] } },
    { name: 'webkit', use: { ...devices['Desktop Safari'] } },
  ],
});
```

```ts
import { test, expect } from '@playwright/test';

test('user can search', async ({ page }) => {
  await page.goto('/');
  await page.getByLabel('Search').fill('vue');
  await expect(page.getByText(/results for "vue"/i)).toBeVisible();
});
```

## Verification gates

```bash
pnpm vue-tsc --noEmit        # TS type-check (.vue + .ts)
pnpm eslint .                # lint
pnpm vitest run              # unit/component tests
pnpm vitest run --coverage   # with coverage
pnpm vite build              # production build (or `pnpm nuxt build` for Nuxt)
pnpm playwright test         # e2e (if present)
```

## Quick Reference

| Concern | Tool |
|---------|------|
| Bundler | Vite 5+ |
| Vue plugin | `@vitejs/plugin-vue` |
| Auto-import | `unplugin-vue-components`, `unplugin-auto-import` |
| Compression | `vite-plugin-compression` (gzip + brotli) |
| Analyzer | `rollup-plugin-visualizer` |
| Lazy load | `defineAsyncComponent`, dynamic route imports |
| Unit/component | Vitest + `@vue/test-utils` |
| Pinia testing | `createTestingPinia({ initialState })` |
| Mock composables | `vi.spyOn(module, 'fn').mockReturnValue(...)` |
| Async | `await flushPromises()` |
| e2e | Playwright |
| Type-check | `vue-tsc --noEmit` (TS) or `tsc --checkJs` (JS+JSDoc) |

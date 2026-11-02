# Testing: RTL + Vitest + MSW + Playwright

Component tests with React Testing Library, hook tests with `renderHook`, network mocking with MSW, and end-to-end with Playwright. Co-locate `*.test.tsx` next to the component.

## Setup

`vitest.config.ts`:
```ts
import { defineConfig } from 'vitest/config';
import react from '@vitejs/plugin-react';
import { fileURLToPath, URL } from 'node:url';

export default defineConfig({
  plugins: [react()],
  resolve: { alias: { '@': fileURLToPath(new URL('./src', import.meta.url)) } },
  test: {
    environment: 'jsdom',
    globals: true,
    setupFiles: ['./vitest.setup.ts'],
    coverage: { provider: 'v8', reporter: ['text', 'html'], thresholds: { lines: 80 } },
  },
});
```

`vitest.setup.ts`:
```ts
import '@testing-library/jest-dom/vitest';
import { cleanup } from '@testing-library/react';
import { afterEach } from 'vitest';

afterEach(() => cleanup());
```

## Query priority

Prefer queries that mirror how users find elements:

```tsx
screen.getByRole('button', { name: /submit/i });
screen.getByLabelText('Email');
screen.getByPlaceholderText('Search…');
screen.getByText('Welcome');

// Async (wait for element)
await screen.findByText('Loaded');

// Negation (assert NOT present)
expect(screen.queryByText('Error')).not.toBeInTheDocument();

// Test ID only as last resort
screen.getByTestId('custom-element');
```

## Component test

```tsx
import { render, screen } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { describe, it, expect } from 'vitest';
import { Counter } from './counter';

describe('Counter', () => {
  it('increments on click', async () => {
    const user = userEvent.setup();
    render(<Counter />);
    await user.click(screen.getByRole('button', { name: /increment/i }));
    expect(screen.getByText('1')).toBeInTheDocument();
  });
});
```

## Form test

```tsx
it('submits form', async () => {
  const onSubmit = vi.fn();
  const user = userEvent.setup();
  render(<ContactForm onSubmit={onSubmit} />);

  await user.type(screen.getByLabelText('Name'), 'John Doe');
  await user.type(screen.getByLabelText('Email'), 'john@example.com');
  await user.click(screen.getByRole('button', { name: /submit/i }));

  await expect(onSubmit).toHaveBeenCalledWith({
    name: 'John Doe',
    email: 'john@example.com',
  });
});
```

## Custom render with providers

```tsx
// test-utils.tsx
import { render, type RenderOptions } from '@testing-library/react';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { ThemeProvider } from '@/components/theme-provider';
import type { ReactElement } from 'react';

function makeWrapper() {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return function Wrapper({ children }: { children: React.ReactNode }) {
    return (
      <QueryClientProvider client={qc}>
        <ThemeProvider>{children}</ThemeProvider>
      </QueryClientProvider>
    );
  };
}

export function renderWithProviders(ui: ReactElement, options?: RenderOptions) {
  return render(ui, { wrapper: makeWrapper(), ...options });
}
```

## Mocking fetch with MSW

```ts
// test/handlers.ts
import { http, HttpResponse } from 'msw';

export const handlers = [
  http.get('/api/users/:id', ({ params }) =>
    HttpResponse.json({ id: params.id, name: 'John' }),
  ),
];
```

```ts
// test/server.ts
import { setupServer } from 'msw/node';
import { handlers } from './handlers';

export const server = setupServer(...handlers);
```

`vitest.setup.ts`:
```ts
import { server } from './test/server';

beforeAll(() => server.listen({ onUnhandledRequest: 'error' }));
afterEach(() => server.resetHandlers());
afterAll(() => server.close());
```

```tsx
import { render, screen } from '@testing-library/react';
import { http, HttpResponse } from 'msw';
import { server } from '@/test/server';
import { UserProfile } from './user-profile';

it('shows user name', async () => {
  render(<UserProfile userId="123" />);
  expect(await screen.findByText('John')).toBeInTheDocument();
});

it('handles 500', async () => {
  server.use(
    http.get('/api/users/:id', () => new HttpResponse(null, { status: 500 })),
  );
  render(<UserProfile userId="123" />);
  expect(await screen.findByText(/error loading/i)).toBeInTheDocument();
});
```

## Hook tests with `renderHook`

```tsx
import { renderHook, act } from '@testing-library/react';
import { describe, it, expect, vi } from 'vitest';
import { useDebounce } from './use-debounce';

describe('useDebounce', () => {
  it('delays value', async () => {
    vi.useFakeTimers();
    const { result, rerender } = renderHook(
      ({ v }) => useDebounce(v, 500),
      { initialProps: { v: 'initial' } },
    );

    rerender({ v: 'updated' });
    expect(result.current).toBe('initial');

    await act(async () => { vi.advanceTimersByTime(500); });
    expect(result.current).toBe('updated');
    vi.useRealTimers();
  });
});
```

## Testing Server Components

Server Components run on the server. Test them by:

1. Extract pure logic into functions and unit-test those.
2. Test the rendered output by rendering inside a Server Component test harness (e.g., `renderToString`) or via integration tests with Playwright.
3. Mock `next/headers`, `next/cache`, `db` at the module boundary.

```tsx
// Test a Server Action's pure logic by mocking dependencies
import { describe, it, expect, vi } from 'vitest';
vi.mock('@/lib/db', () => ({ db: { post: { create: vi.fn() } } }));
vi.mock('@/lib/auth', () => ({ auth: vi.fn() }));
vi.mock('next/cache', () => ({ revalidatePath: vi.fn() }));
vi.mock('next/navigation', () => ({ redirect: vi.fn(() => { throw new Error('REDIRECT'); }) }));

import { createPost } from './actions';
import { db } from '@/lib/db';
import { auth } from '@/lib/auth';

it('creates post when authenticated', async () => {
  (auth as ReturnType<typeof vi.fn>).mockResolvedValue({ user: { id: 'u1' } });
  await createPost({ ok: false }, new FormData());
  expect(db.post.create).toHaveBeenCalledWith({ data: expect.objectContaining({ authorId: 'u1' }) });
});
```

## Playwright e2e

`playwright.config.ts`:
```ts
import { defineConfig, devices } from '@playwright/test';

export default defineConfig({
  testDir: './e2e',
  fullyParallel: true,
  webServer: { command: 'pnpm dev', url: 'http://localhost:3000', reuseExistingServer: !process.env.CI },
  use: { baseURL: 'http://localhost:3000', trace: 'on-first-retry' },
  projects: [
    { name: 'chromium', use: { ...devices['Desktop Chrome'] } },
    { name: 'webkit', use: { ...devices['Desktop Safari'] } },
    { name: 'mobile', use: { ...devices['iPhone 15'] } },
  ],
});
```

```ts
// e2e/smoke.spec.ts
import { test, expect } from '@playwright/test';

test('homepage loads and user can search', async ({ page }) => {
  await page.goto('/');
  await expect(page.getByRole('heading', { name: /welcome/i })).toBeVisible();
  await page.getByRole('searchbox').fill('react');
  await expect(page.getByText(/results for "react"/i)).toBeVisible();
});
```

## Quick Reference

| Test type | Tool | Run |
|-----------|------|-----|
| Component | RTL + Vitest | `pnpm vitest run` |
| Hook | `renderHook` + RTL | `pnpm vitest run` |
| Network mock | MSW | bundled with Vitest |
| Server Action logic | Vitest + `vi.mock` | `pnpm vitest run` |
| e2e | Playwright | `pnpm playwright test` |
| Visual regression | Playwright + `toHaveScreenshot` | `pnpm playwright test` |

| Pattern | When |
|---------|------|
| `userEvent.setup()` | Realistic clicks/types |
| `screen.findByX` | Wait for async content |
| `waitFor` | Multiple assertions after async |
| `vi.useFakeTimers()` | Time-based hooks |
| `server.use(handler)` | Per-test override |
| `cleanup()` | Auto via `afterEach` |

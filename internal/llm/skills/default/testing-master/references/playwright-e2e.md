# Playwright 1.45+ E2E

Playwright is the default recommendation for new E2E work in 2024+: cross-browser (Chromium, Firefox, WebKit), mobile emulation, auto-waiting, trace viewer, network mocking, and sharding — all built in. Cypress users should reach for Playwright when they hit parallelism limits or need cross-browser coverage.

## Project Setup

```typescript
// playwright.config.ts
import { defineConfig, devices } from '@playwright/test';

export default defineConfig({
  testDir: './tests',
  fullyParallel: true,
  forbidOnly: !!process.env.CI,
  retries: process.env.CI ? 2 : 0,
  workers: process.env.CI ? 1 : undefined,
  reporter: [
    ['html'],
    ['json', { outputFile: 'results.json' }],
    ['junit', { outputFile: 'results.xml' }],
  ],
  use: {
    baseURL: 'http://localhost:3000',
    trace: 'retain-on-failure',
    screenshot: 'only-on-failure',
    video: 'retain-on-failure',
    testIdAttribute: 'data-testid',
  },
  projects: [
    { name: 'chromium',  use: { ...devices['Desktop Chrome'] } },
    { name: 'firefox',   use: { ...devices['Desktop Firefox'] } },
    { name: 'webkit',    use: { ...devices['Desktop Safari'] } },
    { name: 'mobile-chrome', use: { ...devices['Pixel 5'] } },
    { name: 'mobile-safari', use: { ...devices['iPhone 13'] } },
  ],
  webServer: {
    command: 'npm run dev',
    url: 'http://localhost:3000',
    reuseExistingServer: !process.env.CI,
    timeout: 120_000,
  },
});
```

## Locators — Priority Order

```typescript
// 1. Role-based (BEST — accessible, resilient to refactor)
await page.getByRole('button', { name: 'Submit' }).click();
await page.getByRole('textbox', { name: 'Email' }).fill('a@x.com');
await page.getByRole('heading', { level: 1 });

// 2. Label / placeholder (good for forms)
await page.getByLabel('Email address').fill('a@x.com');
await page.getByPlaceholder('you@example.com').fill('a@x.com');

// 3. Test ID (good for non-semantic elements)
await page.getByTestId('user-avatar').click();

// 4. Text content
await page.getByText('Welcome back').waitFor();

// 5. CSS / XPath (AVOID — brittle, breaks on refactor)
await page.locator('.btn-primary.submit').click();   // last resort
```

### Filtering & chaining
```typescript
await page.getByRole('listitem')
  .filter({ hasText: 'Product A' })
  .getByRole('button', { name: 'Delete' })
  .click();

await page.getByRole('listitem').filter({
  hasNot: page.getByText('Sold out'),
});
```

### Multiple matches
```typescript
await page.getByRole('listitem').first().click();
await page.getByRole('listitem').nth(2).click();
const count = await page.getByRole('listitem').count();
for (const item of await page.getByRole('listitem').all()) {
  console.log(await item.textContent());
}
```

### Custom test ID attribute
```typescript
// playwright.config.ts
use: { testIdAttribute: 'data-test-id' }
// HTML: <button data-test-id="submit">Submit</button>
// Test: await page.getByTestId('submit').click();
```

## Page Object Model + Fixtures

```typescript
// pages/LoginPage.ts
import { type Page, type Locator } from '@playwright/test';

export class LoginPage {
  readonly emailInput: Locator;
  readonly passwordInput: Locator;
  readonly submitButton: Locator;
  readonly errorMessage: Locator;

  constructor(private readonly page: Page) {
    this.emailInput = page.getByLabel('Email');
    this.passwordInput = page.getByLabel('Password');
    this.submitButton = page.getByRole('button', { name: 'Sign in' });
    this.errorMessage = page.getByRole('alert');
  }

  async goto() { await this.page.goto('/login'); }
  async login(email: string, password: string) {
    await this.emailInput.fill(email);
    await this.passwordInput.fill(password);
    await this.submitButton.click();
  }
}
```

### Custom fixture (auth setup)
```typescript
// fixtures.ts
import { test as base, expect } from '@playwright/test';
import { LoginPage } from './pages/LoginPage';

export const test = base.extend<{ authenticatedPage: import('@playwright/test').Page }>({
  authenticatedPage: async ({ page }, use) => {
    const login = new LoginPage(page);
    await login.goto();
    await login.login('user@test.com', 'pass123!');
    await page.waitForURL(/dashboard/);
    await use(page);
  },
});
export { expect };
```

## Authentication — storageState (don't log in every test)

```typescript
// global-setup.ts
import { chromium, type FullConfig } from '@playwright/test';

async function globalSetup(config: FullConfig) {
  const browser = await chromium.launch();
  const page = await browser.newPage();
  await page.goto('http://localhost:3000/login');
  await page.getByLabel('Email').fill('user@test.com');
  await page.getByLabel('Password').fill('pass123!');
  await page.getByRole('button', { name: 'Sign in' }).click();
  await page.waitForURL(/dashboard/);
  await page.context().storageState({ path: 'auth.json' });
  await browser.close();
}
export default globalSetup;

// playwright.config.ts
export default defineConfig({
  globalSetup: './global-setup',
  use: { storageState: 'auth.json' },
});
```

For multi-role setups, save one `auth.json` per role and select per project.

## Network Mocking

### Mock a response
```typescript
test('displays mocked user data', async ({ page }) => {
  await page.route('**/api/users', (route) =>
    route.fulfill({
      status: 200,
      contentType: 'application/json',
      body: JSON.stringify([{ id: 1, name: 'Alice' }, { id: 2, name: 'Bob' }]),
    })
  );
  await page.goto('/users');
  await expect(page.getByText('Alice')).toBeVisible();
});
```

### Mock an error
```typescript
await page.route('**/api/users', (route) =>
  route.fulfill({ status: 500, json: { error: 'Server error' } })
);
```

### Conditional mocking — modify only some requests
```typescript
await page.route('**/api/**', async (route) => {
  const url = route.request().url();
  if (url.includes('/api/users')) {
    await route.fulfill({ status: 200, json: [{ id: 1, name: 'Mocked' }] });
    return;
  }
  await route.continue();   // let other requests hit the real server
});
```

### Modify a real response
```typescript
await page.route('**/api/products', async (route) => {
  const response = await route.fetch();
  const json = await response.json();
  json.products = json.products.map((p: any) => ({ ...p, price: p.price * 0.9 }));
  await route.fulfill({ json });
});
```

### Wait for a response
```typescript
const responsePromise = page.waitForResponse('**/api/users');
await page.getByRole('button', { name: 'Load users' }).click();
const response = await responsePromise;
expect(response.status()).toBe(200);
```

### HAR record / replay
```typescript
// Record once
await page.routeFromHAR('mocks/api.har', { url: '**/api/**', update: true });

// Replay on subsequent runs (deterministic, fast)
await page.routeFromHAR('mocks/api.har', { url: '**/api/**', update: false });
```

## Parallelism & Sharding

```typescript
// playwright.config.ts
export default defineConfig({
  workers: process.env.CI ? 8 : 4,
  fullyParallel: true,
  retries: process.env.CI ? 2 : 0,
});
```

```yaml
# GitHub Actions — distribute across 5 shards
strategy:
  matrix:
    shard: [1, 2, 3, 4, 5]
steps:
  - run: npx playwright test --shard=${{ matrix.shard }}/5
```

Each shard runs in parallel; merge reports with `npx playwright merge-reports`.

## Traces & Debugging

```typescript
// Enable trace recording
use: { trace: 'on-first-retry' }   // or 'retain-on-failure' / 'on'

// Pause in the middle of a test
await page.pause();                 // opens Playwright Inspector

// UI mode (interactive watch)
// $ npx playwright test --ui

// Headed mode
// $ npx playwright test --headed

// Slow motion
test.use({ launchOptions: { slowMo: 500 } });

// Capture console + page errors
test('debug', async ({ page }) => {
  page.on('console', (msg) => console.log(msg.text()));
  page.on('pageerror', (err) => console.log(err.message));
});
```

```bash
# Open a recorded trace
npx playwright show-trace test-results/.../trace.zip
```

The trace viewer shows: timeline of actions, DOM snapshots per action, network waterfall, console logs, screenshots, source code highlight — all in a single navigable UI.

## Flaky Test Fixes

| Cause | Symptom | Fix |
|---|---|---|
| Race condition | "element not found" intermittently | Use auto-waiting locators; never `waitForTimeout` |
| Animation / transition | Click lands on wrong element | Wait for stable state: `await expect(page.getByRole('menu')).toBeVisible()` |
| Network timing | Data not loaded when asserted | `await page.waitForResponse('**/api/user')` |
| Shared state between tests | Test B fails after Test A | Reset state in `beforeEach` via API; never rely on order |
| Time-based logic | Test fails at midnight | Inject a clock; use `page.clock.install({ now: ... })` |
| Hidden randomness | Different seed each run | Seed the generator; freeze it in test setup |

### NEVER use `waitForTimeout`
```typescript
// ❌ Flaky — arbitrary sleep
await page.waitForTimeout(2000);
await page.getByRole('button', { name: 'Save' }).click();

// ✅ Reliable — waits for element state
await page.getByRole('button', { name: 'Save' }).waitFor({ state: 'visible' });
await page.getByRole('button', { name: 'Save' }).click();

// ✅ Reliable — waits for response
await page.waitForResponse((r) => r.url().includes('/api/save') && r.ok());
```

### Stability verification
```bash
# Run a test 10x to confirm it's stable
npx playwright test tests/checkout.spec.ts --repeat-each=10
```

If a test fails under `--repeat-each=10`, it is flaky. Quarantine it (`test.fixme`) and fix the root cause — do not commit a flaky test.

## Visual Comparison Testing

```typescript
test('matches visual snapshot', async ({ page }) => {
  await page.goto('/dashboard');
  await expect(page).toHaveScreenshot('dashboard.png', {
    maxDiffPixelRatio: 0.01,   // tolerate ≤1% pixel diff
    animations: 'disabled',
  });
});
```

Update baselines after intentional UI changes:
```bash
npx playwright test --update-snapshots
```

Visual tests are slow and brittle — use them for stable, design-reviewed pages, not for every component. For component-level visual tests, use Playwright Component Testing or Storybook + Chromatic.

## CI Integration

```yaml
# .github/workflows/playwright.yml
name: E2E
on: [push, pull_request]
jobs:
  test:
    runs-on: ubuntu-latest
    strategy:
      matrix:
        shard: [1, 2, 3, 4]
    steps:
      - uses: actions/checkout@v4
      - uses: actions/setup-node@v4
      - run: npm ci
      - run: npx playwright install --with-deps
      - run: npx playwright test --shard=${{ matrix.shard }}/4
        env:
          CI: true
      - uses: actions/upload-artifact@v4
        if: always()
        with:
          name: report-${{ matrix.shard }}
          path: playwright-report/
```

## Quick Reference

| Need | Command / API |
|---|---|
| Role-based locator | `page.getByRole('button', { name: 'Save' })` |
| Form field | `page.getByLabel('Email')` |
| Test ID | `page.getByTestId('user-card')` |
| Wait for visible | `await expect(loc).toBeVisible()` |
| Wait for response | `await page.waitForResponse(url)` |
| Mock response | `page.route(url, r => r.fulfill({...}))` |
| HAR replay | `page.routeFromHAR('mocks/api.har')` |
| Pause + inspect | `await page.pause()` |
| Trace viewer | `npx playwright show-trace trace.zip` |
| UI mode | `npx playwright test --ui` |
| Headed | `npx playwright test --headed` |
| Stability check | `--repeat-each=10` |
| Sharding | `--shard=1/4` |
| Snapshot update | `--update-snapshots` |
| Auth state | `storageState: 'auth.json'` |

## MUST / MUST NOT

- MUST use role-based locators (`getByRole`, `getByLabel`) over CSS classes
- MUST use auto-waiting — never `waitForTimeout`
- MUST keep tests independent — no shared state, no test order dependence
- MUST enable traces/screenshots on failure for debugging
- MUST run in parallel (`fullyParallel: true`)
- MUST verify stability with `--repeat-each=10` before merging
- MUST NOT use CSS-class selectors when semantic locators exist
- MUST NOT share state between tests
- MUST NOT ignore flaky tests — quarantine and root-cause
- MUST NOT use `first()` / `nth()` without a comment explaining why

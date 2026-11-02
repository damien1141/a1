# Testing & Accessibility

Alpine components are DOM-bound — test them in a real browser. `@alpinejs/testing` for unit-level component tests, Playwright for end-to-end. Accessibility is not optional: keyboard nav, ARIA, focus trap, `prefers-reduced-motion`.

## `@alpinejs/testing` — unit-level component tests

```bash
npm install -D @alpinejs/testing jsdom
```

```js
import { expect, test, beforeEach } from 'vitest';
import Alpine from 'alpinejs';
import { AlpineTest, setupAlpine } from '@alpinejs/testing';
import focus from '@alpinejs/focus';

// Register plugins once
beforeEach(() => {
  global.document = new DOMParser().parseFromString('<!doctype html><html><body></body></html>', 'text/html');
  global.window = global.document.defaultView;
  setupAlpine(() => {
    Alpine.plugin(focus);
    Alpine.data('dropdown', () => ({
      open: false,
      toggle() { this.open = !this.open; },
      close() { this.open = false; },
    }));
  });
});

test('dropdown toggles open state', async () => {
  const component = new AlpineTest({
    template: `<div x-data="dropdown()">
      <button @click="toggle()"></button>
      <div x-show="open" x-cloak></div>
    </div>`,
  });
  await component.$nextTick();

  expect(component.$root.querySelector('[x-show]').style.display).toBe('none');
  component.$root.querySelector('button').click();
  await component.$nextTick();
  expect(component.data.open).toBe(true);
  expect(component.$root.querySelector('[x-show]').style.display).not.toBe('none');
});
```
- `$root` — the component's root DOM node.
- `component.data` — direct access to the reactive data object.
- `component.$nextTick()` — wait for reactive DOM update.
- Use `jsdom` (or `happy-dom`) for the DOM environment.

## Playwright — end-to-end

```bash
npm install -D @playwright/test
```

```js
// e2e/dropdown.spec.js
import { test, expect } from '@playwright/test';

test.beforeEach(async ({ page }) => {
  await page.goto('/');
});

test('dropdown opens on click and closes on Escape', async ({ page }) => {
  const button = page.getByRole('button', { name: 'Account' });
  await expect(button).toHaveAttribute('aria-expanded', 'false');

  await button.click();
  await expect(button).toHaveAttribute('aria-expanded', 'true');
  await expect(page.getByRole('menu')).toBeVisible();

  await page.keyboard.press('Escape');
  await expect(page.getByRole('menu')).toBeHidden();
  await expect(button).toHaveAttribute('aria-expanded', 'false');
});

test('dropdown closes on click outside', async ({ page }) => {
  await page.getByRole('button', { name: 'Account' }).click();
  await expect(page.getByRole('menu')).toBeVisible();

  await page.mouse.click(10, 10); // top-left corner — outside the menu
  await expect(page.getByRole('menu')).toBeHidden();
});
```

### Verifying `window.Alpine` is loaded
```js
test('Alpine is loaded', async ({ page }) => {
  const loaded = await page.evaluate(() => typeof window.Alpine !== 'undefined');
  expect(loaded).toBe(true);
});

test('no console errors', async ({ page }) => {
  const errors = [];
  page.on('pageerror', (e) => errors.push(e.message));
  await page.goto('/');
  await page.waitForLoadState('networkidle');
  expect(errors).toEqual([]);
});
```

### Verifying no FOUC
```js
test('x-cloak prevents FOUC', async ({ page }) => {
  // Slow down CPU to surface FOUC
  await page.route('**/*', (route) => setTimeout(() => route.continue(), 100));
  await page.goto('/');
  // Element with x-cloak should never be visible until Alpine has initialized
  const hidden = await page.locator('[x-cloak]').first().isVisible();
  // Right after page load, before Alpine init: should be hidden
  // (Playwright may catch either state — assert the CSS rule exists)
  const cssPresent = await page.evaluate(() => {
    const styles = [...document.styleSheets].flatMap((s) => [...s.cssRules]);
    return styles.some((r) => r.cssText && r.cssText.includes('[x-cloak]'));
  });
  expect(cssPresent).toBe(true);
});
```

## Accessibility checklist

For every interactive Alpine component, verify:

### 1. Keyboard navigation
- **Tab** moves between focusable elements in a sensible order.
- **Shift+Tab** moves backward.
- **Enter**/**Space** activates buttons/links.
- **Escape** closes modals/dropdowns/popovers.
- **Arrow keys** navigate tabs, listboxes, menus, sliders.
- **Home**/**End** jump to first/last item in a list.
```js
test('tabs: arrow keys move between tabs', async ({ page }) => {
  await page.getByRole('tab', { name: 'Overview' }).click();
  await page.keyboard.press('ArrowRight');
  await expect(page.getByRole('tab', { name: 'Details' })).toBeFocused();
  await expect(page.getByRole('tab', { name: 'Details' })).toHaveAttribute('aria-selected', 'true');
});
```

### 2. ARIA roles and states
- `:aria-expanded` bound to Alpine state (never hardcoded).
- `:aria-selected` for tabs/options.
- `:aria-controls`/`aria-labelledby` for paired elements.
- `role="dialog"` + `aria-modal="true"` for modals.
- `role="menu"`/`menuitem` for dropdowns.
- `aria-live="polite"` for toasts and async notifications.
- `aria-invalid` + `aria-describedby` for form errors.
```js
test('modal has correct ARIA', async ({ page }) => {
  await page.getByRole('button', { name: 'Open' }).click();
  const dialog = page.getByRole('dialog');
  await expect(dialog).toHaveAttribute('aria-modal', 'true');
  await expect(dialog).toHaveAttribute('aria-labelledby', 'modal-title');
  await expect(dialog).toBeFocused();
});
```

### 3. Focus management
- Modal: focus moves to first focusable on open, returns to trigger on close.
- Dropdown: focus returns to trigger on close.
- Toast: focus should NOT move (announced via `aria-live`).
- Route changes (single-page): move focus to `<h1>` and announce.
```js
test('modal focus trap', async ({ page }) => {
  const trigger = page.getByRole('button', { name: 'Open' });
  await trigger.click();
  const cancel = page.getByRole('button', { name: 'Cancel' });
  await expect(cancel).toBeFocused();

  await page.keyboard.press('Tab');
  await expect(page.getByRole('button', { name: 'Delete' })).toBeFocused();

  await page.keyboard.press('Tab'); // wraps back
  await expect(cancel).toBeFocused();

  await page.keyboard.press('Escape');
  await expect(trigger).toBeFocused(); // focus returned
});
```

### 4. `prefers-reduced-motion`
```js
test('reduced motion disables transitions', async ({ page }) => {
  await page.emulateMedia({ reducedMotion: 'reduce' });
  await page.goto('/');
  const duration = await page.locator('[x-show]').first().evaluate((el) =>
    getComputedStyle(el).transitionDuration,
  );
  expect(duration).toMatch(/0s|0.001s/);
});
```

### 5. Screen reader announcements
Playwright cannot run a screen reader, so verify the structure is correct:
- `aria-live` regions exist for dynamic content.
- `role="status"` for non-urgent updates.
- `role="alert"` for urgent errors.
- Visual-only cues (icons, color) have text alternatives.
Mark live-region announcement behavior as **ASSUMED** unless tested with NVDA/VoiceOver.

## ESLint config — allow Alpine globals

```js
// eslint.config.js
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
        Alpine: 'readonly',
      },
    },
    rules: {
      'no-unused-vars': ['error', { argsIgnorePattern: '^_' }],
      // Inline event handlers like @click="open = false" are JS-in-HTML; lint via a plugin
      // if you want, but most projects rely on Playwright to catch handler bugs.
    },
  },
  {
    files: ['**/*.astro'],
    languageOptions: { parser: 'astro-eslint-parser' },
  },
];
```
The `Alpine` global lets you write `Alpine.store(...)` in inline scripts without lint errors. `$dispatch`, `$store`, etc. are template-only — they don't need globals.

## Verification block (final)

```
$ eslint .                                       # Alpine globals allowed
$ playwright test --project=chromium
$ playwright test --project=firefox
✓ window.Alpine defined | 0 console errors | no FOUC
✓ 24 e2e passed | a11y: focus trap OK, keyboard nav OK, ARIA correct
✓ prefers-reduced-motion: transitions disabled
ASSUMED: NVDA/VoiceOver live-region announcement behavior (not run)
```

## Quick reference

| Need | Tool |
|---|---|
| Unit-level component | `@alpinejs/testing` + Vitest + jsdom |
| E2E | Playwright (chromium + firefox + webkit) |
| Verify Alpine loaded | `await page.evaluate(() => typeof window.Alpine)` |
| Verify no FOUC | assert `[x-cloak]` CSS rule exists |
| Keyboard nav | `page.keyboard.press('Tab' / 'Escape' / 'ArrowRight')` |
| ARIA | `expect(locator).toHaveAttribute('aria-expanded', ...)` |
| Focus trap | `expect(locator).toBeFocused()` after Tab cycles |
| Reduced motion | `page.emulateMedia({ reducedMotion: 'reduce' })` |
| Screen reader | Manual NVDA/VoiceOver — mark ASSUMED in report |
| Lint Alpine globals | `globals: { Alpine: 'readonly' }` in ESLint config |

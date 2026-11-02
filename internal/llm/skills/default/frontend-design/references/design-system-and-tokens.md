# Design System & Tokens (Tailwind 4)

Design tokens are the single source of truth for color, typography, spacing, radius, shadow. Define them as CSS variables in `:root`, then map them to Tailwind 4's `@theme` (or Tailwind 3's `theme.extend`).

## Why tokens

- One change (e.g., `--color-accent`) updates every consumer.
- Theme switching = swap a CSS class, not a code change.
- Tooling (Figma, Style Dictionary, Tailwind) can all read the same source.
- Contrast guarantees are documented next to the value, not discovered in QA.

## Token anatomy

| Token type | Example | Purpose |
|------------|---------|---------|
| Color (semantic) | `--color-bg`, `--color-text-main`, `--color-accent` | Role-based, not raw hex |
| Color (raw, optional) | `--blue-500`, `--gray-900` | Palette primitives |
| Typography | `--font-sans`, `--text-display`, `--text-body` | Font family + size + line-height + tracking |
| Spacing | `--space-1` … `--space-16` (4px scale) | Padding, margin, gap |
| Radius | `--radius-sm`, `--radius-md`, `--radius-lg` | Border radius |
| Shadow | `--shadow-card`, `--shadow-float` | Elevation |
| Z-index | `--z-nav`, `--z-modal`, `--z-toast` | Layering |

Use **OKLCH** for color — perceptually uniform, better contrast math than HSL/RGB.

## Tailwind 4 `@theme` (CSS-first config)

Tailwind 4 reads CSS variables declared inside `@theme` and generates utilities for them.

```css
/* globals.css */
@import "tailwindcss";

@theme {
  /* Color — semantic roles */
  --color-bg: oklch(0.98 0.01 80);
  --color-surface: oklch(1 0 0);
  --color-surface-elevated: oklch(0.96 0.01 80);
  --color-surface-muted: oklch(0.94 0.01 80);
  --color-text-main: oklch(0.18 0.02 80);          /* AA on bg */
  --color-text-muted: oklch(0.42 0.02 80);          /* AA on bg */
  --color-accent: oklch(0.55 0.18 220);
  --color-accent-contrast: oklch(0.98 0.01 220);    /* text on accent */

  /* Typography */
  --font-sans: "Inter", system-ui, -apple-system, sans-serif;
  --font-mono: "JetBrains Mono", ui-monospace, monospace;

  /* Tailwind 4 generates text-display, text-display-sm, etc. */
  --text-display: 3.5rem;
  --text-display--line-height: 1.05;
  --text-display--letter-spacing: -0.02em;
  --text-display--font-weight: 600;

  --text-h1: 2.5rem;
  --text-h1--line-height: 1.1;
  --text-h1--letter-spacing: -0.01em;

  --text-body: 1rem;
  --text-body--line-height: 1.6;

  --text-small: 0.875rem;
  --text-small--line-height: 1.5;

  /* Radius */
  --radius-sm: 0.25rem;
  --radius-md: 0.5rem;
  --radius-lg: 1rem;
  --radius-pill: 9999px;

  /* Shadow */
  --shadow-card: 0 1px 3px rgb(0 0 0 / 0.08), 0 1px 2px rgb(0 0 0 / 0.04);
  --shadow-float: 0 10px 30px rgb(0 0 0 / 0.12);
}
```

Usage in markup:
```html
<section class="bg-bg text-text-main">
  <h1 class="font-sans text-display">Asymmetric headline</h1>
  <p class="text-text-muted text-body max-w-prose">Subtext.</p>
  <button class="rounded-md bg-accent text-accent-contrast px-6 py-3">Get a quote</button>
</section>
```

## Tailwind 3 `theme.extend` (legacy projects)

```js
// tailwind.config.js
export default {
  content: ['./src/**/*.{astro,html,js,jsx,ts,tsx,vue}'],
  theme: {
    extend: {
      colors: {
        bg: 'var(--color-bg)',
        surface: 'var(--color-surface)',
        'text-main': 'var(--color-text-main)',
        'text-muted': 'var(--color-text-muted)',
        accent: 'var(--color-accent)',
      },
      fontFamily: {
        sans: ['Inter', 'system-ui', 'sans-serif'],
        mono: ['JetBrains Mono', 'ui-monospace', 'monospace'],
      },
      fontSize: {
        display: ['3.5rem', { lineHeight: '1.05', letterSpacing: '-0.02em' }],
        body: ['1rem', { lineHeight: '1.6' }],
      },
      borderRadius: { sm: '0.25rem', md: '0.5rem', lg: '1rem' },
    },
  },
};
```

```css
/* globals.css */
:root {
  --color-bg: oklch(0.98 0.01 80);
  --color-surface: oklch(1 0 0);
  --color-text-main: oklch(0.18 0.02 80);
  --color-text-muted: oklch(0.42 0.02 80);
  --color-accent: oklch(0.55 0.18 220);
}

[data-theme="dark"] {
  --color-bg: oklch(0.15 0.01 80);
  --color-surface: oklch(0.18 0.01 80);
  --color-text-main: oklch(0.96 0.01 80);
  --color-text-muted: oklch(0.70 0.02 80);
  --color-accent: oklch(0.70 0.18 220);
}
```

## Type scale

Use a modular scale. The ratio determines density:

| Token | Size | Line height | Tracking | Use |
|-------|------|-------------|----------|-----|
| `text-display` | 3.5rem (56px) | 1.05 | -0.02em | Hero headline |
| `text-h1` | 2.5rem (40px) | 1.1 | -0.01em | Page H1 |
| `text-h2` | 2rem (32px) | 1.15 | -0.01em | Section H2 |
| `text-h3` | 1.5rem (24px) | 1.2 | 0 | Card title |
| `text-body` | 1rem (16px) | 1.6 | 0 | Body |
| `text-small` | 0.875rem (14px) | 1.5 | 0 | Captions, labels |

**Hero headline ≤ 2 lines. Subtext ≤ 20 words. CTA visible without scroll.**

```html
<h1 class="text-balance text-display font-sans">Harrison Roofing replaces roofs in Leeds.</h1>
<p class="text-balance text-body text-text-muted max-w-prose">Quotes in 48 hours. Most jobs done in two days.</p>
```

`text-balance` (CSS `text-wrap: balance`) prevents orphans in headlines. `tabular-nums` on stats and prices:

```html
<dl class="grid grid-cols-3 gap-4 font-mono tabular-nums">
  <dt>Lead time</dt><dd>2 days</dd>
  <dt>Warranty</dt><dd>10 years</dd>
  <dt>Service area</dt><dd>Leeds + 25mi</dd>
</dl>
```

## Spacing scale (4px base)

```css
--space-0: 0;
--space-1: 0.25rem;   /* 4px */
--space-2: 0.5rem;    /* 8px */
--space-3: 0.75rem;   /* 12px */
--space-4: 1rem;      /* 16px */
--space-6: 1.5rem;    /* 24px */
--space-8: 2rem;      /* 32px */
--space-12: 3rem;     /* 48px */
--space-16: 4rem;     /* 64px */
--space-24: 6rem;     /* 96px */
--space-32: 8rem;     /* 128px */
```

Section padding: `py-24 md:py-32` (6–8rem). Hero exception: `pt-16 md:pt-24 pb-24 md:pb-32`. Container: `max-w-6xl mx-auto px-6 md:px-8 lg:px-12`.

## Theme switching

Single source of truth = swap a class on `<html>`:

```html
<html class="" data-theme="light">
<html data-theme="dark">
```

Tailwind 4 (no `dark:` variants needed — tokens re-bind):
```css
[data-theme="dark"] {
  --color-bg: oklch(0.15 0.01 80);
  --color-text-main: oklch(0.96 0.01 80);
  /* ... */
}
```

Tailwind 3 with `darkMode: 'class'`:
```html
<html class="dark">
```

**Pick one theme per surface** — committing to light OR dark is cleaner than `dark:` everywhere. The "Precision Tech" archetype is dark-only; everything else defaults light.

## Contrast verification

```bash
# Quick smoke check with @axe-core/cli
pnpm dlx @axe-core/cli http://localhost:3000 --tags wcag2aa

# Or programmatically in a test
import { expect } from '@playwright/test';
await expect(page).toPassAxe({ rules: { 'color-contrast': { enabled: true } } });
```

Token contrast rules:
- Body text on bg: ≥ 4.5:1 (WCAG AA).
- Large text (≥ 24px or ≥ 18.66px bold) on bg: ≥ 3:1.
- UI components (input borders, icon boundaries) on adjacent: ≥ 3:1.
- Accent button text on accent bg: ≥ 4.5:1. If accent is light, button text MUST be dark — white-on-light fails AA.

## Quick Reference

| Token category | Examples |
|----------------|----------|
| Color (semantic) | `--color-bg`, `--color-surface`, `--color-text-main`, `--color-text-muted`, `--color-accent` |
| Color (state) | `--color-success`, `--color-warning`, `--color-error` |
| Typography | `--font-sans`, `--font-mono`, `--text-display`, `--text-body` |
| Spacing | `--space-1` … `--space-32` (4px base) |
| Radius | `--radius-sm/md/lg/pill` |
| Shadow | `--shadow-card`, `--shadow-float` |
| Z-index | `--z-base/-nav/-modal/-toast` (10, 20, 40, 50) |

| Rule | Why |
|------|-----|
| One change, many consumers | Tokens are indirection |
| OKLCH over HSL | Perceptually uniform |
| `text-balance` on headings | No orphans |
| `tabular-nums` on stats | Alignment |
| Commit to one theme per surface | Avoids `dark:` sprawl |
| Document contrast next to token | Catches regressions |

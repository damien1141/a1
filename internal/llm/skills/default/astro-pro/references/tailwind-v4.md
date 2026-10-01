# Tailwind v4 in Astro

Tailwind v4 is CSS-first: no `tailwind.config.js` (for new projects), no `@tailwind base/components/utilities` directives. Configure via `@theme` in CSS. Use the `@tailwindcss/vite` plugin directly — no `@astrojs/tailwind` integration.

## Install

```bash
npm install tailwindcss @tailwindcss/vite
```

```ts
// astro.config.mjs
import tailwindcss from '@tailwindcss/vite';
import { defineConfig } from 'astro/config';

export default defineConfig({
  vite: { plugins: [tailwindcss()] },
});
```

```css
/* src/styles/global.css */
@import "tailwindcss";
```

```astro
---
// src/layouts/Layout.astro
import '../styles/global.css';
---
```

That's the baseline. No `tailwind.config.mjs`, no `content` array (Tailwind v4 auto-detects).

## `@theme` — Design Tokens

`@theme` declares design tokens that become Tailwind utilities. Each token maps to a utility prefix:

```css
@import "tailwindcss";

@theme {
  /* Colors → bg-*, text-*, border-*, ring-*, fill-*, stroke-* */
  --color-surface: oklch(0.98 0.01 80);
  --color-surface-elevated: oklch(1 0 0);
  --color-surface-muted: oklch(0.94 0.01 80);
  --color-text-main: oklch(0.18 0.02 80);
  --color-text-muted: oklch(0.42 0.02 80);
  --color-accent: oklch(0.55 0.18 220);
  --color-accent-muted: oklch(0.45 0.14 220);

  /* Fonts → font-heading, font-mono, font-sans (default) */
  --font-sans: "Inter", system-ui, sans-serif;
  --font-heading: "Inter", system-ui, sans-serif;
  --font-mono: "JetBrains Mono", ui-monospace, monospace;

  /* Type scale → text-xs, text-sm, ..., text-7xl (overrides defaults) */
  --text-display: 3.5rem;
  --text-display--line-height: 1.05;
  --text-body: 1rem;
  --text-body--line-height: 1.6;

  /* Spacing → p-*, m-*, gap-*, w-*, h-* (extends default 0-96 scale) */
  --spacing-128: 32rem;

  /* Radii → rounded-* (extends default scale) */
  --radius-stamp: 0;          /* Industrial */
  --radius-luxury: 2rem;      /* Luxury */

  /* Shadows → shadow-* */
  --shadow-stamp: 8px 8px 0px 0px rgba(24, 24, 27, 1);
  --shadow-glow: 0 0 40px -10px rgba(59, 130, 246, 0.5);

  /* Container → container utility */
  --breakpoint-3xl: 100rem;
}
```

Usage:

```astro
<section class="bg-surface text-text-main font-heading text-display rounded-luxury shadow-stamp">
  <h1 class="text-balance">Hello</h1>
</section>
```

## Token Naming Convention (archetype-friendly)

The high-end-visual-design templates use **structural tokens** — semantic names that don't lock you to a color. Map them per archetype:

```css
@theme {
  /* Structural tokens (used by all templates) */
  --color-surface: /* archetype bg */;
  --color-surface-elevated: /* archetype elevated bg */;
  --color-surface-muted: /* archetype muted bg */;
  --color-text-main: /* archetype main text */;
  --color-text-muted: /* archetype muted text */;
  --color-accent: /* archetype accent */;
  --color-accent-muted: /* archetype muted accent */;

  --font-heading: /* archetype heading font */;
  --font-mono: /* archetype mono font */;

  --radius-stamp: 0;          /* Industrial */
  --radius-luxury: 2rem;      /* Luxury */
  --radius-clinical: 0.75rem; /* Clinical */
  --radius-tech: 0.75rem;     /* Tech */

  --shadow-stamp: 8px 8px 0px 0px rgba(24,24,27,1);
  --shadow-layered: 0 4px 24px -8px rgba(0,0,0,0.12);
  --shadow-glow: 0 0 40px -10px rgba(59,130,246,0.5);
}
```

This way, swapping archetypes means swapping the `@theme` block — templates don't change.

## `@layer` — Custom Styles

Tailwind v4 keeps the cascade layers: `theme`, `base`, `components`, `utilities`. Add custom styles:

```css
@layer base {
  body {
    background-color: var(--color-surface);
    color: var(--color-text-main);
    font-family: var(--font-sans);
  }
  h1, h2, h3 { font-family: var(--font-heading); }
  :focus-visible { outline: 2px solid var(--color-accent); outline-offset: 2px; }
}

@layer components {
  .btn-primary {
    /* composes utilities */
    @apply inline-flex items-center px-6 py-3 font-heading font-bold uppercase tracking-widest text-sm;
  }
}

@layer utilities {
  .text-balance { text-wrap: balance; }
  .tabular-nums { font-variant-numeric: tabular-nums; }
}
```

## `@utility` — Custom Utility

v4 replaces `plugin/addUtilities` with `@utility`:

```css
@utility tabular-nums {
  font-variant-numeric: tabular-nums;
}

@utility content-visibility-auto {
  content-visibility: auto;
  contain-intrinsic-size: auto 500px;
}
```

Usage: `<div class="tabular-nums content-visibility-auto">`.

## Archetype Texture SVGs (JIT-safe)

Background textures must be URL-encoded (no base64 — Astro's image pipeline doesn't optimize CSS background images, and base64 bloats the CSS):

```css
@layer utilities {
  .texture-blueprint-grid {
    background-image: url("data:image/svg+xml,%3Csvg xmlns='http://www.w3.org/2000/svg' width='40' height='40' viewBox='0 0 40 40'%3E%3Cpath d='M40 0H0V40' fill='none' stroke='%23000' stroke-opacity='0.08' stroke-width='1'/%3E%3C/svg%3E");
  }

  .texture-micro-dot-grid {
    background-image: url("data:image/svg+xml,%3Csvg xmlns='http://www.w3.org/2000/svg' width='24' height='24'%3E%3Ccircle cx='12' cy='12' r='1' fill='%230F172A' fill-opacity='0.05'/%3E%3C/svg%3E");
  }
}
```

See `templates/svg-textures.md` for the 25 archetype textures.

## Reduced Motion (mandatory)

```css
@media (prefers-reduced-motion: reduce) {
  *, *::before, *::after {
    animation-duration: 0.01ms !important;
    animation-iteration-count: 1 !important;
    transition-duration: 0.01ms !important;
    scroll-behavior: auto !important;
  }
}
```

## Dark Mode

Tailwind v4 uses `@custom-variant dark` for dark mode. For archetype-based design (single mode committed, no `dark:` variants), do NOT add the dark variant — instead, swap the `@theme` block per archetype.

If you genuinely need dark mode (user preference toggle):

```css
@import "tailwindcss";
@custom-variant dark (&:where(.dark, .dark *));

@theme {
  --color-bg: oklch(0.98 0.01 80);
  --color-text: oklch(0.18 0.02 80);
}

.dark {
  --color-bg: oklch(0.15 0.01 80);
  --color-text: oklch(0.92 0.01 80);
}
```

But the high-end-visual-design system commits to ONE mode per archetype — `dark:` variants are an instant-fail anti-pattern.

## Bridging v3 Projects

If migrating from v3 to v4 incrementally:

```bash
# install both — v4 supports v3 config via @tailwindcss/upgrade
npx @tailwindcss/upgrade
```

The upgrade tool migrates `tailwind.config.mjs` → `@theme` in CSS, `@tailwind base/components/utilities` → `@import "tailwindcss"`, and `theme.extend` → `@theme`.

Do NOT mix v3 and v4 syntax in one project. Pick one. The high-end-visual-design templates assume v3 arbitrary values (e.g., `text-[0.95rem]`, `shadow-[8px_8px_0px_0px_rgba(24,24,27,1)]`) which work identically in v4 — they're a portable pattern.

## Verification

```bash
astro build        # CSS compiles, no @theme errors
rg 'tailwind.config' .   # should be empty for v4 projects (or only legacy bridge)
rg '@tailwind base|@tailwind components|@tailwind utilities' src/   # v3 syntax, banned in v4
rg 'dark:' src/    # if archetype mode committed, should be empty
```

## Common Gotchas

- **No `tailwind.config.js` in v4** — config goes in CSS via `@theme`. Existing v3 plugins need migration.
- **`@apply` still works** in v4 — use sparingly in `@layer components` for composition.
- **Color opacity modifiers** (`bg-accent/40`) require the token to be in a color format Tailwind can interpolate — `oklch`, `rgb`, `hsl`. Hex works too but oklch is preferred.
- **`@theme` is static** — values must be literal at build time. For runtime theme switching, override CSS variables in a separate `:root` block (without `@theme`).
- **Container query support is built-in** — `@container` and `@sm:` etc. work without plugin.
- **`content` array is gone** — v4 auto-detects from `node_modules` + project files. If you need to exclude, use `@source inline()` or `@source not`.

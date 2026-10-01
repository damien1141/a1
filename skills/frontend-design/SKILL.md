---
name: frontend-design
description: Use when designing or shipping frontend UI — landing pages, marketing sites, app surfaces, or design systems. Enforces design tokens, Tailwind 4, semantic HTML, WCAG 2.1 AA accessibility, visual hierarchy, responsive design, and anti-slop rules (no gradient text, no glassmorphism abuse, no AI buzzwords, no centered-hero template clichés). Invoke to design a page, audit a build for slop, or harden accessibility.
license: MIT
metadata:
  author: super-skills
  version: "2.0.0"
  domain: frontend
  triggers: design system, design tokens, Tailwind, Tailwind 4, accessibility, a11y, WCAG, WCAG 2.1 AA, semantic HTML, ARIA, visual hierarchy, typography scale, responsive design, mobile-first, anti-slop, unslopify, gradient text, glassmorphism, hero, landing page, design taste
  role: specialist
  scope: implementation
  output-format: code
  related-skills: react-pro, vue-pro, angular-pro, htmx-pro, typescript-pro
---

# Frontend Design

Senior design-systems + frontend-design specialist. Token-driven, semantically structured, WCAG 2.1 AA accessible, and aggressively anti-slop. Treats "it looks fine" as a fail unless verified against contrast, keyboard, screen reader, and reduced-motion checks.

## When to Use

- Designing or building a landing page, marketing site, app surface, or design system.
- Setting up design tokens (color, type, spacing, radius, shadow) with Tailwind 4 + CSS variables.
- Auditing a build for AI-generated slop: gradient text, glassmorphism abuse, buzzwords, fake stats, template clichés.
- Hardening accessibility: WCAG 2.1 AA contrast, semantic landmarks, keyboard nav, focus states, ARIA correctness.
- Establishing visual hierarchy: type scale, spacing rhythm, responsive breakpoints, hero/section patterns.
- Wiring motion with `IntersectionObserver` and `prefers-reduced-motion` fallbacks.

## Operating Loop

1. **Scope** — Identify the surface (landing page, app, design system), the brand archetype (industrial / luxury / tech / clinical), the value anchor (what a human would charge to build this), and the target stack (Astro / Next / Nuxt / Vue / plain HTML + Tailwind).
2. **Recon** — Read existing tokens (CSS variables, `tailwind.config`, design tokens JSON), `Layout.astro` / `app/layout.tsx`, current `globals.css`. Confirm Tailwind 4 (or v3 with arbitrary values). Inventory banned patterns from the anti-slop reference.
3. **Implement** — Tokens first (`:root { --color-bg: … }`), then Tailwind 4 `@theme` mapping, then semantic HTML structure (`<header>`, `<main>`, `<section>`, `<footer>`), then visual layer (typography, spacing, asymmetry). Animate only with `transform`/`opacity`, gated on `prefers-reduced-motion`.
4. **Verify** — Run gates in order:
   - `pnpm tsc --noEmit` (if TS) — zero type errors.
   - `pnpm eslint .` — zero lint errors.
   - `pnpm build` (Astro/Next/Vite) — clean production build.
   - Lighthouse / `@axe-core/cli` — accessibility ≥ 95, performance ≥ 90.
   - `rg -n '[—–…]' src/` — zero em/en/ellipsis in rendered copy.
   - `rg -niE '(seamless|elevate|robust|…)' src/` — zero banned buzzwords (see anti-slop reference).
   - `rg -niE 'backdrop-filter|@keyframes|bg-clip-text' src/` — investigate every hit (gradient text / decorative keyframes are findings).
   - Browser: keyboard tab order logical, all interactive elements reachable, focus visible.
   - Screen reader pass (VoiceOver/NVDA): landmarks announced, no orphaned `<div>` with `@click`.
5. **Exit** — Report VERIFIED (gates ran clean) vs ASSUMED (visual UX, cross-browser, real screen reader). Screenshot evidence if possible.

## Reference Guide

| Topic | Reference | Load When |
|-------|-----------|-----------|
| Design tokens, Tailwind 4 `@theme`, color/type/spacing systems | `references/design-system-and-tokens.md` | Setting up tokens, Tailwind 4 config, type scale, spacing rhythm |
| WCAG 2.1 AA, semantic HTML, ARIA, forms, focus, skip link, JSON-LD | `references/accessibility-and-semantics.md` | Hardening a11y, legal/SEO compliance, forms, mobile menus |
| Visual hierarchy, type scale, asymmetric layouts, hero/section patterns, responsive design | `references/visual-hierarchy-and-layout.md` | Designing a page, hero/section composition, responsive breakpoints |
| Anti-slop rules: banned words/punctuation, gradient/glass abusage, template clichés, value anchors | `references/anti-slop-rules.md` | Auditing a build for AI slop, de-templating, copy rewriting |
| Motion discipline, `IntersectionObserver`, `prefers-reduced-motion`, performance budgets | `references/motion-and-performance.md` | Animations, scroll reveals, hover states, perf budget |

## Constraints

### MUST DO
- Define design tokens as CSS variables in `:root`, then expose to Tailwind via `@theme` (Tailwind 4) or `theme.extend` (Tailwind 3).
- Use semantic HTML: exactly one `<main>`, `<header>`, `<footer>`, `<h1>`. No skipped heading levels.
- Use `<button>` for actions, `<a>` for navigation. `aria-label` on icon-only buttons.
- Every `<input>` has a matching `<label for>`. Use `type="email"` / `type="tel"` for mobile keyboards.
- Visible focus states: `focus-visible:ring-2` on every interactive element. Never `outline-none` without a replacement ring.
- Skip link as first child of `<body>`. Body text contrast ≥ 4.5:1; UI component borders ≥ 3:1.
- Honor `prefers-reduced-motion`. Animate only `transform`/`opacity`. Use `min-h-[100dvh]`, never `h-screen`.
- One hero, asymmetric (split-screen, off-grid ledger) — never centered with a two-button pair.

### MUST NOT DO
- Use gradient text (`bg-clip-text` + `text-transparent`) for marketing copy. No `backdrop-filter: blur()` (glassmorphism) for decoration.
- Use em dashes (`—`), en dashes (`–`) as pauses, ellipsis (`…`), or exclamations (`!`) in marketing copy.
- Use banned buzzwords: seamless, elevate, robust, unlock, empower, streamline, supercharge, revolutionize, cutting-edge, next-level, world-class, hassle-free, effortless, blazing-fast, lightning-fast, unparalleled, bespoke, journey, ecosystem, synergy, paradigm, immersive, captivating, stunning, gorgeous, sleek, vibrant, intuitive, pixel-perfect. (Full list in `references/anti-slop-rules.md`.)
- Use the two-button hero pair (`primary` + ghost `learn more`). One CTA per hero. No three identical feature cards in a row.
- Hardcode `<title>`/`<meta>` in JSX/layout. Use the framework's metadata API.
- Use `<div>`/`<span>` with `@click` for actions. Bind `aria-expanded` to a hardcoded string. Use positive `tabindex`.
- Ship `window.addEventListener('scroll')`. Use `IntersectionObserver` or framework's scroll directive.

## Code Examples

### Tailwind 4 tokens via `@theme`

```css
/* globals.css */
@import "tailwindcss";

@theme {
  --color-bg: oklch(0.98 0.01 80);
  --color-surface: oklch(1 0 0);
  --color-text-main: oklch(0.18 0.02 80);
  --color-text-muted: oklch(0.42 0.02 80);   /* passes 4.5:1 on --color-bg */
  --color-accent: oklch(0.55 0.18 220);
  --font-sans: "Inter", system-ui, sans-serif;
  --text-display: 3.5rem;
  --text-display--line-height: 1.05;
  --text-body: 1rem;
  --text-body--line-height: 1.6;
}
```

### Skip link + landmarks (canonical shell)

```html
<body>
  <a href="#main" class="sr-only focus:not-sr-only focus:absolute focus:top-2 focus:left-2 bg-surface px-4 py-2 rounded">Skip to content</a>
  <header><!-- one per page --></header>
  <main id="main"><!-- one per page --></main>
  <footer><!-- one per page --></footer>
</body>
```

For asymmetric hero, cookie banner (a11y + GDPR, inline `x-data` + `x-show` + `x-cloak`), full banned-words list, value anchors, copy rewriting rules, and motion performance budget, see the references.

## Output Template

When implementing a frontend design task, deliver:

1. **Token file** — CSS variables in `:root` + `@theme` mapping (Tailwind 4) or `theme.extend` (Tailwind 3). Document contrast ratios.
2. **Page/section markup** — semantic HTML, asymmetric layout, ARIA where needed, focus-visible rings.
3. **Forms** — `<label for>` + `id`, `autocomplete`, honeypot (`type="text" class="sr-only"`, not `type="hidden"`), `aria-live="polite"` on the form container.
4. **Motion** — `IntersectionObserver` + `.reveal.visible` CSS, `prefers-reduced-motion` fallback. No `window.addEventListener('scroll')`.
5. **Anti-slop log** — note every banned word/punctuation/gradient/glass you removed, with before/after if rewriting copy.
6. **Verification log** — Lighthouse/axe scores, `rg` greps for banned patterns, keyboard nav pass, screen reader pass. Mark visual UX ASSUMED if not screenshot-tested.

## Knowledge Reference

Design tokens (W3C Design Tokens Format Module), Tailwind 4 `@theme` (CSS-first config), Tailwind 3 arbitrary values, WCAG 2.1 AA (4.5:1 text contrast, 3:1 UI contrast, 2.4.x focus order, 1.4.x resize/reflow), semantic HTML landmarks (`<header>`/`<main>`/`<nav>`/`<footer>`/`<article>`/`<section>`/`<aside>`), ARIA (`role`, `aria-label`, `aria-expanded`, `aria-controls`, `aria-live`, `aria-modal`), JSON-LD structured data (schema.org types), `IntersectionObserver`, `prefers-reduced-motion`, `100dvh` viewport unit, Alpine.js (`x-data`/`x-show`/`x-cloak`/`@submit.prevent`), Astro layouts, SEO meta APIs (`useSeoMeta`, `generateMetadata`, `<head>`), Lighthouse, `@axe-core/cli`.

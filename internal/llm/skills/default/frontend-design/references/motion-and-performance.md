# Motion & Performance

Motion must be motivated. If an animation doesn't communicate hierarchy or state transition, delete it. Performance budgets are real budgets.

## Motion discipline

### Rules

1. **Animate only `transform` and `opacity`.** Animating `width`/`height`/`top`/`left` causes layout thrash.
2. **No `window.addEventListener('scroll')`.** Use `IntersectionObserver` or a framework's scroll directive (`@scroll.window` in Alpine, `useScroll` in VueUse).
3. **Honor `prefers-reduced-motion`.** Every animation degrades to static.
4. **One continuous animation per viewport max.** Marquees, aurora, canvas fields count.
5. **`backdrop-blur` only on fixed/sticky elements or Glass cards over texture.** Never on scrolling cards.
6. **Continuous-animation exception**: the `linear` ban is for *state transitions*. Marquees, aurora, canvas fields correctly use `linear`.
7. **Motion must be motivated.** If it doesn't communicate hierarchy or state, delete it.

### `prefers-reduced-motion`

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

This must be in `Layout.astro` / `app/layout.tsx` global CSS, not per-component.

## Scroll reveals (canonical pattern)

One global `IntersectionObserver` in the layout, plus a `.reveal` CSS class. NOT per-component observers, NOT `x-intersect` for entrance-only effects.

```html
<!-- Layout.astro -->
<script is:inline>
  const observer = new IntersectionObserver((entries) => {
    for (const entry of entries) {
      if (entry.isIntersecting) {
        entry.target.classList.add('visible');
        observer.unobserve(entry.target);  // disconnect after firing
      }
    }
  }, { threshold: 0.1, rootMargin: '0px 0px -10% 0px' });

  document.querySelectorAll('.reveal').forEach((el) => observer.observe(el));
</script>

<style is:global>
  .reveal {
    opacity: 0;
    transform: translateY(20px);
    transition: opacity 0.6s cubic-bezier(0.16, 1, 0.3, 1),
                transform 0.6s cubic-bezier(0.16, 1, 0.3, 1);
  }
  .reveal.visible {
    opacity: 1;
    transform: translateY(0);
  }

  @media (prefers-reduced-motion: reduce) {
    .reveal { opacity: 1; transform: none; transition: none; }
  }
</style>
```

Usage:
```html
<section class="reveal relative overflow-hidden py-24 md:py-32">
  <!-- content -->
</section>
```

### Removal trap

If you remove the observer JS without removing the `.reveal { opacity: 0 }` CSS, the page goes blank. Always remove both in the same edit.

## Hover states

Hover must change `color`, `background-color`, `border-color`, or `opacity` — never `transform: scale()` on cards (it's a template cliché and breaks layout if the card is at the edge).

```css
.card { transition: border-color 200ms ease, background-color 200ms ease; }
.card:hover { border-color: var(--color-accent); background-color: var(--color-surface-elevated); }
```

Hover budgets:
- $1,500 anchor: 200ms max.
- $5,000 anchor: 400ms max.
- Image opacity crossfade allowed at $5,000.

## CSS lifecycle classes (htmx-style)

If you're using htmx (see `htmx-pro`), it applies lifecycle classes you can hook:
- `htmx-request` — during the request (on the indicator or triggering element).
- `htmx-swapping` — during the swap phase.
- `htmx-settling` — during the settling phase.
- `htmx-added` — on newly added content before settling.

```css
tr.htmx-swapping td { opacity: 0; transition: opacity 1s ease-out; }
.htmx-added { opacity: 0; }
#element { transition: opacity 300ms ease-in; }
```

## View Transitions API

```html
<div hx-swap="innerHTML transition:true">
```

```css
@keyframes slide-from-right { from { transform: translateX(100%); } }
@keyframes slide-to-left { to { transform: translateX(-100%); } }
::view-transition-old(content) { animation: slide-to-left 0.3s; }
::view-transition-new(content) { animation: slide-from-right 0.3s; }
.content { view-transition-name: content; }
```

Use sparingly — most view transitions are gratuitous. Reserve for genuine page-level navigation (SPA route changes).

## Decorative canvas (constellation, particle fields)

Permitted as the single continuous animation per viewport. Must:
1. Respect `prefers-reduced-motion: reduce` (return early, render static or nothing).
2. Be gated behind `pointer-events-none`.
3. Avoid assigning to read-only props — `canvas.clientWidth` is read-only. Set `canvas.style.width` then `canvas.width = canvas.clientWidth`. Under `'use strict'` the read-only assignment throws and kills the script.

```js
const canvas = document.getElementById('stars');
if (!canvas) return;
const ctx = canvas.getContext('2d');

function resize() {
  canvas.style.width = '100%';
  canvas.style.height = '100%';
  canvas.width = canvas.clientWidth;
  canvas.height = canvas.clientHeight;
}

const reduced = window.matchMedia('(prefers-reduced-motion: reduce)').matches;
if (reduced) {
  // render a static frame, no animation loop
  drawFrame(ctx, canvas.width, canvas.height);
} else {
  resize();
  window.addEventListener('resize', resize);
  requestAnimationFrame(animate);
}
```

## Z-index layers

Single source of truth:

| Layer | Z-index | Use |
|-------|---------|-----|
| Background | `-z-30` | Surface texture (Layout.astro) |
| Grid | `-z-20` | SVG grid pattern (Layout.astro) |
| Section glow | `-z-10` | Per-section subtle glow (Layout.astro) |
| Local | `z-10` | Default content |
| Cascade | `z-30` | Sticky elements, sticky headers |
| Overlay | `z-40` | Backdrop behind modal |
| Nav/modal | `z-50` | Mobile menu, modal dialog |
| Toast | `z-50` | Toasts (same as nav, but in different container) |

## Performance budgets

| Metric | Target |
|--------|--------|
| LCP (Largest Contentful Paint) | < 2.5s |
| FID/INP (Interaction to Next Paint) | < 200ms |
| CLS (Cumulative Layout Shift) | < 0.1 |
| FCP (First Contentful Paint) | < 1.8s |
| TTFB (Time to First Byte) | < 600ms |
| Total JS (gzipped) | < 100KB on landing pages |
| Lighthouse Performance | ≥ 90 |
| Lighthouse Accessibility | ≥ 95 |

### Deferring non-critical JS

```html
<!-- Analytics after hydration -->
<script src="/analytics.js" defer></script>

<!-- Or Next.js: -->
<Script src="/analytics.js" strategy="afterInteractive" />

<!-- Or third-party chat widget, etc. -->
<Script src="/chat.js" strategy="lazyOnload" />
```

### Image optimization

- Use the framework's image component (`next/image`, `<NuxtImg>`, Astro's `<Image>`).
- Always specify `width` and `height` (or use `aspect-ratio` CSS) to prevent CLS.
- Use modern formats (AVIF, WebP) — most image components handle this automatically.
- `loading="lazy"` for below-the-fold images (default in modern browsers).
- `loading="eager"` + `fetchpriority="high"` for the LCP image (hero).

```html
<img src="/hero.jpg" alt="Crew installing shingles" width="1200" height="675" loading="eager" fetchpriority="high" />
<img src="/chart.png" alt="" width="800" height="450" loading="lazy" decoding="async" />
```

### Font loading

```css
@font-face {
  font-family: 'Inter';
  src: url('/fonts/inter-var.woff2') format('woff2-variations');
  font-weight: 100 900;
  font-display: swap;        /* show fallback immediately, swap when loaded */
  font-style: normal;
}
```

Or via framework:
```ts
// Next.js
import { Inter } from 'next/font/google';
const inter = Inter({ subsets: ['latin'], display: 'swap' });

// Astro
import { Inter } from '@fontsource/inter';
```

Avoid `font-display: block` (invisible text up to 3s).

### Code splitting

- Route-level splitting (lazy import routes in React Router / Vue Router / Angular Router).
- `defineAsyncComponent` (Vue) / `lazy()` (React) / `loadComponent` (Angular) for heavy components below the fold.
- Direct imports (not barrel files) for tree-shaking.

### Web Vitals monitoring

```ts
import { onCLS, onINP, onLCP, onFCP, onTTFB } from 'web-vitals';

function send(metric: { name: string; value: number; rating: string }) {
  navigator.sendBeacon('/analytics/vitals', JSON.stringify(metric));
}

onCLS(send); onINP(send); onLCP(send); onFCP(send); onTTFB(send);
```

## Quick Reference

| Concern | Rule |
|---------|------|
| Animatable properties | `transform`, `opacity` only |
| Scroll listener | `IntersectionObserver`, never `window.addEventListener('scroll')` |
| Reduced motion | Honor `prefers-reduced-motion`; degrade to static |
| Continuous animations | One per viewport max; `linear` is OK for these |
| `backdrop-blur` | Only on fixed/sticky or Glass cards over texture |
| Hover | Color/bg/border/opacity; no `scale()` on cards |
| Hover budget | 200ms ($1,500), 400ms ($5,000) |
| Z-index | `-z-30` to `z-50`; documented per layer |
| LCP | < 2.5s, eager-load hero image with `fetchpriority="high"` |
| Images | Always `width`+`height`, `loading="lazy"` below fold |
| Fonts | `font-display: swap`, never `block` |
| JS budget | < 100KB gzipped on landing pages |
| Lighthouse | Perf ≥ 90, A11y ≥ 95 |

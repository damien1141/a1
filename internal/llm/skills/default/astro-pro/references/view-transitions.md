# View Transitions (Astro 5)

Astro's View Transitions API provides SPA-like navigation with native browser animations, falling back gracefully. Add `<ClientRouter />` from `astro:transitions` to your layout's `<head>`, then annotate elements for morph/slide/fade/persist behavior.

## Enabling

```astro
---
// src/layouts/Layout.astro
import { ClientRouter } from 'astro:transitions';
interface Props { title: string; }
const { title } = Astro.props;
---
<!doctype html>
<html lang="en">
  <head>
    <meta charset="utf-8" />
    <title>{title}</title>
    <ClientRouter />
  </head>
  <body>
    <header transition:persist>
      <nav><!-- survives navigation, stays mounted --></nav>
    </header>
    <main>
      <slot />
    </main>
  </body>
</html>
```

Without `<ClientRouter />`, every navigation is a full page reload. With it, Astro intercepts same-origin link clicks, fetches the next page, swaps the DOM, and animates between matched elements.

## Directives

### `transition:name` — pair elements across routes for morph

Two elements with the same `transition:name` on different pages will morph into each other during navigation. Use for: hero images, product photos, modal openers, "view more" cards.

```astro
<!-- src/pages/index.astro -->
<li>
  <a href={`/products/${product.id}/`}>
    <img src={product.image} alt={product.name} transition:name={`product-${product.id}`} />
  </a>
</li>
```

```astro
<!-- src/pages/products/[id].astro -->
<img src={product.image} alt={product.name} transition:name={`product-${product.id}`} />
```

The image morphs from the list position/size to the detail position/size. Names must be **unique per page** (a single source and single destination).

### `transition:animate` — built-in animations

| Value | Behavior |
|---|---|
| `slide` (default for main) | Outgoing page slides out left, incoming slides in from right |
| `fade` | Cross-fade |
| `morph` | Element-level morph (use with `transition:name`) |
| `none` | No animation, instant swap |

```astro
<main transition:animate="fade">
  <slot />
</main>

<h1 transition:animate="slide">About</h1>
```

### `transition:persist` — keep element mounted across navigation

Use for: nav bars, audio players, video streams, complex client state (carousels, maps). The element's DOM node is preserved; Astro does not re-render it.

```astro
<header transition:persist>
  <nav>...</nav>
</header>

<audio controls transition:persist>
  <source src="/stream.mp3" type="audio/mpeg" />
</audio>
```

`transition:persist:name="nav"` — names a persist target so a different element on the next page can claim it (rare; only when you want a different element to inherit another's state).

## Lifecycle Events

Astro fires custom events on `document` during navigation:

```ts
// src/scripts/nav-events.ts — load with is:inline in Layout.astro
document.addEventListener('astro:before-preparation', () => {
  // before fetch of next page starts
});

document.addEventListener('astro:after-preparation', () => {
  // next page HTML fetched, not yet swapped
});

document.addEventListener('astro:before-swap', () => {
  // before DOM swap — last chance to read old DOM
});

document.addEventListener('astro:after-swap', () => {
  // after swap, new DOM is in place — re-init third-party widgets here
  // e.g. re-bind jQuery, re-init analytics, re-mount Alpine components manually
});

document.addEventListener('astro:page-load', () => {
  // after first load and every subsequent navigation
  // equivalent to DOMContentLoaded but for SPA navigations too
});
```

Typical pattern — re-init analytics + scroll position:

```ts
document.addEventListener('astro:after-swap', () => {
  // Re-trigger analytics pageview
  window.gtag?.('event', 'page_view', { page_path: location.pathname });
});

document.addEventListener('astro:page-load', () => {
  // Scroll to top on every navigation unless there's a hash
  if (!location.hash) window.scrollTo(0, 0);
});
```

## Custom Animations (CSS)

Override the default slide/fade with CSS:

```css
@keyframes fade-in {
  from { opacity: 0; }
  to { opacity: 1; }
}

@keyframes fade-out {
  from { opacity: 1; }
  to { opacity: 0; }

/* Apply to ::view-transition-old and ::view-transition-new */
@media (prefers-reduced-motion: no-preference) {
  ::view-transition-old(root) { animation: fade-out 200ms ease-out forwards; }
  ::view-transition-new(root) { animation: fade-in 200ms ease-in forwards; }
}
```

Named transitions (used by `transition:name="foo"`):

```css
::view-transition-old(product-42) { animation: fade-out 150ms; }
::view-transition-new(product-42) { animation: fade-in 150ms; }
```

## `prefers-reduced-motion` (mandatory)

All transitions must honor reduced motion. Astro ships a sensible default that disables animations, but custom CSS must opt out explicitly:

```css
@media (prefers-reduced-motion: reduce) {
  ::view-transition-old(*),
  ::view-transition-new(*) {
    animation: none !important;
  }
}
```

If a user has `prefers-reduced-motion: reduce`, the swap still happens (DOM updates) but the visual animation is skipped — content arrives instantly.

## Gotchas

- **One source, one destination** for `transition:name` — if two elements on the same page share a name, the morph breaks. Use IDs (`product-${id}`), not static strings, for lists.
- **`transition:persist` breaks if the element's structure changes** between pages — if the `<nav>` on page A has 3 links and on page B has 4, the persisted element shows 3 links. Use `transition:persist` only for elements that should NOT update. For nav state like "active link," don't persist — let it re-render.
- **Third-party widgets need `astro:after-swap` re-init** — CodeMirror, map libraries, video players, etc. assume a single mount. Wire `astro:after-swap` to re-init them.
- **Form state is lost without `transition:persist`** — if a user is typing in a form and navigates, the input is gone. Wrap forms (or the entire form section) in `transition:persist` if you want to preserve input across nav.
- **External links (`target="_blank"`, different origin) skip View Transitions** — full browser navigation. This is correct behavior.
- **Anchor links (`#section`) trigger a transition** — to skip the animation and just scroll, use `<a href="#section" data-astro-reload>`.

## Fallback

Astro automatically falls back to full-page navigation in browsers without View Transitions support (older Safari, Firefox without the flag). No code changes needed. The `astro:before-swap` / `astro:after-swap` events still fire on the full load.

## Verification

```bash
# Manual: navigate between routes, confirm morph/persist works
# Lighthouse: TBT should not regress (View Transitions are GPU-accelerated)
# Reduced motion: DevTools → Rendering → Emulate prefers-reduced-motion: reduce,
#   confirm no animation but swap still completes
```

```bash
rg -n 'transition:(name|animate|persist)' src/   # confirm annotations present
rg -n 'astro:after-swap' src/                    # confirm re-init wired for any 3rd-party widgets
```

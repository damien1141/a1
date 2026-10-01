# Integration: htmx + Astro

Alpine's three rules for the Astro/htmx pipeline:
1. **CDN, not npm import.** `<script defer>` in `<head>`; do NOT `import 'alpinejs'` or call `Alpine.start()`.
2. **Plugins BEFORE core.** Plugin scripts register via `alpine:init` and must run first.
3. **Global `IntersectionObserver` for scroll reveals.** Not `x-intersect` — see `transitions-and-motion.md`.

## Astro `Layout.astro` — canonical wiring

```astro
---
import '../styles/global.css';
---
<!doctype html>
<html lang="en">
  <head>
    <meta charset="utf-8" />
    <meta name="viewport" content="width=device-width, initial-scale=1" />
    <title>{Astro.props.title ?? 'App'}</title>

    <!-- 1. Alpine plugins (BEFORE core) -->
    <script is:inline defer src="https://cdn.jsdelivr.net/npm/@alpinejs/persist@3.x.x/dist/cdn.min.js"></script>
    <script is:inline defer src="https://cdn.jsdelivr.net/npm/@alpinejs/collapse@3.x.x/dist/cdn.min.js"></script>
    <script is:inline defer src="https://cdn.jsdelivr.net/npm/@alpinejs/focus@3.x.x/dist/cdn.min.js"></script>

    <!-- 2. Alpine core (auto-inits on DOMContentLoaded — DO NOT call Alpine.start()) -->
    <script is:inline defer src="https://cdn.jsdelivr.net/npm/alpinejs@3.x.x/dist/cdn.min.js"></script>

    <!-- 3. Register stores + factories BEFORE Alpine inits -->
    <script is:inline>
      document.addEventListener('alpine:init', () => {
        Alpine.store('ui', {
          sidebarOpen: false,
          theme: Alpine.$persist('light').as('theme'),
          toggleSidebar() { this.sidebarOpen = !this.sidebarOpen; },
        });
        Alpine.data('nav', () => ({
          open: false,
          toggle() { this.open = !this.open; },
        }));
      });
    </script>

    <!-- 4. FOUC guard + reduced-motion + scroll-reveal CSS -->
    <style is:global>
      [x-cloak] { display: none !important; }
      .reveal { opacity: 0; transform: translateY(24px); transition: opacity 0.6s ease-out, transform 0.6s ease-out; }
      .reveal.visible { opacity: 1; transform: translateY(0); }
      @media (prefers-reduced-motion: reduce) {
        .reveal { opacity: 1 !important; transform: none !important; transition: none !important; }
      }
    </style>

    <!-- 5. Global scroll-reveal IntersectionObserver (NOT x-intersect) -->
    <script is:inline>
      const io = new IntersectionObserver((entries, obs) => {
        for (const e of entries) {
          if (e.isIntersecting) { e.target.classList.add('visible'); obs.unobserve(e.target); }
        }
      }, { rootMargin: '100px' });
      document.addEventListener('DOMContentLoaded', () =>
        document.querySelectorAll('.reveal').forEach((el) => io.observe(el)));
    </script>
  </head>
  <body>
    <slot />
  </body>
</html>
```

### Why `is:inline`?
Astro processes and bundles `<script>` tags by default (strips them, hashes them, defers them). For Alpine's CDN scripts and your `alpine:init` listener, you want the raw script emitted as-is into the HTML — that's what `is:inline` does. Skip `is:inline` only for your own bundled modules.

### Why `defer` on the CDN scripts?
`defer` preserves execution order while letting the parser continue. Without `defer`, the browser blocks on each script download — bad for LCP. Without correct order (plugins before core), the plugin's `Alpine.plugin()` call fires after Alpine inits and never registers.

## The DON'Ts (canonical double-init bug)

```astro
---
// WRONG — npm install + import + start()
import Alpine from 'alpinejs';
import persist from '@alpinejs/persist';
Alpine.plugin(persist);
Alpine.start();
---
```
Why this is wrong in an Astro pipeline:
- You've already loaded the CDN script (auto-inits). Calling `Alpine.start()` re-inits and re-binds every directive — double-event-listeners, broken transitions, modals that open twice.
- You're shipping the npm copy AND the CDN copy — 30KB+ wasted.
- The `alpine:init` event has already fired; any plugins registered after `start()` are too late.

**Pick ONE:** CDN script (default for Astro/htmx/marketing sites) OR npm import (only for SPAs or when you need build-time tree-shaking). Never both.

## When npm import IS right

- You're bundling the whole app with Vite and want a single JS file.
- You need to tree-shake plugins you don't use.
- You're targeting strict CSP that forbids external scripts.

```js
// main.js (entry bundled by Vite)
import Alpine from 'alpinejs';
import persist from '@alpinejs/persist';
import focus from '@alpinejs/focus';
import collapse from '@alpinejs/collapse';

Alpine.plugin(persist);
Alpine.plugin(focus);
Alpine.plugin(collapse);

window.Alpine = Alpine;
Alpine.start(); // Required when not using the CDN auto-init script
```
Astro: skip `is:inline` on this script; let Astro bundle it. Do NOT also include the CDN script.

## Alpine + htmx — division of labor

| Concern | Tool |
|---|---|
| Server roundtrip (form submit, save, delete) | htmx `hx-post`/`hx-delete` |
| HTML fragment swap | htmx `hx-swap`/`hx-target` |
| Multi-target updates | htmx `hx-swap-oob` |
| Server → client events | htmx `HX-Trigger` header → `htmx:customEvent` |
| Client state (open/close, selected tab) | Alpine `x-data` |
| Transitions on state change | Alpine `x-transition` |
| Global state (cart, theme, sidebar) | Alpine `Alpine.store()` |
| Form input binding (before submit) | Alpine `x-model` |

### Pattern: Alpine owns client state, htmx fetches HTML fragments
```html
<div x-data="{ open: false, loaded: false }">
  <button @click="open = true; loaded = false"
          hx-get="/comments/42"
          hx-target="#comments-body"
          hx-trigger="click once"
          hx-on::after-request="loaded = true">
    Show comments
  </button>
  <div x-show="open" x-cloak
       x-transition:enter="transition ease-[cubic-bezier(0.16,1,0.3,1)] duration-300"
       x-transition:enter-start="opacity-0"
       x-transition:enter-end="opacity-100">
    <span x-show="!loaded" x-cloak>Loading…</span>
    <div id="comments-body"></div>
  </div>
</div>
```

### Pattern: htmx swap triggers Alpine init
When htmx injects new Alpine components via a swap, they auto-initialize — Alpine 3 uses a `MutationObserver` so new `x-data` roots are picked up automatically. No manual `Alpine.initTree()` call is needed.

### Pattern: htmx `HX-Trigger` → Alpine store
```http
HTTP/1.1 200 OK
HX-Trigger: {"toast": {"message": "Saved!"}}
```
```html
<script>
  document.body.addEventListener('toast', (e) => Alpine.store('toasts').push(e.detail.message));
</script>
```

### Pattern: `x-if` vs `x-show` across htmx swaps
- `x-if` removes the element from DOM. If htmx later swaps in markup referencing it, Alpine state is lost.
- `x-show` keeps the element in DOM (just `display: none`). State survives.
**Rule:** for surfaces htmx swaps into, use `x-show` + `x-cloak`, never `x-if`.

## Alpine in Astro components

Astro components are server-rendered. Alpine directives in `.astro` files pass through as raw HTML:
```astro
---
// Dropdown.astro
const { label = 'Menu' } = Astro.props;
---
<div x-data="{ open: false }" class="relative">
  <button type="button" @click="open = !open"
          :aria-expanded="open.toString()" aria-haspopup="menu">
    {label}
  </button>
  <div x-show="open" x-cloak role="menu"
       x-transition:enter="transition ease-[cubic-bezier(0.16,1,0.3,1)] duration-200"
       x-transition:enter-start="opacity-0 -translate-y-2"
       x-transition:enter-end="opacity-100 translate-y-0"
       @click.outside="open = false"
       @keydown.escape.window="open = false">
    <slot />
  </div>
</div>
```
- Astro does NOT process `@click`/`x-show` — they pass through to the client as-is.
- Use `{jsExpression}` for server-side interpolation; `x-text="jsExpression"` for client-side.
- For repeated client-side components, define `Alpine.data('dropdown', () => ({...}))` once in the layout, then use `<div x-data="dropdown()">` in each component instance.

## No-JS fallback

Alpine degrades gracefully for `x-show`/`x-bind`/`x-text` (the element stays in its initial server-rendered state). It does NOT degrade for `x-if` (the element is removed once Alpine inits). For critical content:
- Render server-side as visible by default.
- Use `x-show="false"` to hide on init if needed (with `x-cloak` to prevent flash).
- Provide real `<a href>`/`<form action>` fallbacks for any `@click`-driven navigation.

## Quick reference

| Need | Use |
|---|---|
| Alpine in Astro | CDN `<script is:inline defer>` + `alpine:init` listener |
| Plugins | `<script is:inline defer>` BEFORE core script |
| FOUC guard | `<style is:global>[x-cloak] { display: none !important; }</style>` |
| Scroll reveal | Global `IntersectionObserver` + `.reveal`/`.reveal.visible` CSS (NOT `x-intersect`) |
| htmx + Alpine | htmx for roundtrips, Alpine for state; `x-show` (not `x-if`) on swap surfaces |
| Server → Alpine | htmx `HX-Trigger` → `addEventListener` → `Alpine.store` |
| npm install | Only for bundled SPAs; call `Alpine.start()` explicitly |
| No-JS fallback | Server-render visible, use real `<a>`/`<form>` for navigation |

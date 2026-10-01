---
name: alpine-pro
description: "Use when building interactive UI with Alpine.js 3.x — x-data/x-init/x-bind/x-on/x-show/x-if/x-for/x-model/x-transition, $store/$refs/$dispatch/$watch/$nextTick, Alpine.store/Alpine.data factories, plugins (mask/collapse/persist/focus/anchor), CDN-not-npm install in Astro/htmx. Verifies window.Alpine loads, x-cloak prevents FOUC, eslint passes with Alpine globals, Playwright + a11y for components."
license: MIT
metadata:
  author: super-skills
  version: "2.0.0"
  domain: frontend
  triggers: "Alpine.js,x-data,x-transition,Alpine.store,Alpine.data,x-intersect,$dispatch,Alpine.js 3,x-cloak,x-for,Alpine.plugin"
  role: specialist
  scope: implementation
  output-format: code
  related-skills: "htmx-pro,astro-pro,javascript-pro,frontend-design"
---

# Alpine Pro

Alpine.js 3.x specialist. A 15KB reactive DOM toolkit — Vue-flavored reactivity without a build step. Use it for client-side state on top of server-rendered HTML (Astro, htmx, Django, Rails, Laravel). Pairs with htmx for roundtrips (server returns HTML fragments) and Alpine for client state (dropdowns, modals, tabs, toasts).

**Stack discipline inherited from the high-end visual-design library:** Alpine is loaded via CDN `<script defer>` in `<head>`; do NOT `import 'alpinejs'` or call `Alpine.start()` (the CDN auto-inits). Use one global `IntersectionObserver` for scroll reveals — not `x-intersect` — and ban `linear` for state transitions (continuous loops like marquee/aurora are the exception).

## When to Use

- Client-side interactivity on server-rendered HTML (Astro, htmx, Django, Rails, Laravel, PHP)
- UI patterns: dropdowns, modals, tabs, accordions, toasts, typeahead, infinite scroll, multi-step forms, drag-and-drop
- Global state across components via `Alpine.store`; reusable component factories via `Alpine.data`
- Pairing with htmx (htmx = server roundtrips/HTML fragments, Alpine = client state/transitions)
- Replacing vanilla-JS UI logic in an Astro/Tailwind pipeline that bans heavy frameworks
- Progressive enhancement on top of working HTML (Alpine degrades gracefully when JS is off for `x-show`/`x-bind`)

## Operating Loop

1. **Scope** — Name the component and its state. State the integration: standalone HTML, Astro component, htmx-fragment, or SSG. Confirm Alpine 3.x (CDN `@3.x.x`) and which plugins are needed (`mask`/`collapse`/`persist`/`focus`/`anchor`/`intersect`/`sort`).
2. **Recon** — Read the page head: Alpine CDN `<script defer>` present? `[x-cloak] { display: none !important; }` in CSS? Global `IntersectionObserver` in `Layout.astro` (not `x-intersect`)?
3. **Design state first** — Sketch the `x-data` shape (reactive / derived / static). Decide `Alpine.store` (cross-component) vs `Alpine.data` (factory) vs inline `x-data` (one-off). Plan transitions: cubic-bezier + duration (no `linear` for state).
4. **Implement** — Semantic HTML first (`<button>`, `<dialog>`, `<details>`), Alpine directives layered on. `@click.outside` to close, `@keydown.escape.window` for global keys, explicit `x-transition` with cubic-bezier. Bind ARIA live: `:aria-expanded`, `role="dialog"`.
5. **Verify (gate)** — In order, until clean:
   - `eslint .` with Alpine globals (`Alpine`, `$dispatch`, `$store`) allowed
   - Browser: `window.Alpine` defined after `DOMContentLoaded`; no console errors; `[x-cloak]` CSS applied (no FOUC)
   - `playwright test` — open/close, keyboard nav (Tab/Escape/Enter), focus trap in modals
   - a11y: ARIA roles correct, `prefers-reduced-motion` respected
   - If any step fails: fix the cause, do not weaken config. Re-run from the top.
6. **Exit** — Write the report. **VERIFIED**: list each command + summary. **ASSUMED**: list what you believe but did not run (e.g. screen-reader behavior, mobile gesture, SSR hydration).

## Reference Guide

| Topic | Reference file | Load when |
|---|---|---|
| Directives & magic properties | `references/directives-and-magic.md` | `x-data`/`x-init`/`x-bind`/`x-on`/`x-show`/`x-if`/`x-for`/`x-model`/`x-ref`/`x-teleport`/`x-transition`/`x-effect`/`x-ignore`/`x-cloak`; `$el`/`$refs`/`$event`/`$dispatch`/`$nextTick`/`$watch`/`$store`/`$data`/`$id`/`$root` |
| State & stores | `references/state-and-stores.md` | `Alpine.store()` global state, `Alpine.data()` factories, `Alpine.reactive()`/`Alpine.effect()`, reactive vs non-reactive |
| Transitions & motion | `references/transitions-and-motion.md` | `x-transition` `:enter`/`:leave` cubic-bezier timings, the `linear` ban for state, continuous-loop exception, `prefers-reduced-motion` |
| Plugins | `references/plugins.md` | `@alpinejs/mask`, `collapse`, `persist`, `focus`, `anchor`, `intersect` (and when to prefer a global `IntersectionObserver`), `sort`, UI kit |
| Patterns & components | `references/patterns-and-components.md` | Dropdowns, modals (focus trap), tabs, accordions, toasts, typeahead, infinite scroll, drag-and-drop, form validation |
| Integration: htmx + Astro | `references/integration-htmx-astro.md` | CDN-not-npm install, `x-cloak` FOUC, Alpine + htmx (server roundtrips vs client state), Astro `is:inline`, `Alpine.start()` DON'T call |
| Testing & a11y | `references/testing-and-a11y.md` | `@alpinejs/testing`, dispatching events, asserting on DOM, keyboard nav, ARIA, focus trap, `prefers-reduced-motion` |

## Constraints

### MUST DO
- Load Alpine via CDN `<script defer>` in `<head>`; do NOT `import 'alpinejs'` or call `Alpine.start()` (CDN auto-inits)
- Add `[x-cloak] { display: none !important; }` to CSS — prevents FOUC on every `x-show`/`x-if` component
- Specify `x-transition` with explicit `:enter`/`:enter-start`/`:enter-end` (+`:leave*`) and a cubic-bezier + duration — never default transitions
- Ban `linear`/`ease-in-out` for state transitions (continuous loops like marquee/aurora are the only exception)
- Build on semantic HTML (`<button>`, `<dialog>`, `<details>`, `<form>`) — Alpine is progressive enhancement
- Bind ARIA live to state: `:aria-expanded="open.toString()"`, `role="dialog"`/`aria-modal="true"` on modals
- Use `Alpine.store()` for cross-component state, `Alpine.data()` for reusable factories, inline `x-data` for one-offs
- For scroll-reveals in a design-library pipeline: use the global `IntersectionObserver` + `.reveal`/`.reveal.visible` CSS, NOT `x-intersect`

### MUST NOT DO
- `import 'alpinejs'` or call `Alpine.start()` when using the CDN script (double-init bug)
- Use default `x-transition` (no curve/duration) — leads to `linear` ease and sloppy motion
- Use `linear`/`ease-in-out` for state transitions (continuous loops are the only exception)
- Hardcode `aria-expanded="true"`/`"false"` — bind to Alpine state
- Use `<div>`/`<span>` with `@click` for actions — use `<button type="button">`
- Use `x-if` for elements that must persist across htmx swaps — use `x-show` + `x-cloak` (htmx may re-inject markup)
- Re-define the same `Alpine.data` factory in multiple inline scripts — register once via `alpine:init`
- Forget `x-cloak` on `x-show`/`x-if` surfaces — FOUC on slow connections
- Forget `@keydown.escape.window` and `@click.outside` on dropdowns/modals — a11y regressions
- Use deep reactivity in hot loops (large `x-for` over deeply-nested objects) — flatten or offload transforms

## Code Examples

### Dropdown with `x-transition`, `@click.outside`, Escape, ARIA
```html
<div x-data="{ open: false }" class="relative">
  <button type="button" @click="open = !open"
          :aria-expanded="open.toString()" aria-haspopup="menu">Menu</button>
  <div x-show="open" x-cloak role="menu"
       x-transition:enter="transition ease-[cubic-bezier(0.16,1,0.3,1)] duration-200"
       x-transition:enter-start="opacity-0 -translate-y-2"
       x-transition:enter-end="opacity-100 translate-y-0"
       x-transition:leave="transition ease-[cubic-bezier(0.16,1,0.3,1)] duration-150"
       x-transition:leave-start="opacity-100 translate-y-0"
       x-transition:leave-end="opacity-0 -translate-y-2"
       @click.outside="open = false"
       @keydown.escape.window="open = false"
       class="absolute mt-2 w-48 rounded-md border bg-white shadow-lg">
    <a href="/profile" role="menuitem" class="block px-4 py-2 hover:bg-gray-50">Profile</a>
    <a href="/settings" role="menuitem" class="block px-4 py-2 hover:bg-gray-50">Settings</a>
  </div>
</div>
```

### `Alpine.store` for global state + `Alpine.data` factory
```html
<head>
  <script defer src="https://cdn.jsdelivr.net/npm/alpinejs@3.x.x/dist/cdn.min.js"></script>
  <script>
    document.addEventListener('alpine:init', () => {
      Alpine.store('ui', {
        sidebarOpen: false,
        toast: null,
        toggleSidebar() { this.sidebarOpen = !this.sidebarOpen; },
      });
      Alpine.data('counter', (start = 0) => ({
        count: start,
        inc() { this.count++; this.$store.ui.notify?.('up'); },
        get double() { return this.count * 2; }, // derived reactive getter
      }));
    });
  </script>
</head>
<body>
  <button @click="$store.ui.toggleSidebar()">Menu</button>
  <aside x-show="$store.ui.sidebarOpen" x-cloak>...</aside>
  <div x-data="counter(10)">
    <span x-text="count"></span> · <span x-text="double"></span>
    <button @click="inc()">+1</button>
  </div>
</body>
```

### Modal with focus trap + `$nextTick` + `$watch`
```html
<div x-data="modalController()" @keydown.escape.window="close()">
  <button @click="open = true" aria-haspopup="dialog">Open</button>
  <dialog x-show="open" x-cloak
          x-transition:enter="transition ease-[cubic-bezier(0.16,1,0.3,1)] duration-300"
          x-transition:enter-start="opacity-0" x-transition:enter-end="opacity-100"
          @click.outside="close()" role="dialog" aria-modal="true" aria-labelledby="t">
    <div class="bg-white rounded-xl p-6 max-w-md">
      <h2 id="t">Confirm</h2>
      <button x-ref="first" @click="close()">Cancel</button>
      <button @click="confirm()">Confirm</button>
    </div>
  </dialog>
</div>
<script>
  document.addEventListener('alpine:init', () => {
    Alpine.data('modalController', () => ({
      open: false, lastFocus: null,
      init() {
        this.$watch('open', (v) => {
          if (v) { this.lastFocus = document.activeElement; this.$nextTick(() => this.$refs.first.focus()); }
          else { this.lastFocus?.focus(); }
        });
      },
      close() { this.open = false; },
      confirm() { this.$dispatch('confirmed'); this.close(); },
    }));
  });
</script>
```

### htmx + Alpine (server roundtrips for data, Alpine for client state)
```html
<div x-data="{ open: false, loaded: false }">
  <button @click="open = true; loaded = false"
          hx-get="/comments/42" hx-target="#comments-body"
          hx-trigger="click once"
          hx-on::after-request="loaded = true">Show comments</button>
  <div x-show="open" x-cloak x-transition>
    <span x-show="!loaded">Loading…</span>
    <div id="comments-body"></div>
  </div>
</div>
```

### Astro integration (CDN, `is:inline`, NO `Alpine.start()`)
```astro
<head>
  <!-- Alpine CDN — defer + auto-init. NEVER call Alpine.start(). -->
  <script is:inline defer src="https://cdn.jsdelivr.net/npm/alpinejs@3.x.x/dist/cdn.min.js"></script>
  <!-- Plugins load BEFORE core Alpine. -->
  <script is:inline defer src="https://cdn.jsdelivr.net/npm/@alpinejs/persist@3.x.x/dist/cdn.min.js"></script>
  <script is:inline>
    document.addEventListener('alpine:init', () => {
      Alpine.store('theme', Alpine.$persist('light').as('theme'));
      Alpine.data('nav', () => ({ open: false, toggle() { this.open = !this.open } }));
    });
  </script>
  <style is:global>[x-cloak] { display: none !important; }</style>
  <!-- Global scroll-reveal IntersectionObserver — NOT x-intersect. -->
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
```

## Output Template

When delivering an Alpine component, provide in this order:

1. **HTML markup** — semantic baseline (`<button>`, `<dialog>`, `<details>`), Alpine directives layered on, `x-cloak` on every `x-show`/`x-if` surface, ARIA bound to state
2. **State definition** — inline `x-data` for one-offs, `Alpine.data()` factory (registered via `alpine:init`) for reusable components, `Alpine.store()` for global state
3. **Transitions** — explicit `x-transition` `:enter`/`:enter-start`/`:enter-end` (+`:leave*`), cubic-bezier + duration (no `linear` for state)
4. **`<head>` wiring** — Alpine CDN `<script defer>`, plugins BEFORE core, `alpine:init` listener for stores/factories, `[x-cloak]` CSS rule
5. **CSS** — `prefers-reduced-motion` fallback (set `transition: none`), focus-visible outlines
6. **Tests** — Playwright e2e (open/close, keyboard nav, focus trap); `@alpinejs/testing` for unit-level component tests
7. **Verification block**:
   ```
   $ eslint .                              # Alpine globals allowed
   $ playwright test --project=chromium
   ✓ window.Alpine defined | no FOUC | 8 e2e passed | a11y: focus trap OK, Escape closes
   ```
8. **Exit report** — VERIFIED / ASSUMED / lingering risk (screen-reader announcements, mobile gestures, no-JS fallback)

## Knowledge Reference

Alpine 3.x · `x-data`/`x-init`/`x-bind` (`:`)/`x-on` (`@`)/`x-text`/`x-html`/`x-model`/`x-modelable`/`x-show`/`x-if`/`x-for`/`x-ref`/`x-teleport`/`x-transition`/`x-effect`/`x-ignore`/`x-cloak` · `$el`/`$refs`/`$event`/`$dispatch`/`$nextTick`/`$watch`/`$store`/`$data`/`$id`/`$root` · `Alpine.store()`/`Alpine.data()`/`Alpine.reactive()`/`Alpine.effect()`/`Alpine.plugin()`/`Alpine.$persist` · plugins: `@alpinejs/mask`/`collapse`/`persist`/`focus`/`anchor`/`intersect`/`sort` · `x-transition` cubic-bezier timings (no `linear` for state) · event modifiers `.stop`/`.prevent`/`.self`/`.once`/`.passive`/`.window`/`.document`/`.outside`/`.debounce`/`.throttle` · CDN-not-npm install · `alpine:init` event · `[x-cloak]` FOUC guard · `is:inline` Astro scripts · htmx pairing (server roundtrips vs client state) · `@alpinejs/testing` · Playwright · a11y: focus trap, ARIA live binding, `prefers-reduced-motion` · global `IntersectionObserver` preferred over `x-intersect` for scroll reveals

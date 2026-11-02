# Plugins

Alpine 3.x plugins extend the core with directives and magic properties. Load each plugin script BEFORE the core Alpine script (the CDN auto-registers via `Alpine.plugin(...)` on `alpine:init`).

## Install pattern (CDN)

```html
<head>
  <!-- 1. Plugins first -->
  <script defer src="https://cdn.jsdelivr.net/npm/@alpinejs/persist@3.x.x/dist/cdn.min.js"></script>
  <script defer src="https://cdn.jsdelivr.net/npm/@alpinejs/collapse@3.x.x/dist/cdn.min.js"></script>
  <script defer src="https://cdn.jsdelivr.net/npm/@alpinejs/focus@3.x.x/dist/cdn.min.js"></script>
  <!-- 2. Core last -->
  <script defer src="https://cdn.jsdelivr.net/npm/alpinejs@3.x.x/dist/cdn.min.js"></script>
</head>
```
With `defer`, scripts execute in order despite being downloaded in parallel. Plugin scripts call `document.addEventListener('alpine:init', () => Alpine.plugin(...))` internally, so order matters.

## `@alpinejs/persist` — `Alpine.$persist`

```html
<script>
  document.addEventListener('alpine:init', () => {
    Alpine.store('ui', {
      theme: Alpine.$persist('light').as('theme'),
      sidebarOpen: Alpine.$persist(false).as('sidebar-open'),
    });
  });
</script>
<button @click="$store.ui.theme = $store.ui.theme === 'light' ? 'dark' : 'light'">
  Theme: <span x-text="$store.ui.theme"></span>
</button>
```
- Persists to `localStorage` under the `as()` key.
- Rehydrates automatically on page load.
- Methods don't persist — only the value.
- Use `Alpine.$persist(value).as('key').using(sessionStorage)` for session-scoped persistence.

## `@alpinejs/collapse` — smooth height transitions

```html
<div x-data="{ open: false }">
  <button @click="open = !open">Toggle</button>
  <div x-show="open" x-collapse>
    Hidden content animates its height smoothly.
  </div>
</div>
```
- Animates `height: 0` → `height: auto` (which CSS transitions can't do natively).
- Modifiers: `x-collapse.duration.500ms`, `x-collapse.min-x.px`, `x-collapse.max-x.px`.

## `@alpinejs/focus` — focus management

```html
<div x-data="{ open: false }" x-trap="open">
  <!-- When open is true, focus moves into this subtree and is trapped. -->
  <input x-ref="first" placeholder="Name" />
  <button @click="open = false">Close</button>
</div>
```
- `x-trap="boolean"` — when true, focuses the first focusable child and traps Tab within.
- When false, returns focus to the previously-focused element.
- Essential for accessible modals — see `patterns-and-components.md`.

Modifiers: `x-trap.inline` (don't return focus), `x-trap.noinitialfocus` (don't auto-focus first child), `x-trap.nofallback` (don't fall back to container if no focusable children).

## `@alpinejs/anchor` — floating UI positioning

```html
<button x-data="{ open: false }" @click="open = !open" x-anchor="$refs.menu">
  Open
</button>
<div x-ref="menu" x-show="open" x-cloak x-anchor.me>
  Menu floats relative to the button.
</div>
```
- `x-anchor="$refs.target"` positions the element relative to the target.
- Modifiers for placement: `.top` `.bottom` `.left` `.right`, plus `.start` `.end` `.center` for alignment.
- Auto-flips when near viewport edges (with `.flip`).
- Use for dropdowns, popovers, tooltips — replaces Popper.js for simple cases.

## `@alpinejs/mask` — input masks

```html
<input type="text" x-mask="999-999-9999" placeholder="Phone" />
<input type="text" x-mask="$9,999.99" placeholder="Currency" />
<input type="text" x-mask="mm/dd/yyyy" />
<input type="text" x-mask:dynamic="($input) => $input.startsWith('1') ? '999-999-9999' : '(99) 9999-9999'" />
```
- `9` matches any digit; `a` letter; `*` alphanumeric; literal chars are preserved.
- `:dynamic` accepts a function returning the mask based on input.
- Server-side normalization still required — masks are UX only.

## `@alpinejs/intersect` — `x-intersect`

```html
<div x-data="{ shown: false }" x-intersect="shown = true">
  <p x-show="shown">Revealed on scroll into view</p>
</div>
<div x-intersect.once="loadChart()">Chart loads when scrolled into view</div>
```
- Modifiers: `.once` (fire one time), `.half` (50% threshold), `.full` (100% threshold).
- Accepts an expression that runs when the element enters the viewport.

**Design-library override:** for *scroll reveals* (decorative sections fading in), use the global `IntersectionObserver` + `.reveal`/`.reveal.visible` CSS — not `x-intersect`. The plugin is acceptable for *functional* intersections (load data, trigger an animation, lazy-mount a heavy component) where you need Alpine-state awareness.

## `@alpinejs/sort` — drag-and-drop reordering

```html
<ul x-data="{ items: ['A', 'B', 'C', 'D'] }">
  <template x-for="(item, i) in items" :key="item">
    <li x-sort:item="i" x-text="item"></li>
  </template>
</ul>
```
- `x-sort:item="i"` makes the element draggable and updates `items` order on drop.
- Pairs with `x-sort.handle` for a drag handle, `x-sort.group` for cross-list dragging.
- For full sortable lists with custom drag previews, use SortableJS instead.

## `@alpinejs/ui` — Alpine UI kit

A higher-level component library on top of Alpine. Provides `<x-ui-modal>`, `<x-ui-dropdown>`, `<x-ui-tabs>`, etc. as ready-made components. Useful for rapid prototyping; for design-system-driven projects prefer hand-rolled components that match your archetype tokens.

## Building a custom plugin

```js
// my-plugin.js (loaded before core Alpine)
document.addEventListener('alpine:init', () => {
  Alpine.plugin((Alpine) => {
    Alpine.directive('log', (el, { expression }) => {
      el.addEventListener('click', () => console.log(expression));
    });
    Alpine.magic('now', () => () => Date.now());
  });
});
```
- `Alpine.directive(name, (el, { value, expression, modifiers, attributes }, { cleanup, evaluate, effect }))` — register a custom directive.
- `Alpine.magic(name, (el) => value)` — register a magic property.
- Use `cleanup(() => ...)` to remove listeners when the element is destroyed.

## When NOT to use a plugin

- **Scroll reveals** → global `IntersectionObserver` (see `transitions-and-motion.md`).
- **Tooltips** → CSS-only with `:hover` + `group-hover` for simple cases; `@alpinejs/anchor` only if positioning is dynamic.
- **Form validation** → hand-rolled with `x-model` + a getter for error state. The pattern is small enough that a plugin adds weight without value.
- **Toasts** → `Alpine.store('toasts')` + a single `<template x-for>` container — see `patterns-and-components.md`.

## Quick reference

| Plugin | Adds | Use |
|---|---|---|
| `persist` | `Alpine.$persist` | localStorage-backed state |
| `collapse` | `x-collapse` | Smooth height: 0 → auto |
| `focus` | `x-trap` | Focus trap for modals |
| `anchor` | `x-anchor` | Floating UI positioning |
| `mask` | `x-mask` | Input formatting |
| `intersect` | `x-intersect` | Functional scroll triggers (NOT for decorative reveals) |
| `sort` | `x-sort` | Drag-and-drop reorder |
| `ui` | `<x-ui-*>` | Component kit (optional) |
| (custom) | `Alpine.directive` + `Alpine.magic` | Project-specific directives |

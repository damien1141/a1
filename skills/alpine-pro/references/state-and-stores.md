# State & Stores

Alpine's reactivity is Vue 3-inspired: a `reactive()` proxy tracks reads, `effect()` re-runs when tracked props change. Most components use `x-data` for local state; `Alpine.store` and `Alpine.data` are the building blocks for cross-component and reusable state.

## Local state — `x-data`

```html
<div x-data="{ count: 0, inc() { this.count++ } }">
  <span x-text="count"></span>
  <button @click="inc()">+1</button>
</div>
```
- `this` inside methods refers to the data object.
- Getters are reactive:
```js
x-data="{ items: [1,2,3], get total() { return this.items.length } }"
```
- Methods can be `async`; Alpine awaits them in `x-init` and event handlers.

## `Alpine.data()` — reusable component factory

When the same component appears multiple times or grows beyond a one-liner, register a factory:
```html
<script>
  document.addEventListener('alpine:init', () => {
    Alpine.data('tabs', (defaultTab = 0) => ({
      active: defaultTab,
      tabs: [],
      select(i) { this.active = i; this.$dispatch('tab-change', { index: i }); },
      init() {
        this.tabs = [...this.$root.querySelectorAll('[role="tab"]')].map((t, i) => ({
          id: this.$id('tab'), label: t.textContent, index: i,
        }));
      },
    }));
  });
</script>

<div x-data="tabs(0)">
  <button x-for="(t, i) in tabs" :key="t.id" :aria-selected="(active === i).toString()"
          @click="select(i)" x-text="t.label"></button>
</div>
```
Rules:
- Register inside `alpine:init` BEFORE Alpine auto-inits.
- Factory accepts args — `x-data="tabs(0)"` passes `0` to the factory.
- `init()` runs automatically after the component is created.
- `this.$root`, `this.$refs`, `this.$dispatch`, `this.$store` are available inside methods.

## `Alpine.store()` — global reactive state

```html
<script>
  document.addEventListener('alpine:init', () => {
    Alpine.store('cart', {
      items: [],
      get count() { return this.items.reduce((n, i) => n + i.qty, 0); },
      get total() { return this.items.reduce((s, i) => s + i.qty * i.price, 0); },
      add(item) {
        const existing = this.items.find((i) => i.id === item.id);
        if (existing) existing.qty++;
        else this.items.push({ ...item, qty: 1 });
      },
      remove(id) { this.items = this.items.filter((i) => i.id !== id); },
    });

    Alpine.store('ui', {
      sidebarOpen: false,
      theme: 'light',
      toggleSidebar() { this.sidebarOpen = !this.sidebarOpen; },
    });
  });
</script>

<!-- Anywhere in the app -->
<button @click="$store.cart.add({ id: 1, price: 9.99 })">Add to cart</button>
<span x-text="`Cart: ${$store.cart.count} items ($${$store.cart.total})`"></span>

<aside x-show="$store.ui.sidebarOpen" x-cloak>...</aside>
```
- Stores are singletons — same instance across all components.
- Access via `$store.name` in markup, `Alpine.store('name')` in JS.
- Getters are reactive; methods mutate freely.
- Persist a store to `localStorage` with the `persist` plugin (see `plugins.md`):
```js
Alpine.store('theme', Alpine.$persist('light').as('theme'));
```

## `Alpine.reactive()` and `Alpine.effect()` — escape hatches

For logic that lives outside an `x-data` scope:
```js
document.addEventListener('alpine:init', () => {
  const state = Alpine.reactive({ users: [], loading: false });

  Alpine.effect(() => {
    // Re-runs whenever state.loading or state.users change
    document.querySelector('#count').textContent = state.users.length;
  });

  // Mutate from anywhere — effects re-run
  async function load() {
    state.loading = true;
    state.users = await fetch('/api/users').then(r => r.json());
    state.loading = false;
  }
});
```
Use cases:
- Bridge Alpine to a non-Alpine library (D3, Three.js, Chart.js).
- Pre-populate a store from an async fetch before the component mounts.
- Build a custom directive that needs its own reactive state.

## Reactive vs non-reactive

**Reactive** (Alpine tracks reads and re-runs effects):
- Properties declared in `x-data` / `Alpine.store` / `Alpine.reactive`.
- Plain object properties accessed in `x-effect` / `x-text` / `:class` etc.

**Non-reactive** (changes do NOT trigger re-render):
- Class instances (e.g. `new Map()`, `new Date()`, custom classes).
- Values assigned to `this` outside the initial data object.
- Properties added after creation (Alpine 3 makes a best effort but proxies on existing props only).

```js
Alpine.data('bad', () => ({
  items: new Map(),        // NOT reactive — Alpine can't observe Map mutations
  init() { this.cache = {}; }, // NOT reactive — added post-creation
}));
```
Workarounds:
- Replace `Map` with a plain object or reassign `items = new Map([...])` to trigger updates.
- Declare all reactive properties up front, even as `null`/`undefined`.
- For `Map`/`Set`, mutate then reassign: `this.items = new Map(this.items.set(k, v))`.

## Derived state — prefer getters over `$watch`

```js
Alpine.data('cart', () => ({
  items: [],
  get count() { return this.items.length; },          // reactive
  get total() { return this.items.reduce((s, i) => s + i.qty * i.price, 0); },
}));
```
- Getters recompute lazily when read in a template — no `effect` overhead.
- Avoid `$watch('items', updateTotal)` + manual `this.total = ...` — that's the anti-pattern getters replace.

## `$watch` — when to use it

```js
init() {
  this.$watch('activeTab', (newVal, oldVal) => {
    history.replaceState(null, '', `#tab-${newVal}`);
  });
}
```
- Use for side-effects: history, localStorage, analytics, third-party sync.
- Do NOT use to update derived state — use a getter.
- Watch dot-paths: `this.$watch('form.email', fn)`.

## State shape — keep it flat

```js
// Bad — deep nesting causes slow reactivity
Alpine.data('page', () => ({
  data: { user: { profile: { address: { city: '' } } } }
}));

// Good — flat with explicit keys
Alpine.data('page', () => ({
  userCity: '',
  async loadUser(id) { const u = await fetchUser(id); this.userCity = u.profile.address.city; },
}));
```
For large lists, avoid `x-for` over deeply-nested objects — flatten before render:
```js
init() {
  this.flatItems = this.rawItems.map(({ id, name, meta }) => ({
    id, name, tag: meta?.tag ?? 'misc',
  }));
}
```

## Quick reference

| Need | Use |
|---|---|
| One-off component | inline `x-data="{ ... }"` |
| Reusable component | `Alpine.data('name', (arg) => ({ ... }))` + `x-data="name(arg)"` |
| Cross-component state | `Alpine.store('name', { ... })` + `$store.name` |
| Persisted store | `Alpine.$persist(value).as('key')` (persist plugin) |
| External reactive state | `Alpine.reactive({...})` + `Alpine.effect(() => ...)` |
| Derived value | `get prop() { return ... }` (getter — reactive) |
| Side-effect on change | `$watch('path', cb)` or `x-effect` |
| Wait for DOM update | `$nextTick(() => ...)` |
| Init logic | `init()` method inside data/factory |

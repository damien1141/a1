# Directives & Magic Properties

Alpine 3.x reference for every directive and magic. Syntax: most directives have a shorthand (`x-bind:` → `:`, `x-on:` → `@`).

## Directives

### `x-data` — declare component scope
```html
<div x-data="{ open: false, count: 0 }">...</div>
<div x-data="myFactory(42)">...</div>   <!-- references Alpine.data('myFactory', ...) -->
```
- Required on every Alpine component root.
- Object literal OR reference to an `Alpine.data()` factory.
- Nested `x-data` creates nested scopes — inner state shadows outer.

### `x-init` — run setup
```html
<div x-data="{ items: [] }" x-init="items = await fetch('/api/items').then(r => r.json())">
```
- Runs after Alpine initializes the component but before first paint effects.
- Can be `async` — Alpine awaits it.
- For factory functions, define `init()` inside the data object instead of `x-init=`.

### `x-bind` / `:` — attribute binding
```html
<button :disabled="loading">Save</button>
<div :class="{ 'bg-red': isError, 'bg-green': !isError }">    <!-- object form -->
<input :value="form.name" :class="error ? 'invalid' : 'valid'">
<a :href="`/users/${user.id}`">Profile</a>
```
- `:class` accepts object (`{ cls: bool }`), array (`['a', condition && 'b']`), or string.
- `:style` accepts object (`{ color: 'red' }`) or string.
- Bind `checked`, `disabled`, `readonly`, `selected`, `href`, `src`, `value`, ARIA attributes.

### `x-on` / `@` — event listeners
```html
<button @click="open = !open">Toggle</button>
<form @submit.prevent="save()">...</form>
<input @input.debounce.500ms="search($event.target.value)">
<div @click.outside="close()"></div>
<button @keydown.escape.window="close()">Close</button>
```
Modifiers: `.stop` `.prevent` `.self` `.once` `.passive` `.capture` `.window` `.document` `.outside` `.debounce` `.debounce.500ms` `.throttle` `.throttle.500ms`.

### `x-text` / `x-html` — content binding
```html
<p x-text="user.name"></p>            <!-- sets textContent; escapes HTML -->
<div x-html="rawHtml"></div>          <!-- sets innerHTML; XSS risk if untrusted -->
```
Prefer `x-text` (escaped) unless you control the HTML source. `x-html` with server-rendered trusted fragments only.

### `x-model` — two-way form binding
```html
<input type="text" x-model="form.name">
<textarea x-model="form.bio"></textarea>
<input type="checkbox" x-model="form.terms">              <!-- boolean -->
<input type="checkbox" value="opt-a" x-model="form.opts"> <!-- array: ['opt-a', ...] -->
<select x-model="form.country">
  <option value="us">United States</option>
  <option value="ca">Canada</option>
</select>
<input type="radio" value="monthly" x-model="form.billing">
```
Modifiers: `.lazy` (sync on `change` not `input`), `.number` (cast to number), `.trim` (trim whitespace), `.debounce.500ms`, `.throttle.500ms`.

### `x-modelable` — expose nested property for `x-model`
```html
<div x-data="{ open: false }" x-modelable="open" x-model="dialogOpen">
  <!-- parent's dialogOpen two-way binds to local `open` -->
</div>
```

### `x-show` — toggle `display` (keeps DOM)
```html
<div x-show="open" x-cloak>...</div>
```
- Sets `display: none` when false; preserves the element in DOM.
- Pairs with `x-transition` for animation.
- Use `x-cloak` to avoid FOUC.

### `x-if` — add/remove from DOM
```html
<template x-if="show">
  <p>Only rendered when show is true</p>
</template>
```
- MUST be on a `<template>` element.
- Element is fully removed (not just hidden) when false.
- Use for expensive conditional content. Do NOT use for surfaces that htmx swaps — use `x-show` + `x-cloak` instead (htmx may re-inject markup and lose Alpine state).

### `x-for` — list rendering
```html
<template x-for="item in items" :key="item.id">
  <li x-text="item.name"></li>
</template>
<template x-for="(item, index) in items" :key="item.id">
  <li><span x-text="index + 1"></span>. <span x-text="item.name"></span></li>
</template>
<template x-for="n in 10" :key="n">  <!-- range -->
  <li x-text="n"></li>
</template>
```
- MUST be on a `<template>` element with ONE root child.
- `:key` is required for stable identity across updates.

### `x-ref` — direct DOM reference
```html
<input x-ref="email" type="email" />
<button @click="$refs.email.focus()">Focus email</button>
```
Use sparingly — prefer declarative bindings. Right tool for focus management and third-party libraries.

### `x-teleport` — render elsewhere in DOM
```html
<div x-data="{ open: false }">
  <button @click="open = true">Open</button>
  <template x-teleport="body">
    <div x-show="open" x-cloak>Modal content — moved to end of body</div>
  </template>
</div>
```
Useful for modals, tooltips, toasts — escapes parent `overflow: hidden` and `z-index` stacking.

### `x-transition` — see `transitions-and-motion.md` for full reference
```html
<div x-show="open" x-cloak
     x-transition:enter="transition ease-[cubic-bezier(0.16,1,0.3,1)] duration-300"
     x-transition:enter-start="opacity-0 translate-y-4"
     x-transition:enter-end="opacity-100 translate-y-0">
```
**MUST specify curve + duration. NEVER use `linear`/`ease-in-out` for state changes.**

### `x-effect` — re-run when deps change
```html
<div x-data="{ a: 1, b: 2 }" x-effect="console.log('sum:', a + b)"></div>
```
Like `$watch` but auto-tracks dependencies. Use for side-effects that should re-run whenever any referenced reactive property changes.

### `x-ignore` — skip Alpine processing
```html
<div x-ignore>
  <!-- Alpine does NOT process this subtree. Useful for off-screen heavy components or pre-rendered HTML that should stay static. -->
</div>
```

### `x-cloak` — hide pre-init
```html
<style>[x-cloak] { display: none !important; }</style>
<div x-show="open" x-cloak>...</div>
```
- Alpine removes the `x-cloak` attribute after initialization.
- Without the CSS rule, `x-show="false"` elements flash visible before Alpine hides them.
- Apply to every `x-show`/`x-if` surface.

## Magic properties

| Magic | Returns | Use |
|---|---|---|
| `$el` | HTMLElement | The current directive's element |
| `$refs` | Object | Map of `x-ref` references |
| `$event` | Event | The DOM event in `@` handlers |
| `$dispatch(name, detail)` | void | Dispatch a `CustomEvent` (bubbles) on `$el` |
| `$nextTick(callback)` | Promise | Run after Alpine's reactive DOM update |
| `$watch(expr, callback)` | void | Run callback when expr changes |
| `$store` | Object | Access registered `Alpine.store` instances |
| `$data` | Object | Access another component's data (rare) |
| `$id(name)` | string | Generate unique IDs for ARIA wiring |
| `$root` | HTMLElement | The `x-data` root of the current scope |

### `$dispatch` — custom events with payload
```html
<div x-data="{ onSaved(e) { console.log(e.detail.id); } }" @saved="onSaved($event)">
  <button @click="$dispatch('saved', { id: 42 })">Save</button>
</div>
```
- Bubbles up by default. Use `$dispatch('saved', { ... }, { bubbles: false })` to suppress.
- Pair with `@saved="handler($event)"` on an ancestor.

### `$watch` — observe changes
```html
<div x-data="{ count: 0 }" x-init="$watch('count', (v) => console.log('count:', v))">
  <button @click="count++">+1</button>
</div>
```
- Watches dot-paths (`'form.name'`).
- For multiple properties, use `x-effect` instead of multiple `$watch` calls.

### `$nextTick` — wait for DOM update
```html
<button @click="open = true; $nextTick(() => $refs.input.focus())">Open</button>
```
- Reactive DOM updates are batched; `$nextTick` runs after they flush.
- Required before reading layout or focusing newly-shown elements.

### `$id` — ARIA wiring for paired elements
```html
<div x-data="{ id: $id('combobox') }">
  <input :aria-controls="`${id}-listbox`" />
  <ul :id="`${id}-listbox`" role="listbox">...</ul>
</div>
```
Generates unique IDs per component instance — essential when a component appears multiple times on a page.

## Quick reference

| Need | Use |
|---|---|
| Component scope | `x-data="{ ... }"` |
| Attribute binding | `:class="..."`, `:disabled="loading"` |
| Event handling | `@click="..."`, `@keydown.escape.window="..."` |
| Two-way form | `x-model="form.name"` |
| Toggle visibility | `x-show="open"` + `x-cloak` (keeps DOM) |
| Conditional render | `<template x-if="...">` (adds/removes DOM) |
| List rendering | `<template x-for="x in xs" :key="x.id">` |
| Direct DOM ref | `x-ref="name"` + `$refs.name` |
| Render elsewhere | `<template x-teleport="body">` |
| Side-effect on change | `x-effect="..."` |
| Hide pre-init | `x-cloak` + `[x-cloak] { display: none !important; }` |
| Custom event up | `$dispatch('name', detail)` + `@name="handler"` |
| Wait for DOM update | `$nextTick(() => ...)` |
| Watch one property | `$watch('path', cb)` |
| Global state | `$store.namespace.prop` |
| Unique IDs for ARIA | `$id('role')` |

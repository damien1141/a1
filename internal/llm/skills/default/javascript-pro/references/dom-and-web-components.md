# DOM & Web Components

Vanilla DOM patterns that scale without a framework. Use Web Components (`customElements` + Shadow DOM + `<template>`) for reusable, framework-agnostic widgets; use event delegation and `AbortSignal` for teardown everywhere else.

## Selection

```js
const el = document.querySelector('#app .card');
const cards = document.querySelectorAll('.card');           // static NodeList
const [first] = document.getElementsByClassName('card');    // live HTMLCollection
```
Prefer `querySelector`/`querySelectorAll` over legacy `getElementById`/`getElementsByTagName` for consistency. Convert `NodeList` to `Array` only when you need `map`/`filter`:
```js
[...document.querySelectorAll('.card')].filter((c) => c.dataset.active === '1');
```

## Event delegation

Attach one listener to a container, branch on `event.target`:
```js
list.addEventListener('click', (e) => {
  const btn = e.target.closest('[data-action]');
  if (!btn || !list.contains(btn)) return;
  const action = btn.dataset.action;
  if (action === 'delete') removeRow(btn.dataset.id);
  if (action === 'edit')   editRow(btn.dataset.id);
});
```
- One listener for any number of children — survives dynamic insertion.
- `closest('[data-action]')` climbs to the actionable element even from nested children.

## `addEventListener` with `AbortSignal` — the canonical teardown

```js
class View {
  #ac = new AbortController();

  mount(root) {
    const { signal } = this.#ac;
    root.addEventListener('click', this.#onClick, { signal });
    root.addEventListener('input', this.#onInput, { signal });
    document.addEventListener('keydown', this.#onKey, { signal });
    window.addEventListener('resize', this.#onResize, { signal, passive: true });
  }

  unmount() {
    this.#ac.abort(); // every listener registered with `signal` is removed atomically
  }

  #onClick = (e) => { /* ... */ };
  #onInput = (e) => { /* ... */ };
  #onKey   = (e) => { /* ... */ };
  #onResize = () => { /* ... */ };
}
```
This is the **only** pattern to use. Manual `removeEventListener` bookkeeping is a leak waiting to happen.

### Modifiers via options
```js
el.addEventListener('scroll', handler, { passive: true });          // never preventDefault
el.addEventListener('click', handler, { once: true });              // auto-remove after first
el.addEventListener('click', handler, { capture: true });           // catch in capture phase
el.addEventListener('click', handler, { signal, passive, once });
```

## Custom events with payload
```js
el.dispatchEvent(new CustomEvent('item-selected', {
  bubbles: true,
  composed: true,        // cross shadow boundary
  detail: { id: 42 },
}));

// Listener:
el.addEventListener('item-selected', (e) => {
  console.log(e.detail.id);
});
```

## `<template>` — declare DOM, clone on demand
```html
<template id="row-tpl">
  <tr>
    <td class="name"></td>
    <td class="action"><button type="button" data-action="delete">×</button></td>
  </tr>
</template>
```
```js
const tpl = document.getElementById('row-tpl');
function addRow(name) {
  const frag = tpl.content.cloneNode(true);
  frag.querySelector('.name').textContent = name;
  tbody.append(frag);
}
```
`<template>` content is inert — no scripts run, no images load — until cloned.

## Web Components — `customElements`

### Lifecycle
```js
class CountdownTimer extends HTMLElement {
  // observed attributes (only these trigger attributeChangedCallback)
  static observedAttributes = ['seconds'];

  #ac = new AbortController();
  #root = this.attachShadow({ mode: 'open' });
  #remaining = 0;

  constructor() { super(); }

  connectedCallback() {
    this.#render();
    this.#root.querySelector('button')
      .addEventListener('click', () => this.#start(), { signal: this.#ac.signal });
  }

  disconnectedCallback() {
    this.#ac.abort(); // remove all signal-bound listeners
  }

  attributeChangedCallback(name, oldVal, newVal) {
    if (name === 'seconds') this.#remaining = Number(newVal);
  }

  #render() {
    this.#root.innerHTML = `<p>0</p><button type="button">Start</button>`;
  }
  #start() { /* ... */ }
}

customElements.define('countdown-timer', CountdownTimer);
```
Lifecycle order: `constructor` → `connectedCallback` (inserted) → `attributeChangedCallback` (for declared observed attributes) → `disconnectedCallback` (removed). `adoptedCallback` fires when moved between documents (rare).

Rules:
- `constructor` must call `super()` first and may NOT touch attributes or children — the element isn't fully wired yet.
- `connectedCallback` is the place to set up listeners and start work. It may fire multiple times if the element moves.
- Always pair listener setup with `disconnectedCallback` teardown (use `AbortSignal`).
- `customElements.define(name, ctor)` is idempotent per name; defining the same name twice throws. Use `customElements.whenDefined(name)` to await another definition.

### Shadow DOM
```js
const shadow = this.attachShadow({ mode: 'open' });  // 'open' = inspectable, 'closed' = hidden
shadow.adoptedStyleSheets = [sheet];                  // shared stylesheet (faster than <style>)
shadow.innerHTML = `<style>:host { display: block; }</style><slot></slot>`;
```
- `<slot>` projects light-DOM children into the shadow tree.
- `:host` styles the custom element itself.
- Shadow DOM encapsulates styles and IDs — `document.querySelector('#x')` from the page cannot pierce in.
- Use `composed: true` on `CustomEvent` to bubble out of shadow DOM.

### Constructable stylesheets (shared across components)
```js
const sheet = new CSSStyleSheet();
sheet.replaceSync(`
  :host { display: block; font: inherit; }
  button { background: var(--accent, blue); }
`);
class Btn extends HTMLElement {
  constructor() {
    super();
    const shadow = this.attachShadow({ mode: 'open' });
    shadow.adoptedStyleSheets = [sheet];
    shadow.innerHTML = `<button><slot></slot></button>`;
  }
}
```
Faster and deduplicated across thousands of instances vs per-instance `<style>`.

### Form-associated custom elements
```js
class StarRating extends HTMLElement {
  static formAssociated = true;
  #internals = this.attachInternals();

  connectedCallback() {
    this.addEventListener('click', (e) => {
      const value = Number(e.target.dataset.value);
      this.#internals.setFormValue(String(value));
      this.#internals.setValidity({ rangeUnderflow: value < 1 }, 'pick at least one');
    });
  }
}
```
Lets custom elements participate in `<form>` serialization and validation.

## MutationObserver

```js
const mo = new MutationObserver((mutations) => {
  for (const m of mutations) {
    if (m.type === 'childList') {
      m.addedNodes.forEach((n) => n.nodeType === 1 && enhance(n));
    }
  }
});
mo.observe(root, { childList: true, subtree: true });
// mo.disconnect() when done — leaks otherwise.
```
Useful for progressive enhancement of server-rendered HTML or third-party content; do NOT use as a poor man's reactivity system — Web Components are the right tool for that.

## IntersectionObserver for lazy load

```js
const io = new IntersectionObserver(
  (entries, observer) => {
    for (const e of entries) {
      if (!e.isIntersecting) continue;
      const img = e.target;
      img.src = img.dataset.src;
      observer.unobserve(img); // fire-once
    }
  },
  { rootMargin: '200px' },
);
document.querySelectorAll('img[data-src]').forEach((img) => io.observe(img));
```

## Quick reference

| Need | Use |
|---|---|
| Select | `querySelector` / `querySelectorAll` |
| Many children, one listener | event delegation + `closest()` |
| Listener teardown | `addEventListener(type, fn, { signal })` + `controller.abort()` |
| Custom payload events | `new CustomEvent(name, { detail, bubbles, composed })` |
| Inert DOM | `<template>` + `content.cloneNode(true)` |
| Reusable widget | `customElements.define(name, class extends HTMLElement)` |
| Encapsulated styles | `attachShadow({ mode: 'open' })` + `adoptedStyleSheets` |
| Form participation | `static formAssociated = true` + `attachInternals()` |
| DOM watch | `MutationObserver` + `disconnect()` |
| Lazy images | `IntersectionObserver` + `unobserve` after fire |

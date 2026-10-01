# htmx 2.x Attribute Reference

## HTTP verb attributes

`hx-get`, `hx-post`, `hx-put`, `hx-patch`, `hx-delete` — issue an AJAX request to the given URL. **None inherited.**

```html
<button hx-get="/api/items">Load Items</button>
<form hx-post="/api/items">…</form>
<button hx-put="/api/items/1">Update</button>
<button hx-patch="/api/items/1">Partial Update</button>
<button hx-delete="/api/items/1">Delete</button>
```

- Empty value (`hx-get=""`) requests the current URL.
- GET on non-form elements does NOT include surrounding input values by default. Use `hx-include="closest form"`.
- For DELETE that should remove the element: return `200` with empty body. `204` triggers NO swap.

## `hx-trigger`

**Syntax:** `hx-trigger="<event>[<filter>] <modifiers>, ..."`

### Default triggers
- Most elements: `click`
- `<input>`/`<textarea>`/`<select>`: `change`
- `<form>`: `submit`

### Event filters (JS expressions in `[]`)

```html
<div hx-get="/data" hx-trigger="click[ctrlKey]">Ctrl+Click only</div>
<input hx-get="/search" hx-trigger="keyup[key=='Enter']" />
<div hx-get="/data" hx-trigger="click[event.shiftKey]">Shift+Click</div>
```

### Modifiers

| Modifier | Description | Example |
|----------|-------------|---------|
| `once` | Trigger only once | `click once` |
| `changed` | Only if value changed | `keyup changed` |
| `delay:<time>` | Debounce | `input delay:500ms` |
| `throttle:<time>` | Throttle | `scroll throttle:200ms` |
| `from:<selector>` | Listen on another element | `keyup from:body` |
| `target:<selector>` | Filter by event target | `click target:.btn` |
| `consume` | Stop propagation | `click consume` |
| `queue:<strategy>` | Queue behavior | `click queue:last` |

### `from:` extended selectors
- `from:document` / `from:window` — global
- `from:closest <sel>` — nearest ancestor
- `from:find <sel>` — descendant
- `from:next` / `from:previous` — siblings
- `from:body` — for `HX-Trigger` response headers

### Special events

```html
<div hx-get="/content" hx-trigger="load">             <!-- page load -->
<div hx-get="/content" hx-trigger="revealed">         <!-- scrolled into view -->
<img hx-get="/image" hx-trigger="intersect once threshold:0.5">   <!-- IntersectionObserver -->
<div hx-get="/status" hx-trigger="every 2s">          <!-- polling -->
<div hx-get="/status" hx-trigger="every 1s [isActive]">           <!-- conditional polling -->
```

### Multiple triggers

```html
<input hx-post="/search"
       hx-trigger="input changed delay:500ms, keyup[key=='Enter'], load">
```

### Keyboard shortcuts

```html
<button hx-post="/action"
        hx-trigger="click, keyup[altKey&&shiftKey&&key=='D'] from:body">
  Do Action (Alt+Shift+D)
</button>
```

**Not inherited.**

## `hx-target`

| Syntax | Description |
|--------|-------------|
| CSS selector | Any matching element |
| `this` | The element itself |
| `closest <sel>` | Nearest ancestor |
| `find <sel>` | First descendant |
| `next` / `next <sel>` | Next sibling |
| `previous` / `previous <sel>` | Previous sibling |

```html
<div hx-target="#output">
<tr hx-target="closest tbody">
<button hx-target="find .result">
<button hx-target="next div">
```

**Inherited.**

## `hx-swap`

Values: `innerHTML` (default), `outerHTML`, `textContent`, `beforebegin`, `afterbegin`, `beforeend`, `afterend`, `delete`, `none`.

### Modifiers

```html
<div hx-swap="outerHTML swap:1s">       <!-- delay swap for animation -->
<div hx-swap="outerHTML settle:1s">     <!-- delay settle -->
<div hx-swap="innerHTML transition:true"> <!-- View Transitions API -->
<div hx-swap="innerHTML ignoreTitle">   <!-- don't update page title -->
<div hx-swap="innerHTML scroll:top">    <!-- scroll target after swap -->
<div hx-swap="innerHTML show:top">      <!-- scroll element into view -->
<div hx-swap="innerHTML focus-scroll:true">
```

`outerHTML` on `<body>` is automatically converted to `innerHTML` (you cannot replace the body element). **Inherited.**

## `hx-swap-oob`

Response elements with `hx-swap-oob` update different parts of the page simultaneously.

| Value | Behavior |
|-------|----------|
| `true` | Swap into element with matching `id` using `innerHTML` |
| Any `hx-swap` value | Use that strategy |
| `<strategy>:<selector>` | Swap using strategy into element matching selector |

```html
<!-- In response HTML: -->
<div id="alerts" hx-swap-oob="true">New alert!</div>
<div id="count" hx-swap-oob="outerHTML">42</div>
<div hx-swap-oob="innerHTML:#sidebar">Sidebar content</div>
```

### Template wrapping (table elements)

`<tr>`, `<td>`, `<option>` can't exist outside their parent. Wrap in `<template>`:

```html
<template><tr id="row-5" hx-swap-oob="true"><td>Updated</td></tr></template>
```

For SVG, double-wrap:
```html
<template><svg><circle id="c1" hx-swap-oob="true" r="10"/></svg></template>
```

Nested OOB swaps are processed by default (`htmx.config.allowNestedOobSwaps`). **Not inherited.**

## `hx-select` / `hx-select-oob`

```html
<div hx-get="/page" hx-select="#main-content">           <!-- pick portion for swap -->
<div hx-get="/page"
     hx-select="#content"
     hx-select-oob="#sidebar, #nav:afterbegin">          <!-- pick OOB portions -->
```

**Both inherited.**

## `hx-vals` / `hx-vars`

```html
<!-- Static JSON -->
<button hx-post="/action" hx-vals='{"key": "value", "count": 42}'>Go</button>

<!-- Dynamic (js: prefix evaluates JS) -->
<div hx-post="/action" hx-vals='js:{lastKey: event.key, time: Date.now()}'>Go</div>
```

**Inherited.** Child values override parent. `hx-vars` is deprecated — use `hx-vals` (more XSS-safe).

## `hx-headers`

```html
<button hx-post="/api" hx-headers='{"X-CSRF-Token": "abc123"}'>Go</button>
<button hx-post="/api" hx-headers='js:{"X-Token": getToken()}'>Go</button>
```

**Inherited.** Child overrides parent.

## `hx-params`

```html
<div hx-params="*">          <!-- all (default) -->
<div hx-params="none">       <!-- none -->
<div hx-params="name,email"> <!-- only these -->
<div hx-params="not token">  <!-- all except these -->
```

**Inherited.**

## `hx-include`

```html
<button hx-post="/action" hx-include="#extra-fields">
<button hx-post="/action" hx-include="closest form">
<button hx-post="/action" hx-include="find .inputs">
<button hx-post="/action" hx-include="next input">
```

Non-input elements include all their child inputs. Disabled inputs ignored. `inherit` keyword combines parent + own selectors. **Inherited.**

## `hx-encoding`

```html
<form hx-post="/upload" hx-encoding="multipart/form-data">
  <input type="file" name="file">
  <button>Upload</button>
</form>
```

**Inherited.**

## `hx-boost`

Progressively enhance links and forms to use AJAX:

```html
<div hx-boost="true">
  <a href="/page">Becomes AJAX</a>
  <form action="/submit" method="post">Becomes AJAX</form>
</div>
<a href="/normal" hx-boost="false">Still normal</a>
```

- Anchors: GET to href, targets body, pushes URL to history.
- Forms: method determines verb, targets body, does NOT push URL by default.
- Only same-domain links boosted (not `#anchors`, not external).

**Inherited.**

## `hx-confirm` / `hx-prompt`

```html
<button hx-delete="/item" hx-confirm="Delete this item?">Delete</button>
<button hx-post="/rename" hx-prompt="Enter new name:">Rename</button>
```

`hx-prompt` value sent in `HX-Prompt` request header. Custom confirm via `htmx:confirm` event:

```js
document.body.addEventListener('htmx:confirm', (e) => {
  if (!e.target.hasAttribute('hx-confirm')) return;
  e.preventDefault();
  showCustomDialog().then((ok) => { if (ok) e.detail.issueRequest(true); });
});
```

**Both inherited.**

## `hx-indicator`

```html
<button hx-get="/data" hx-indicator="#spinner">Load</button>
<span id="spinner" class="htmx-indicator">Loading…</span>
```

htmx adds `htmx-request` class to the indicator during requests. Default CSS transitions `opacity` 0 → 1.

```html
<button hx-indicator="closest .card">
<div hx-indicator="#global-spinner">
  <button hx-indicator="inherit #local-spinner">
```

Without `hx-indicator`, the triggering element gets `htmx-request`. **Inherited.**

## `hx-disabled-elt`

```html
<button hx-post="/save" hx-disabled-elt="this">Save</button>
<form hx-post="/save" hx-disabled-elt="find input, find button">
```

**Inherited.**

## `hx-on`

```html
<button hx-on:click="alert('clicked')">Click</button>
<button hx-get="/data" hx-on::before-request="showSpinner()">     <!-- htmx events -->
<button hx-get="/data" hx-on::after-request="hideSpinner()">
<form hx-post="/save" hx-on::after-request="if(event.detail.successful) this.reset()">

<button hx-on:htmx:before-request="showSpinner()">                <!-- full form -->
<button hx-on--before-request="showSpinner()">                    <!-- JSX-compatible -->
```

`::` is shorthand for `htmx:`. `this` = element, `event` = event object. **Not inherited** (but events bubble).

## `hx-push-url` / `hx-replace-url`

```html
<a hx-get="/page" hx-push-url="true">          <!-- push fetched URL -->
<a hx-get="/page" hx-push-url="/custom-url">   <!-- push custom URL -->
<a hx-get="/page" hx-push-url="false">         <!-- no history update -->
<a hx-get="/page" hx-replace-url="true">       <!-- replace current entry -->
<a hx-get="/page" hx-replace-url="/custom-url">
```

Response headers `HX-Push-Url` / `HX-Replace-Url` override the attributes. **Both inherited.**

## `hx-sync`

```html
<form hx-sync="this:replace">                  <!-- only latest submission goes through -->
<input hx-post="/validate" hx-sync="closest form:abort">   <!-- aborts if form submits -->
<button hx-get="/data" hx-sync="this:drop">    <!-- ignore clicks while in-flight -->
<button hx-get="/data" hx-sync="this:queue last">
```

| Strategy | Behavior |
|----------|----------|
| `drop` | Ignore new request if in-flight (default) |
| `abort` | Drop this request, abort ongoing |
| `replace` | Abort ongoing, replace with this |
| `queue first` | Queue first only |
| `queue last` | Queue last only |
| `queue all` | Queue all |

**Inherited.**

## `hx-request`

```html
<div hx-request='{"timeout": 5000}'>
<div hx-request='{"credentials": true}'>
<div hx-request='{"noHeaders": true}'>
<div hx-request='js:{"timeout": getTimeout()}'>
```

Merge-inherited (child values merge with parent).

## `hx-ext`

```html
<body hx-ext="response-targets, head-support">
  <div hx-ext="ignore:response-targets">
    <!-- response-targets disabled here, head-support still active -->
  </div>
</body>
```

**Inherited and merged.**

## `hx-preserve`

```html
<video id="player" hx-preserve>…</video>
```

Element unchanged across swaps. Requires stable `id`. Response must contain element with same `id`. **Not inherited.**

## `hx-disable`

```html
<div hx-disable>
  <!-- No htmx attributes processed here -->
  <div hx-get="/nope">This won't work</div>
</div>
```

Use for user-generated content as a security measure. **Inherited** (cannot be overridden).

## `hx-history` / `hx-history-elt`

```html
<body hx-history="false">          <!-- don't cache page in localStorage (sensitive data) -->
<div id="content" hx-history-elt>  <!-- use as history snapshot root (default: <body>) -->
```

**Not inherited.**

## `hx-disinherit` / `hx-inherit`

```html
<div hx-target="#output" hx-disinherit="*">          <!-- disable all inheritance -->
<div hx-target="#output" hx-disinherit="hx-target">  <!-- disable specific -->
<div hx-target="#output" hx-inherit="hx-target">     <!-- enable when global off -->
```

## `hx-validate`

```html
<input hx-post="/validate" hx-validate="true">
```

Forces HTML5 validation before request. Default: only `<form>` elements validate. **Not inherited.**

## Inheritance summary

**Inherited:** `hx-target`, `hx-swap`, `hx-select`, `hx-select-oob`, `hx-boost`, `hx-vals`, `hx-confirm`, `hx-indicator`, `hx-include`, `hx-push-url`, `hx-replace-url`, `hx-sync`, `hx-headers`, `hx-params`, `hx-ext`, `hx-request`, `hx-encoding`, `hx-disable`, `hx-disabled-elt`, `hx-prompt`, `hx-vars`.

**Not inherited:** `hx-get`, `hx-post`, `hx-put`, `hx-delete`, `hx-patch`, `hx-trigger`, `hx-swap-oob`, `hx-on`, `hx-preserve`, `hx-history-elt`, `hx-validate`.

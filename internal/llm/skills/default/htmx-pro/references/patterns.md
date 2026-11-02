# htmx UI Patterns

## Click to Edit

Display mode → click → edit form → save → back to display.

```html
<div hx-target="this" hx-swap="outerHTML">
  <p><strong>Name:</strong> Joe Smith</p>
  <p><strong>Email:</strong> joe@example.com</p>
  <button hx-get="/contact/1/edit" type="button">Edit</button>
</div>

<!-- GET /contact/1/edit returns: -->
<form hx-put="/contact/1" hx-target="this" hx-swap="outerHTML">
  <label>Name <input name="name" value="Joe Smith" /></label>
  <label>Email <input name="email" type="email" value="joe@example.com" /></label>
  <button type="submit">Save</button>
  <button hx-get="/contact/1" type="button">Cancel</button>
</form>
```

## Inline Validation

Validate as user types/tabs away.

```html
<div hx-target="this" hx-swap="outerHTML">
  <label for="email">Email</label>
  <input id="email" name="email" hx-post="/validate/email"
         hx-trigger="change, keyup delay:500ms changed"
         type="email" value="">
</div>
```

Server returns the wrapping div with `.error` or `.valid`:
```html
<div hx-target="this" hx-swap="outerHTML" class="error">
  <label for="email">Email</label>
  <input id="email" name="email" hx-post="/validate/email" type="email" value="bad">
  <span class="error-message">That email is already taken</span>
</div>
```

```css
.error input  { box-shadow: 0 0 3px red; }
.valid input  { box-shadow: 0 0 3px green; }
```

## Active Search

```html
<input type="search" name="q"
       hx-post="/search"
       hx-trigger="input changed delay:500ms, keyup[key=='Enter'], load"
       hx-target="#results"
       hx-indicator=".search-spinner">

<span class="search-spinner htmx-indicator" aria-hidden="true">Searching…</span>

<table>
  <thead><tr><th>Name</th><th>Email</th></tr></thead>
  <tbody id="results"></tbody>
</table>
```

- `input changed delay:500ms` debounces typing.
- `keyup[key=='Enter']` allows immediate search on Enter.
- `load` shows initial results on page load.

Server returns `<tr>` rows.

## Infinite Scroll

```html
<table>
  <tbody>
    <tr>…</tr>
    <tr>…</tr>
    <tr hx-get="/items?page=2" hx-trigger="revealed" hx-swap="afterend">
      <td>Loading…</td>
    </tr>
  </tbody>
</table>
```

Server returns more rows + a new sentinel for the next page. On the last page, omit the sentinel. For `overflow: auto/scroll` containers, use `intersect once` instead of `revealed`:

```html
<tr hx-trigger="intersect once" …>
```

## Click to Load

```html
<table><tbody id="contacts"><tr>…</tr></tbody></table>
<button hx-get="/contacts?page=2"
        hx-target="#contacts"
        hx-swap="beforeend"
        id="load-more">
  Load More
</button>
```

Last batch replaces the button via OOB:
```html
<tr>…</tr>
<button id="load-more" hx-swap-oob="true" hx-get="/contacts?page=3"
        hx-target="#contacts" hx-swap="beforeend">
  Load More
</button>
```

## Lazy Loading

```html
<div hx-get="/chart-data" hx-trigger="load">
  <img src="/spinner.gif" class="htmx-indicator" alt="">
</div>
```

```css
.htmx-settling img { opacity: 0; }
img { transition: opacity 300ms ease-in; }
```

## Delete Row with Animation

```html
<tbody hx-confirm="Are you sure?"
       hx-target="closest tr"
       hx-swap="outerHTML swap:1s">
  <tr>
    <td>Joe</td>
    <td><button hx-delete="/contact/1" type="button">Delete</button></td>
  </tr>
</tbody>
```

```css
tr.htmx-swapping td { opacity: 0; transition: opacity 1s ease-out; }
```

Server returns `200` with empty body. Row replaced with nothing.

## Edit Row (inline table editing)

```html
<tbody hx-target="closest tr" hx-swap="outerHTML">
  <tr>
    <td>Joe</td>
    <td>joe@example.com</td>
    <td><button hx-get="/contact/1/edit" type="button">Edit</button></td>
  </tr>
</tbody>
```

Edit mode: `<form>` can't go inside `<tr>`, so use `hx-include="closest tr"` on the save button.

## Bulk Update

```html
<form hx-post="/users/bulk-update" hx-swap="outerHTML settle:3s" hx-target="#toast">
  <table>
    <tr>
      <td><input type="checkbox" name="active:user1"></td>
      <td>User 1</td>
    </tr>
  </table>
  <button type="submit">Bulk Update</button>
  <output id="toast"></output>
</form>
```

```css
#toast { opacity: 0; transition: opacity 3s ease-out; }
#toast.htmx-settling { opacity: 100; }
```

## Progress Bar (polling)

```html
<button hx-post="/start-job">Start Job</button>

<!-- Returned after POST: -->
<div hx-target="this" hx-swap="innerHTML"
     hx-trigger="done" hx-get="/job/complete">
  <div hx-get="/job/progress"
       hx-trigger="every 600ms"
       hx-target="this"
       hx-swap="innerHTML">
    <div class="progress-bar" style="width:0%"></div>
  </div>
</div>
```

Server sends `HX-Trigger: done` when complete; outer div then fetches the completion state.

```css
.progress-bar { transition: width 0.6s ease; }
```

## Cascading Selects

```html
<label>Make:
  <select name="make" hx-get="/models" hx-target="#models">
    <option value="audi">Audi</option>
    <option value="toyota">Toyota</option>
  </select>
</label>
<label>Model:
  <select id="models" name="model">
    <option value="a1">A1</option>
  </select>
</label>
```

`GET /models?make=toyota` returns `<option>` elements.

## Tabs (HATEOAS — server-driven)

```html
<div id="tabs" hx-target="this" hx-swap="innerHTML"
     hx-get="/tab1" hx-trigger="load delay:100ms">
</div>
```

Each tab endpoint returns full tab nav + content:
```html
<a hx-get="/tab1" class="selected">Tab 1</a>
<a hx-get="/tab2">Tab 2</a>
<div id="tab-content">Content for tab 1…</div>
```

## Tabs (JavaScript — client-side switching)

```html
<div id="tabs" hx-target="#tab-contents" hx-swap="innerHTML">
  <button hx-get="/tab1" class="selected"
          hx-on:htmx:after-on-load="document.querySelectorAll('#tabs button').forEach(b => b.classList.remove('selected')); this.classList.add('selected');">
    Tab 1
  </button>
  <button hx-get="/tab2" hx-on:htmx:after-on-load="…">Tab 2</button>
</div>
<div id="tab-contents">Tab 1 content</div>
```

## Modal (custom, no Bootstrap)

```html
<button hx-get="/modal" hx-target="body" hx-swap="beforeend">Open Modal</button>
```

Server returns:
```html
<div id="modal-backdrop" class="modal-backdrop"
     _="on click trigger closeModal">
  <div class="modal-content"
       _="on closeModal add .closing wait for animationend then remove me">
    <h2>Title</h2>
    <p>Content…</p>
    <button _="on click trigger closeModal">Close</button>
  </div>
</div>
```

(`_=` is Alpine/hyperscript syntax. For plain JS, use `hx-on:click`.)

## File Upload with Progress

```html
<form hx-encoding="multipart/form-data" hx-post="/upload"
      hx-on::xhr:progress="document.querySelector('#progress').value = event.detail.loaded/event.detail.total * 100">
  <input type="file" name="file">
  <button>Upload</button>
  <progress id="progress" value="0" max="100"></progress>
</form>
```

## Dialogs (`hx-prompt` / `hx-confirm`)

```html
<button hx-post="/action"
        hx-prompt="Enter a value"
        hx-confirm="Are you sure?"
        hx-target="#response">
  Do Action
</button>
```

Server reads `HX-Prompt` header for the prompt value.

## Custom Confirmation (SweetAlert2 or similar)

```html
<button hx-post="/delete" hx-trigger="confirmed"
        onClick="Swal.fire({title:'Confirm?',preConfirm:()=>{htmx.trigger(this,'confirmed')}})">
  Delete
</button>
```

Or via `htmx:confirm`:
```js
document.body.addEventListener('htmx:confirm', (e) => {
  if (!e.target.hasAttribute('hx-confirm')) return;
  e.preventDefault();
  Swal.fire({ title: 'Are you sure?', text: e.detail.question })
    .then((r) => { if (r.isConfirmed) e.detail.issueRequest(true); });
});
```

## Keyboard Shortcuts

```html
<button hx-post="/action"
        hx-trigger="click, keyup[altKey&&shiftKey&&key=='D'] from:body">
  Action (Alt+Shift+D)
</button>
```

## Sortable (drag-and-drop)

```html
<form class="sortable" hx-post="/items/reorder" hx-trigger="end">
  <div class="item"><input type="hidden" name="item" value="1">Item 1</div>
  <div class="item"><input type="hidden" name="item" value="2">Item 2</div>
</form>
```

```js
htmx.onLoad((content) => {
  content.querySelectorAll('.sortable').forEach((el) => {
    new Sortable(el, { animation: 150, ghostClass: 'blue-bg' });
  });
});
```

## Updating Other Content (4 strategies)

### 1. Expand the target
Wrap form + table in shared container, target that.

### 2. Out of Band (OOB)
```html
<form>…</form>
<tbody id="contacts-table" hx-swap-oob="beforeend">
  <tr><td>New Contact</td></tr>
</tbody>
```

### 3. Server-triggered events
Server: `HX-Trigger: newContact`
```html
<tbody hx-get="/contacts" hx-trigger="newContact from:body"></tbody>
```

### 4. Path Dependencies extension
```html
<body hx-ext="path-deps">
  <form hx-post="/contacts">…</form>
  <tbody hx-get="/contacts" hx-trigger="path-deps" path-deps="/contacts">
```

## Reset Form After Submit

```html
<form hx-post="/items"
      hx-target="#items-list"
      hx-swap="beforeend"
      hx-on::after-request="if(event.detail.successful) this.reset()">
  <input name="item">
  <button>Add</button>
</form>
```

## Animations

### Fade out on delete
```css
tr.htmx-swapping td { opacity: 0; transition: opacity 1s; }
```
```html
<tr hx-swap="outerHTML swap:1s">
```

### Fade in new content
```css
.new-item.htmx-added { opacity: 0; }
.new-item { transition: opacity 300ms; }
```

### Request in-flight dimming
```css
form.htmx-request { opacity: 0.5; transition: opacity 300ms; }
```

### View Transitions
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

## Web Components (Shadow DOM)

```js
class MyComponent extends HTMLElement {
  connectedCallback() {
    const root = this.attachShadow({ mode: 'open' });
    root.innerHTML = `
      <button hx-get="/data" hx-target="find .output">Load</button>
      <div class="output"></div>
    `;
    htmx.process(root);   // required!
  }
}
customElements.define('my-component', MyComponent);
```

`hx-target` and selectors only see elements within the same Shadow DOM. Use `host` to target the host element. Use `global` prefix to select from main document.

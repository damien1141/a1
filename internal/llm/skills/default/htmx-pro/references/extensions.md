# htmx Extensions

htmx 2.x moved SSE/WebSocket from core attributes (`hx-ws`/`hx-sse`) to extensions. Enable with `hx-ext` on `<body>` (or per-element).

## SSE (Server-Sent Events)

Real-time, uni-directional server-to-client streaming.

```html
<body hx-ext="sse">
  <div sse-connect="/events">
    <div sse-swap="message">Waiting for messages…</div>
    <div sse-swap="notification">Waiting for notifications…</div>
  </div>
</body>
```

### Attributes

| Attribute | Description |
|-----------|-------------|
| `sse-connect="<url>"` | Establish SSE connection |
| `sse-swap="<event-name>"` | Swap content when named event arrives |
| `sse-close="<event-name>"` | Close connection on event |
| `hx-trigger="sse:<event>"` | Trigger htmx request on SSE event |

### Named events

```html
<div sse-connect="/events">
  <div sse-swap="userUpdate">…</div>                <!-- swap on named event -->
  <div hx-get="/data" hx-trigger="sse:refresh">…</div>  <!-- trigger GET on SSE event -->
</div>
```

### Swap strategies with SSE

```html
<div sse-swap="message" hx-swap="beforeend">       <!-- append instead of replace -->
</div>
```

### Closing the connection

```html
<div sse-connect="/events" sse-close="complete">   <!-- closes when server sends "complete" -->
</div>
```

### Events

- `htmx:sseOpen`, `htmx:sseError`, `htmx:sseBeforeMessage` (cancelable), `htmx:sseMessage`, `htmx:sseClose` (reasons: `nodeMissing`/`nodeReplaced`/`message`).

### Server-side (HTTP)

```
event: userUpdate
data: <div>New user data</div>

event: notification
data: <span class="badge">3 new</span>
```

Set `Content-Type: text/event-stream`. Each `data:` line ends with `\n\n`.

## WebSocket

Bi-directional communication.

```html
<body hx-ext="ws">
  <div ws-connect="/chat">
    <div id="messages">…</div>           <!-- incoming messages swapped by element id (OOB) -->
    <form ws-send>                        <!-- send form data as JSON on submit -->
      <input name="message">
      <button>Send</button>
    </form>
  </div>
</body>
```

### Attributes

| Attribute | Description |
|-----------|-------------|
| `ws-connect="<url>"` | Establish WebSocket connection |
| `ws-send` | Send form data as JSON on submit |

### How it works

- **Sending:** `ws-send` serializes the nearest form's inputs as JSON and sends to the server.
- **Receiving:** Incoming HTML messages are parsed and swapped via OOB (matching element `id`s).

### URL prefixes

```html
<div ws-connect="ws://example.com/chat">    <!-- explicit ws -->
<div ws-connect="wss://example.com/chat">   <!-- explicit wss -->
<div ws-connect="/chat">                    <!-- auto: wss for https, ws for http -->
```

### Configuration

- `htmx.config.wsReconnectDelay` — reconnect strategy (default: `full-jitter`).
- `htmx.config.wsBinaryType` — binary data type (default: `blob`).
- `htmx.createWebSocket` — factory function override.

### Events

`htmx:wsConnecting`, `htmx:wsOpen`, `htmx:wsClose`, `htmx:wsError`, `htmx:wsBeforeMessage` (cancelable), `htmx:wsAfterMessage`, `htmx:wsConfigSend`, `htmx:wsBeforeSend`, `htmx:wsAfterSend`.

## Idiomorph (DOM morphing)

DOM morphing swap strategy. Reuses existing nodes for smoother transitions and preserved state (scroll position, focus, form input).

```html
<body hx-ext="morph">
  <div hx-get="/content" hx-swap="morph">
    <!-- Content morphed instead of replaced -->
  </div>
</body>
```

### Swap strategies

```html
<div hx-swap="morph">              <!-- morph target + children (outerHTML style) -->
<div hx-swap="morph:outerHTML">    <!-- same as above -->
<div hx-swap="morph:innerHTML">    <!-- morph only children, keep target -->
```

### When to use

- Smooth transitions without losing DOM state (scroll, focus, form input).
- Complex nested structures that benefit from minimal DOM changes.
- Pairs well with polling or SSE for live-updating UIs.

## Head Support

Manage `<head>` content from htmx responses.

```html
<head hx-ext="head-support">
  <title>My App</title>
  <link rel="stylesheet" href="/styles.css">
</head>
```

### Behavior

- **Boosted requests:** Merge algorithm — keeps matches, adds new, removes old.
- **Non-boosted requests:** Appends new head content only.

### Override per-request

```html
<head hx-head="merge">    <!-- force merge -->
<head hx-head="append">   <!-- force append -->
```

### Per-element control

```html
<script src="/app.js" hx-head="re-eval">    <!-- re-execute on every request -->
<link rel="stylesheet" hx-preserve="true">  <!-- never remove -->
```

### Events

`htmx:beforeHeadMerge`, `htmx:afterHeadMerge`, `htmx:removingHeadElement` (cancelable), `htmx:addingHeadElement` (cancelable).

## Response Targets

Route responses to different swap targets based on HTTP status code.

```html
<body hx-ext="response-targets">
  <form hx-post="/api/submit"
        hx-target="#success"
        hx-target-422="#form-errors"
        hx-target-5*="#server-error"
        hx-target-error="#error-container">
    …
  </form>
</body>
```

### Attribute syntax

| Attribute | Matches |
|-----------|---------|
| `hx-target-404` | Exactly 404 |
| `hx-target-4*` | 400-499 |
| `hx-target-40*` | 400-409 |
| `hx-target-*` | Any non-2xx/3xx |
| `hx-target-error` | Any 4xx or 5xx |

Wildcard resolution: most specific first: `404` → `40*` → `4*` → `*`. Use `x` instead of `*` if tooling doesn't support asterisks: `hx-target-4xx`.

### Configuration

- `responseTargetPrefersRetargetHeader` (default: `true`) — `HX-Retarget` header overrides.
- `responseTargetUnsetsError` (default: `true`) — clears `isError` for matched errors.
- `responseTargetPrefersExisting` (default: `false`) — pre-existing targets take precedence.

## Preload

Preload content before the user clicks for near-instant page loads.

```html
<body hx-ext="preload">
  <a href="/page" preload>Fast Link</a>
  <button hx-get="/data" preload="mouseover">Hover to Preload</button>
</body>
```

### Trigger modes

| Value | Behavior |
|-------|----------|
| `mousedown` | Load on mouse press (default, ~100-200ms head start) |
| `mouseover` | Load on hover (100ms debounce) |
| `custom-event` | Load on custom event |
| `always` | Re-preload on every trigger (not just once) |

### Images

```html
<a href="/page" preload preload-images="true">
  <!-- Also preloads images found in the response -->
</a>
```

### Immediate preloading

```html
<a href="/page" preload="preload:init">
  <!-- Preload immediately on page load -->
</a>
```

Only works with GET requests. Server responses must include `Cache-Control` headers for browser caching.

## htmx 1.x Compatibility

Bridge extension for migrating from htmx 1.x to 2.x.

```html
<script src="htmx.js"></script>
<script src="htmx-1-compat.js"></script>
```

### What it restores

- `hx-ws` and `hx-sse` attributes (replaced by extensions in v2).
- Old-style `hx-on` attribute (replaced by `hx-on*` wildcard in v2).
- `scrollBehavior` default `'smooth'` (v2 uses `'instant'`).
- DELETE requests use form-encoded body (v2 uses URL parameters).
- Cross-domain requests allowed by default (v2 blocks them).

### What it does NOT cover

- IE11 support (dropped in htmx 2).
- Extension `swap` method API changes.

## Building Custom Extensions

```js
htmx.defineExtension('my-extension', {
  // Called once when the extension is initialized
  init(api) { /* api provides internal htmx methods */ },

  // Return additional CSS selectors for htmx to process
  getSelectors() { return ['[my-attr]']; },

  // Called on every htmx event
  onEvent(name, evt) {
    if (name === 'htmx:beforeRequest') { /* modify request */ }
  },

  // Transform response text before processing
  transformResponse(text, xhr, elt) { return text.toUpperCase(); },

  // Declare custom swap styles
  isInlineSwap(swapStyle) { return swapStyle === 'my-swap'; },

  // Handle custom swap
  handleSwap(swapStyle, target, fragment, settleInfo) {
    if (swapStyle === 'my-swap') {
      // custom swap logic
      return [];  // return settled elements
    }
  },

  // Custom parameter encoding
  encodeParameters(xhr, parameters, elt) {
    xhr.setRequestHeader('Content-Type', 'application/json');
    xhr.overrideMimeType('text/json');
    return JSON.stringify(parameters);
  },
});
```

### Using the extension

```html
<div hx-ext="my-extension">
  <button hx-get="/data">Load</button>
</div>
```

### Naming convention

Dash-separated, short, descriptive names (e.g., `json-enc`, `class-tools`, `response-targets`).

## Quick Reference

| Extension | Purpose | Setup |
|-----------|---------|-------|
| `sse` | Server-Sent Events | `<body hx-ext="sse">` + `sse-connect` / `sse-swap` |
| `ws` | WebSocket | `<body hx-ext="ws">` + `ws-connect` / `ws-send` |
| `morph` | Idiomorph DOM morphing | `<body hx-ext="morph">` + `hx-swap="morph"` |
| `head-support` | Manage `<head>` | `<head hx-ext="head-support">` |
| `response-targets` | Status-code routing | `<body hx-ext="response-targets">` + `hx-target-4*` etc. |
| `preload` | Preload on hover/mousedown | `<body hx-ext="preload">` + `preload` attribute |
| `path-deps` | Trigger on path match | `<body hx-ext="path-deps">` + `path-deps="/path"` |
| `htmx-1-compat` | Bridge from v1 | `<script src="htmx-1-compat.js">` after htmx |
| Custom | `htmx.defineExtension(name, { … })` | `<div hx-ext="name">` |

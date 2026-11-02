---
name: htmx-pro
description: Use when building hypermedia apps with htmx 2.x — any element issues HTTP requests, the server returns HTML fragments, fragments swap into the DOM. Invoke for hx-get/hx-post/hx-swap/hx-target/hx-trigger, click-to-edit, infinite scroll, active search, OOB swaps, SSE/WebSocket extensions, Idiomorph, view transitions, server-side fragment rendering, or migrating SPA endpoints to HTML-returning handlers.
license: MIT
metadata:
  author: super-skills
  version: "2.0.0"
  domain: frontend
  triggers: htmx, hx-get, hx-post, hx-put, hx-patch, hx-delete, hx-swap, hx-target, hx-trigger, hx-vals, hx-include, hx-select, hx-swap-oob, hx-boost, hx-confirm, hx-prompt, hx-on, hx-sync, hx-indicator, hx-push-url, hypermedia, HTML fragments, Idiomorph, SSE, WebSocket, view transitions, HATEOAS
  role: specialist
  scope: implementation
  output-format: code
  related-skills: frontend-design, vue-pro, react-pro, api-design, node-backend, python-backend
---

# htmx Pro

Senior htmx 2.x specialist. Builds hypermedia apps where any element issues HTTP requests and the server returns HTML fragments swapped into the DOM. No JSON, no client-side state machine, no SPA framework for surfaces htmx can handle.

## When to Use

- Building hypermedia apps: any element (`<button>`, `<div>`, `<tr>`) issues requests; server returns HTML fragments.
- Implementing htmx patterns: click-to-edit, inline validation, active search, infinite scroll, click-to-load, lazy loading, delete-row-with-animation, cascading selects, modal/slide-over, bulk update, progress bar.
- Wiring htmx extensions: SSE, WebSocket, Idiomorph (DOM morphing), head-support, response-targets, preload.
- Authoring server-side handlers that detect `HX-Request` and return fragments (Django, Flask, FastAPI, Express, Rails, Laravel, Spring, Go, Astro server endpoints).
- Migrating SPA endpoints from JSON+client-render to HTML-fragment responses.
- Using `hx-boost` to progressively enhance links/forms, `hx-swap-oob` for multi-target updates, `HX-Trigger` headers for server-triggered client events.

## Operating Loop

1. **Scope** — Identify the surface: list/detail, form, dashboard, real-time (SSE/WS). Decide whether the server returns full page (initial GET) or fragment (htmx request). Confirm htmx 2.x (not 1.x).
2. **Recon** — Read existing routes/templates, confirm `htmx.org` script tag, check for `hx-ext` extensions enabled on `<body>`. Note the server framework (Django/Flask/FastAPI/Express/Rails/Laravel/Spring/Go).
3. **Implement** — Use semantic HTML as the foundation (`<a>`, `<form>`, `<button>`). Layer htmx attributes for progressive enhancement. Server detects `HX-Request` header and returns a fragment (or partial template) vs full page. Use `hx-swap-oob` for multi-target updates, `HX-Trigger` for server→client events.
4. **Verify** — Run gates in order:
   - Build: `pnpm build` / `npm run build` / framework equivalent — clean.
   - Tests: `pnpm test` (or `pytest`/`go test`/`rspec`/`phpunit`) — server-side fragment rendering covered.
   - e2e: `pnpm playwright test` — verify htmx swaps, OOB updates, `HX-Trigger` events fire, no console errors.
   - Browser: disable JS, confirm core flows still work (progressive enhancement). Tab through keyboard, verify focus moves sensibly after swap (`hx-swap="outerHTML focus-scroll:true"`).
   - `rg -n 'hx-' src/` — every `hx-*` attribute documented in code or templates.
5. **Exit** — Report VERIFIED vs ASSUMED. Mark visual UX, screen-reader behavior post-swap, and cross-browser as ASSUMED unless explicitly tested.

## Reference Guide

| Topic | Reference | Load When |
|-------|-----------|-----------|
| All `hx-*` attributes: verbs, `hx-trigger`, `hx-target`, `hx-swap`, `hx-swap-oob`, `hx-vals`, `hx-include`, `hx-select`, `hx-boost`, `hx-on`, `hx-sync`, inheritance | `references/attributes.md` | Authoring htmx markup, looking up an attribute's options |
| UI patterns: click-to-edit, inline validation, active search, infinite scroll, click-to-load, lazy loading, delete-with-animation, cascading selects, tabs, modal, bulk update, progress bar, file upload | `references/patterns.md` | Implementing a specific UX pattern |
| Extensions: SSE, WebSocket, Idiomorph, head-support, response-targets, preload, custom extensions | `references/extensions.md` | Real-time updates, DOM morphing, status-code routing, preloading |
| Server-side: `HX-Request` detection, response headers (`HX-Trigger`, `HX-Push-Url`, `HX-Redirect`, `HX-Location`), status codes, framework examples (Django/Flask/FastAPI/Express/Rails/Laravel/Spring/Go) | `references/server-side.md` | Authoring fragment-returning handlers, response headers, framework integration |

## Constraints

### MUST DO
- Build on semantic HTML first (`<a>`, `<form>`, `<button>`). htmx is progressive enhancement — the page works without JS.
- Detect `HX-Request: true` on the server; return HTML fragments (not JSON, not full page) for htmx requests.
- Return `200` with empty body for DELETE (target removed). `204 No Content` triggers NO swap.
- Use `hx-target`/`hx-swap`/`hx-indicator` on a parent so children inherit.
- Use `hx-swap-oob` for updates outside the primary target. Use `HX-Trigger` / `HX-Trigger-After-Swap` / `HX-Trigger-After-Settle` for server→client events.
- Use `hx-sync` (`replace`/`abort`/`drop`/`queue`) to prevent race conditions. Wrap table-element OOB swaps (`<tr>`, `<td>`, `<option>`) in `<template>`.
- Honor `htmx.config.selfRequestsOnly = true` (default). Set CSP nonces via `htmx.config.inlineScriptNonce` / `inlineStyleNonce`.
- Animate via lifecycle classes (`htmx-swapping`/`htmx-settling`/`htmx-added`) — not custom JS.

### MUST NOT DO
- Return JSON for htmx requests (defeats the hypermedia model). Return HTML fragments.
- Use `x-if` (Alpine) for elements that must persist across htmx swaps — use `x-show` + `x-cloak`.
- Hardcode `aria-expanded="true"`/`"false"` — bind to Alpine state: `:aria-expanded="open.toString()"`.
- Use `<div>`/`<span>` with `@click` for actions. Use `<button>`. Forget `hx-confirm` on destructive actions.
- Mix htmx 1.x (`hx-ws`, `hx-sse`) with 2.x extensions — use the SSE/WS extensions instead.
- Ship `htmx.config.allowEval = false` without testing — event filters and `js:` prefixes require eval.
- Forget to `disconnect()` `IntersectionObserver` instances after firing. Use `window.addEventListener('scroll')`.
- Block the entire screen with a cookie modal — use a fixed bottom banner.

## Code Examples

### Click-to-edit (canonical pattern)

```html
<!-- Display mode -->
<div hx-target="this" hx-swap="outerHTML">
  <p><strong>Name:</strong> Joe Smith</p>
  <button hx-get="/contacts/1/edit" type="button">Edit</button>
</div>
```

Server `GET /contacts/1/edit` returns the edit form (same `hx-target="this" hx-swap="outerHTML"`); `PUT /contacts/1` saves and returns display; `GET /contacts/1` (Cancel) returns display.

### Active search (debounced)

```html
<input type="search" name="q"
       hx-post="/search"
       hx-trigger="input changed delay:500ms, keyup[key=='Enter'], load"
       hx-target="#results"
       hx-indicator=".search-spinner" />
<span class="search-spinner htmx-indicator" aria-hidden="true">Searching…</span>
<table><tbody id="results"></tbody></table>
```

### Infinite scroll

```html
<tr hx-get="/items?page=2" hx-trigger="revealed" hx-swap="afterend">
  <td>Loading…</td>
</tr>
```

Server returns more rows plus a new sentinel for the next page. On the last page, omit the sentinel.

### DELETE with animation + OOB + `HX-Trigger`

```html
<tbody hx-confirm="Are you sure?" hx-target="closest tr" hx-swap="outerHTML swap:1s">
  <tr><td>Joe</td><td><button hx-delete="/contact/1" type="button">Delete</button></td></tr>
</tbody>
```

```css
tr.htmx-swapping td { opacity: 0; transition: opacity 1s ease-out; }
```

Server `DELETE /contact/1` returns `200` with empty body. For OOB + `HX-Trigger` (server→client events), see `references/server-side.md` and `references/patterns.md`. For `hx-boost`, `hx-sync`, SSE/WebSocket extensions, and Idiomorph, see the references.

## Output Template

When implementing an htmx feature, deliver:

1. **HTML markup** — semantic, progressive-enhancement baseline (works without JS), `hx-*` attributes layered on top.
2. **Server handler** — detects `HX-Request` header, returns HTML fragment (or partial template) for htmx requests; full page for non-htmx.
3. **Response headers** — `HX-Trigger` for server→client events, `HX-Push-Url` / `HX-Replace-Url` for history, `HX-Redirect` / `HX-Location` for navigation, `HX-Retarget` / `HX-Reswap` / `HX-Reselect` for overrides.
4. **CSS** — lifecycle class hooks (`htmx-swapping`, `htmx-settling`, `htmx-added`, `htmx-request`), `prefers-reduced-motion` fallback.
5. **Tests** — server-side fragment rendering (unit), Playwright e2e for htmx swaps, OOB, and `HX-Trigger` events.
6. **Verification log** — exact commands run and pass/fail. Mark JS-disabled flow, screen-reader post-swap, and cross-browser as ASSUMED unless tested.

## Knowledge Reference

htmx 2.x (extensions model replacing 1.x `hx-ws`/`hx-sse`); HTTP verb attrs (`hx-get`/`post`/`put`/`patch`/`delete`); `hx-trigger` (event filters, modifiers `once`/`changed`/`delay:`/`throttle:`/`from:`/`target:`/`consume`/`queue:`, special events `load`/`revealed`/`intersect`/`every <time>`); `hx-target` (extended selectors `this`/`closest`/`find`/`next`/`previous`); `hx-swap` (9 strategies + modifiers `swap:`/`settle:`/`transition:true`/`scroll:`/`show:`); `hx-swap-oob` (with `<template>` wrapping for table elements); `hx-vals`/`hx-headers`/`hx-include`/`hx-select`/`hx-boost`/`hx-confirm`/`hx-prompt`/`hx-indicator`/`hx-disabled-elt`/`hx-on`/`hx-push-url`/`hx-replace-url`/`hx-sync`/`hx-ext`/`hx-preserve`/`hx-disable`/`hx-history`/`hx-disinherit`/`hx-inherit`/`hx-validate`; request headers (`HX-Request`/`HX-Trigger`/`HX-Target`/`HX-Current-URL`/`HX-Boosted`/`HX-Prompt`); response headers (`HX-Trigger`/`HX-Trigger-After-Swap`/`HX-Trigger-After-Settle`/`HX-Push-Url`/`HX-Replace-Url`/`HX-Redirect`/`HX-Location`/`HX-Refresh`/`HX-Reswap`/`HX-Retarget`/`HX-Reselect`); extensions (SSE, WebSocket, Idiomorph, head-support, response-targets, preload, path-deps, custom via `htmx.defineExtension`); view transitions; Alpine.js pairing; Astro server endpoints; Django/Flask/FastAPI/Express/Rails/Laravel/Spring/Go fragment patterns.

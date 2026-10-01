# Accessibility (WCAG 2.1 AA) + Semantic HTML + SEO

Compliance, accessibility, and SEO rules embedded directly in markup. Output code with these rules enforced — not an audit report.

## Landmarks (one of each per page)

```html
<!doctype html>
<html lang="en">
  <head>
    <meta charset="utf-8" />
    <meta name="viewport" content="width=device-width, initial-scale=1" />
    <title>…</title>
    <meta name="description" content="…" />
    <script type="application/ld+json">
      {
        "@context": "https://schema.org",
        "@type": "HVACBusiness",            // specific type, not LocalBusiness
        "name": "Harrison Roofing",
        "telephone": "+1-312-847-1928",
        "address": {
          "@type": "PostalAddress",
          "addressLocality": "Leeds",
          "addressRegion": "AL",
          "postalCode": "35094"
        },
        "areaServed": "Leeds, AL + 25mi",
        "licenseNumber": "AL-12345"
      }
    </script>
  </head>
  <body>
    <a href="#main" class="sr-only focus:not-sr-only focus:absolute focus:top-2 focus:left-2 bg-surface px-4 py-2 rounded">Skip to content</a>
    <header>…nav…</header>
    <main id="main">…content…</main>
    <footer>…NAP, license, legal links…</footer>
  </body>
</html>
```

**Exactly one `<h1>` per page. No skipped heading levels** (e.g., `<h2>` followed by `<h4>` is a fail).

## `<html lang>` (always)

Missing `lang` causes screen readers to mispronounce text. Pick the right code:
```html
<html lang="en">
<html lang="es">
<html lang="zh-Hans">
```

## Heading hierarchy

```html
<h1>Page title — one per page</h1>
  <h2>Section A</h2>
    <h3>Subsection</h3>
  <h2>Section B</h2>
    <h3>Subsection</h3>
      <h4>Detail</h4>
```

## Forms (labels, autocomplete, types, honeypot)

```html
<form @submit.prevent="submit" aria-live="polite" x-data="{ submitted: false, submitting: false }">
  <!-- Honeypot: visually hidden text input, NOT type="hidden" -->
  <input type="text" name="_gotcha" class="sr-only" tabindex="-1" autocomplete="off" aria-hidden="true" />

  <div>
    <label for="name">Name</label>
    <input id="name" name="name" type="text" required autocomplete="name" />
  </div>

  <div>
    <label for="email">Email</label>
    <input id="email" name="email" type="email" required autocomplete="email" />
  </div>

  <div>
    <label for="phone">Phone</label>
    <input id="phone" name="phone" type="tel" autocomplete="tel" />
  </div>

  <button type="submit" :disabled="submitting">
    <span x-show="!submitting">Get a quote</span>
    <span x-show="submitting">Sending…</span>
  </button>

  <template x-if="submitted">
    <p role="status">Thanks — we'll reply within 48 hours.</p>
  </template>
</form>
```

Rules:
- `<label for="id">` matches `<input id="id">` exactly. Floating labels still need this.
- `type="email"` / `type="tel"` triggers the right mobile keyboard.
- `autocomplete="email|tel|name|street-address|postal-code"` on every applicable input.
- Honeypot is `type="text"` + `class="sr-only"` (NOT `type="hidden"` — bots ignore hidden).
- Submit button disables (`:disabled="submitting"`) and shows "Sending…" while pending.
- `aria-live="polite"` on the form container so screen readers announce success/error.
- Success message inside `<template x-if="submitted">` so it leaves the accessibility tree when not active.

## Mobile menu / modal (a11y)

```html
<button
  @click="open = true"
  :aria-expanded="open.toString()"
  aria-controls="mobile-menu"
  aria-label="Open menu"
  class="md:hidden"
>☰</button>

<div
  x-show="open"
  x-cloak
  id="mobile-menu"
  role="dialog"
  aria-modal="true"
  aria-label="Site menu"
  @keydown.escape="open = false"
  class="fixed inset-0 z-50 bg-surface"
>
  <button @click="open = false" aria-label="Close menu" class="absolute top-2 right-2">×</button>
  <nav>…links…</nav>
</div>
```

Rules:
- `aria-expanded` is dynamically bound to state, never hardcoded.
- `aria-controls="mobile-menu"` matches the menu's `id`.
- `role="dialog"` + `aria-modal="true"` on the menu container.
- `@keydown.escape="open = false"` lets keyboard users close it.
- Body scroll lock: `:class="{ 'overflow-hidden': open }"` on `<body>`.
- Icon-only buttons need `aria-label`.
- Use `x-show` + `x-cloak`, never `x-if` for the menu — `x-if` destroys DOM and breaks screen reader state.

## Cookie consent (GDPR / CCPA)

```html
<div
  x-data="{ consentGiven: localStorage.getItem('cookieConsent') === 'true', accept() { localStorage.setItem('cookieConsent', 'true'); this.consentGiven = true; }, decline() { localStorage.setItem('cookieConsent', 'false'); this.consentGiven = true; } }"
  x-show="!consentGiven"
  x-cloak
  class="fixed bottom-0 inset-x-0 bg-surface-elevated border-t p-4 flex flex-col sm:flex-row gap-3 items-center justify-between"
  role="region"
  aria-label="Cookie consent"
>
  <p class="text-sm text-text-muted">We use cookies for analytics. No tracking until you accept.</p>
  <div class="flex gap-3 w-full sm:w-auto">
    <button @click="accept()" class="flex-1 sm:flex-none rounded bg-accent px-4 py-2 text-accent-contrast">Accept</button>
    <button @click="decline()" class="flex-1 sm:flex-none rounded border border-black/40 px-4 py-2">Decline</button>
  </div>
</div>
```

Required global CSS:
```css
[x-cloak] { display: none !important; }
```

Banned:
- Modals that block the entire screen for consent.
- `x-if` for the banner (destroys DOM).
- Declared `function myComponent()` in a scoped `<script>` — Astro scopes scripts; Alpine can't find it on `window`.
- Only "Accept" — both Accept and Decline must be present and equally visible.

## Color & contrast (WCAG AA)

- Body text on bg: ≥ 4.5:1.
- Large text (≥ 24px regular or ≥ 18.66px bold) on bg: ≥ 3:1.
- UI components (input borders, icon boundaries) on adjacent: ≥ 3:1.
- `--text-muted` on light bg should be equivalent to `text-gray-600` or darker. `text-gray-400` fails.
- `--text-muted` on dark bg should be equivalent to `text-gray-300` or lighter.
- Input borders: at least `border-black/40` (light) or `border-white/40` (dark). `border-black/10` fails.
- Light accent button → dark button text. White on light-blue fails AA.

Verify with `@axe-core/cli` or Lighthouse.

## Focus states

Every interactive element needs a visible focus indicator:
```css
:focus-visible {
  outline: 2px solid var(--color-accent);
  outline-offset: 2px;
}
/* OR Tailwind: */
.focus-visible:ring-2.focus-visible:ring-accent.focus-visible:ring-offset-2
```

Never `outline-none` without a replacement ring. Exception: visually-hidden `sr-only peer` checkboxes paired with a visible focusable sibling (the bespoke haptic pattern).

## Skip link (always first child of body)

```html
<a href="#main" class="sr-only focus:not-sr-only focus:absolute focus:top-2 focus:left-2 z-50 bg-surface px-4 py-2 rounded">Skip to content</a>
```

## `prefers-reduced-motion`

Every animation must degrade to static:
```css
@media (prefers-reduced-motion: reduce) {
  *, *::before, *::after {
    animation-duration: 0.01ms !important;
    animation-iteration-count: 1 !important;
    transition-duration: 0.01ms !important;
    scroll-behavior: auto !important;
  }
}
```

## Footer (legal, NAP, SEO)

```html
<footer class="border-t mt-24 py-12 text-sm text-text-muted">
  <div class="max-w-6xl mx-auto px-6 grid gap-8 md:grid-cols-3">
    <div>
      <p class="font-semibold text-text-main">Harrison Roofing LLC</p>
      <p>123 Main St, Leeds, AL 35094</p>
      <p><a href="tel:+13128471928" class="underline">+1 (312) 847-1928</a></p>
      <p>AL License #12345</p>
    </div>
    <nav aria-label="Legal">
      <ul class="space-y-2">
        <li><a href="/privacy">Privacy Policy</a></li>
        <li><a href="/terms">Terms of Service</a></li>
      </ul>
    </nav>
    <p>© {new Date().getFullYear()} Harrison Roofing LLC.</p>
  </div>
</footer>
```

Footer links (`/privacy`, `/terms`) MUST resolve to real pages. In a static Astro build, dangling links 404 — create stub pages.

## Quick Reference

| Concern | Rule |
|---------|------|
| `<html lang>` | Always set; default `en` |
| Landmarks | Exactly one `<main>`, `<header>`, `<footer>` |
| Headings | One `<h1>`, no skipped levels |
| Skip link | First child of `<body>` |
| Forms | `<label for>` + matching `id`, `type=email/tel`, `autocomplete`, honeypot as `type=text sr-only` |
| Submit | `:disabled` while pending, `aria-live` on form, `<template x-if>` for success |
| Modal/menu | `role="dialog"`, `aria-modal="true"`, `@keydown.escape`, `aria-expanded` bound, body scroll lock |
| Cookie banner | Inline `x-data`, `x-show`+`x-cloak`, Accept + Decline equally visible |
| Contrast | 4.5:1 body, 3:1 UI components |
| Focus | `focus-visible:ring-2` on every interactive element |
| Motion | Honor `prefers-reduced-motion` |
| Footer | NAP, license #, Privacy + Terms links (resolving), dynamic year |
| JSON-LD | Specific `@type` (HVACBusiness, Plumber, Electrician), full address, licenseNumber |

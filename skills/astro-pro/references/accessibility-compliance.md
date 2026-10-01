# Accessibility & Compliance

WCAG 2.1 AA, SEO schema, form/cookie compliance — adapted from `web-design-guidelines`. Embed these rules directly into the HTML/Alpine.js you write. You do not output an audit report; you output code with these rules strictly enforced.

## Cookie Consent (GDPR / CCPA)

- **BAN:** Modals that block the entire screen. Use a fixed bottom banner.
- **BAN:** `x-if` for the cookie banner — destroys the DOM and breaks screen reader state. Use `x-show` with `x-cloak`.
- **BAN:** Declaring `function myComponent()` in a scoped `<script>` and calling it via `x-data` — Astro scopes scripts by default; Alpine cannot find it on `window`.
- **REQUIRE:** Global CSS for `x-cloak`: `[x-cloak] { display: none !important; }` in `Layout.astro`.
- **REQUIRE:** Inline `x-data` object on the banner div. Canonical string:

```astro
<div
  x-data="{ consentGiven: localStorage.getItem('cookieConsent') === 'true', accept() { localStorage.setItem('cookieConsent', 'true'); this.consentGiven = true; }, decline() { localStorage.setItem('cookieConsent', 'false'); this.consentGiven = true; } }"
  x-show="!consentGiven"
  x-cloak
  role="region"
  aria-label="Cookie consent"
  class="fixed bottom-0 inset-x-0 z-50 bg-surface border-t-2 border-text-main p-4"
>
  <p class="text-text-main">We use cookies for analytics. Accept to enable, decline to opt out.</p>
  <div class="flex gap-3 mt-3">
    <button @click="accept()" class="px-4 py-2 bg-accent text-white">Accept</button>
    <button @click="decline()" class="px-4 py-2 border-2 border-text-main text-text-main">Decline</button>
  </div>
</div>
```

- **REQUIRE:** BOTH Accept and Decline buttons present and equally visible. No dark patterns.

## Async Forms & Input Logic

- **BAN:** Standard form POSTs (`method="POST"` without JS). Use Alpine `@submit.prevent`.
- **BAN:** `type="text"` for email/phone. Use `type="email"` and `type="tel"` for correct mobile keyboards.
- **BAN:** `<input type="hidden" name="_gotcha">` for honeypot — bots ignore hidden fields. Use:
  ```astro
  <input type="text" name="_gotcha" class="sr-only" tabindex="-1" autocomplete="off" />
  ```
- **REQUIRE:** `autocomplete` attributes on all inputs (`autocomplete="tel"`, `autocomplete="email"`, `autocomplete="name"`).
- **REQUIRE:** `required` on mandatory inputs.
- **REQUIRE:** Alpine state MUST track `submitting` and `submitted` on the `<form>` tag: `x-data="{ submitted: false, submitting: false }"`.
- **REQUIRE:** Submit button MUST disable (`:disabled="submitting"`) and show "Sending…" while submitting.
- **REQUIRE:** `fetch()` to the action URL. On success set `submitted=true`; on error set `submitting=false`.
- **REQUIRE:** Success message inside `<template x-if="submitted">` so it's removed from the a11y tree when not active.
- **REQUIRE:** `aria-live="polite"` on the form container so screen readers announce state.

```astro
<form
  x-data="{ submitted: false, submitting: false }"
  @submit.prevent="
    submitting = true;
    fetch('https://formspree.io/f/XXXX', {
      method: 'POST',
      headers: { 'Accept': 'application/json' },
      body: new FormData($event.target),
    })
      .then((r) => { if (r.ok) { submitted = true; } else { submitting = false; } })
      .catch(() => { submitting = false; })
  "
  aria-live="polite"
>
  <label for="name" class="block font-heading text-sm uppercase tracking-widest">Name</label>
  <input id="name" name="name" type="text" autocomplete="name" required
    class="w-full mt-1 px-3 py-2 border-2 border-text-main/40 bg-surface" />

  <label for="email" class="block font-heading text-sm uppercase tracking-widest mt-4">Email</label>
  <input id="email" name="email" type="email" autocomplete="email" required
    class="w-full mt-1 px-3 py-2 border-2 border-text-main/40 bg-surface" />

  <input type="text" name="_gotcha" class="sr-only" tabindex="-1" autocomplete="off" />

  <button type="submit" :disabled="submitting"
    class="mt-6 px-6 py-3 bg-accent text-white font-heading uppercase tracking-widest">
    <span x-show="!submitting">Send message</span>
    <span x-show="submitting" x-cloak>Sending…</span>
  </button>

  <template x-if="submitted">
    <p class="mt-4 text-accent">Thanks. We'll reply within one business day.</p>
  </template>
</form>
```

## Legal, NAP & SEO (Local Business Trust)

- **REQUIRE:** Footer MUST contain exact NAP: Company Name, physical address, local phone number.
- **REQUIRE:** Trade license number visible in the footer (e.g., "MN License #12345").
- **REQUIRE:** Privacy Policy and Terms of Service links in the footer.
- **REQUIRE (route existence):** Footer links to `/privacy` and `/terms` MUST resolve to real pages. Create `src/pages/privacy.astro` and `src/pages/terms.astro` as minimal stub pages so routes resolve. Do NOT ship footer links that 404.
- **REQUIRE:** Dynamic copyright year: `{new Date().getFullYear()}` (Astro syntax).
- **REQUIRE:** `<html lang="en">` (or appropriate language code). Missing causes screen readers to mispronounce.
- **REQUIRE:** `<script type="application/ld+json">` in `<head>`. Use a specific `@type` (e.g., `HVACBusiness`, `Plumber`, `Electrician`, `Dentist`, `Restaurant` — not just `LocalBusiness`). Include `@context`, `name`, `telephone`, `address` (with `addressLocality`, `addressRegion`, `postalCode`), `areaServed`, `licenseNumber`.

```astro
---
const business = {
  name: "Harrison Roofing",
  telephone: "+1-312-555-0142",
  address: {
    "@type": "PostalAddress",
    addressLocality: "Chicago",
    addressRegion: "IL",
    postalCode: "60601",
  },
  areaServed: "Chicago metro",
  licenseNumber: "IL-ROOF-4821",
};
const jsonLd = { "@context": "https://schema.org", "@type": "RoofingContractor", ...business };
---
<script type="application/ld+json" set:html={JSON.stringify(jsonLd)} />
```

## WCAG 2.1 AA — Semantic & Interactive

- **BAN:** `<div>` or `<span>` with `@click` for actions. Use `<button>`.
- **BAN:** Hardcoded `aria-expanded` with any value. MUST bind to Alpine state: `:aria-expanded="open.toString()"`.
- **BAN:** Positive `tabindex` (e.g., `tabindex="1"`). Breaks natural keyboard navigation.
- **REQUIRE:** `aria-label` on ALL icon-only buttons (hamburger, close, scroll-top).
- **REQUIRE:** `aria-controls="mobile-menu"` on hamburger toggles, matching `id="mobile-menu"` on the menu div.
- **REQUIRE:** `role="dialog"` and `aria-modal="true"` on mobile menus/modals.
- **REQUIRE:** `@keydown.escape="open = false"` on mobile menu container.
- **REQUIRE:** Body scroll lock: `:class="{ 'overflow-hidden': open }"` on `<body>` when mobile menu open.
- **REQUIRE:** Every `<input>` MUST have a matching `<label for="id">`. Do not rely on placeholders. (Floating-label templates still need `<label for>` matching the input `id`.)
- **REQUIRE:** `alt=""` on decorative images/SVGs. Descriptive `alt` on content images.
- **REQUIRE:** Visible focus states. `focus-visible:ring-2` (or `focus:ring-2` for older browsers) on all links, buttons, inputs. NEVER `outline-none` without a replacement ring. (Exception: the Bespoke Haptic Checkbox uses `sr-only peer` + visible focusable sibling — not a focusable control, no ring needed.)
- **REQUIRE:** Skip link as absolute first element in `<body>`:
  ```astro
  <a href="#main" class="sr-only focus:not-sr-only focus:absolute focus:top-2 focus:left-2 bg-surface px-4 py-2 rounded">Skip to content</a>
  ```
- **REQUIRE:** Semantic landmarks: exactly one `<main>`, `<header>`, `<footer>`.
- **REQUIRE:** Logical heading hierarchy: exactly one `<h1>`. No skipped levels (`<h2>` → `<h4>` is a fail).
- **REQUIRE:** `prefers-reduced-motion` media query for all CSS animations and transitions (handled in `Layout.astro` `.reveal` CSS).

## Color & Contrast (WCAG AA math)

- **REQUIRE:** `--text-muted` token MUST pass 4.5:1 against `--surface` background. Avoid overly light grays (`text-gray-400` on white fails). Use `text-gray-600` or darker on light backgrounds.
- **REQUIRE:** `--text-muted` on dark backgrounds must be `text-gray-300` or lighter.
- **REQUIRE:** UI components (input borders, icon boundaries) must have 3:1 contrast against adjacent colors.
- **CRITICAL INPUT CONTRAST:** Interactive input borders MUST be at least `border-black/40` (or `border-white/40` on DARK themes) to pass the 3:1 UI component contrast rule. `border-black/10` / `border-white/10` is a WCAG failure. A faint `ring-1 ring-black/5` (or `ring-white/5` on dark) is acceptable for subtle *card* rings, but the *input* boundary that the user tabs to must be distinct (use `/40`).

Archetype tokens (verify against actual project values after token swap — see `design-archetypes.md`):

| Archetype | `--text-main` on `--surface` | `--text-muted` on `--surface` | `--accent` contrast |
|---|---|---|---|
| Industrial | zinc-900 on zinc-100 — passes 4.5:1 | zinc-600 on zinc-100 — passes 4.5:1 | white on amber-400 — verify; black on amber-400 passes |
| Luxury | #1C1917 on #FDFBF7 — passes | #57534E on #FDFBF7 — passes | #FDFBF7 on sage #7C8471 — verify; dark on sage passes |
| Tech (DARK) | #FAFAFA on #0F0F11 — passes | #9CA3AF on #0F0F11 — verify | DARK text on light blue (`#8AB4F8`); white on `#3B82F4` passes |
| Clinical | #0F172A on #FAFAFA — passes | #475569 on #FAFAFA — passes | white on teal #0D9488 — verify; dark on light teal passes |

If your archetype uses a LIGHT accent, button text MUST be DARK (`text-surface`). White-on-light-accent fails AA.

## Pre-Output Checklist

- [ ] Cookie banner uses inline `x-data` and `x-show` (not `x-if`).
- [ ] Form uses `@submit.prevent`, `fetch()`, `<template x-if="submitted">`, `aria-live="polite"`, `autocomplete` on every input.
- [ ] Honeypot is `type="text"` with `class="sr-only"`, NOT `type="hidden"`.
- [ ] All `aria-expanded` dynamically bound to `.toString()`.
- [ ] Mobile menu has `role="dialog"`, `aria-modal="true"`, `@keydown.escape`.
- [ ] JSON-LD uses a specific `@type` (e.g., `HVACBusiness`), not just `LocalBusiness`.
- [ ] Input borders meet the 3:1 contrast minimum (`border-black/40` / `border-white/40`).
- [ ] Skip link is the absolute first child of `<body>`.
- [ ] One `<h1>`, one `<main>`, one `<header>`, one `<footer>`.
- [ ] `prefers-reduced-motion` honored on every animation.

## Verification

```bash
# axe-core (CLI) — zero critical violations
npx @axe-core/cli http://localhost:4321 --tags wcag2a,wcag2aa

# Lighthouse — Accessibility ≥ 90
npx lighthouse http://localhost:4321 --only-categories=accessibility --view

# Manual: keyboard tab through every interactive element, focus visible
# Manual: VoiceOver / NVDA pass — landmarks announced, no orphaned @click divs
```

---
name: astro-pro
description: Use when building Astro 5+ sites — islands architecture, content collections (Content Layer API + zod), View Transitions, Actions, server islands, middleware, endpoints, Tailwind v4 via @theme, and the 110-file high-end visual design template library (Industrial/Luxury/Tech/Clinical archetypes). Generates .astro components, layouts, and integrations with lucide-astro + Alpine.js, anti-slop verified.
license: MIT
metadata:
  author: super-skills
  version: "2.0.0"
  domain: frontend
  triggers: Astro, .astro, islands architecture, content collections, View Transitions, Astro 5, design templates, Tailwind, lucide-astro, server islands, astro:actions, ClientRouter, middleware, Content Layer API, glob loader
  role: specialist
  scope: implementation
  output-format: code
  related-skills: react-pro, vue-pro, alpine-pro, frontend-design, htmx-pro
---

# Astro Pro

Astro 5+ framework specialist with a battle-tested 110-template visual-design library. Ships `.astro` components, content collections (Content Layer API + zod), server islands, View Transitions, Actions, middleware, endpoints, and Tailwind v4 — composed against four design archetypes (Industrial / Luxury / Tech / Clinical). Islands stay small; the page is mostly static HTML with zero JS by default.

## When to Use

- Building a new Astro 5+ site or migrating from Astro 4 / Next.js / Nuxt static.
- Authoring `.astro` components (frontmatter `---` script + JSX-like template + `define:vars` + slots).
- Defining content collections with the Content Layer API (`src/content.config.ts`, `glob()` loader, zod schemas, `getCollection`/`render()`).
- Adding interactivity via islands (`client:load`/`idle`/`visible`/`media`/`only`) with React / Vue / Svelte / Solid / Alpine.
- Wiring View Transitions (`ClientRouter`, `transition:name`, `transition:animate`, morph/slide/fade, persistence).
- Writing Actions (`astro:actions`, `defineAction`, `action`), middleware (`src/middleware.ts`, `context.locals`), or server endpoints (`export const GET/POST`).
- Generating design-led pages from the 110-template library — Industrial / Luxury / Tech / Clinical (see Recipe Index below).
- De-slopifying an Astro build (em dashes, AI buzzwords, gradient text, glassmorphism, fake stats).

## Operating Loop

1. **Scope** — Build mode (`static`/`server`/`hybrid`), output target (Node / Cloudflare / Vercel / Netlify / Deno), required islands (framework + hydration directive), content collections to define, and the **design archetype** (Industrial / Luxury / Tech / Clinical — see `references/design-archetypes.md`). Lock ONE archetype per page. Refuse to mix.
2. **Recon** — Read `astro.config.*`, `package.json` (Astro ≥ 5, integrations, package manager from lockfile), `src/content.config.ts`, `src/layouts/Layout.astro`, `src/styles/global.css` (Tailwind v4 `@import "tailwindcss"` + `@theme`), `src/middleware.ts` if present. Inventory existing `templates/*.md` matches for the task category.
3. **Implement** — Apply the **Template Loading Protocol**:
   1. Determine the task (button, hero, nav, section, form, card, footer, motion).
   2. Find the category in the Recipe Index below (or `ls templates/`).
   3. Read the template file — do NOT inline from memory.
   4. Apply the archetype spec (`references/design-archetypes.md`): swap structural tokens (`bg-accent`, `text-text-main`, `bg-surface`, `bg-surface-elevated`, `bg-surface-muted`, `text-text-muted`, `font-heading`, `font-mono`); enforce archetype cubic-bezier / radii / shadow / texture.
   5. Combine with the section shell (transparent `<section>` + `relative overflow-hidden` + `relative z-10 max-w-*` content) and mobile collapse rules.
   Then write `.astro` components (frontmatter script + JSX-like template, `class` not `className`, `define:vars`, `<slot/>`), collections in `src/content.config.ts`, Actions for typed server mutations, View Transitions via `<ClientRouter />`.
4. **Verify** — `astro check` (zero diagnostics); `astro build` (clean); `tsc --noEmit` (zero errors, `astro/tsconfigs/strict`); Lighthouse ≥ 90 all categories; `@axe-core/cli` (zero critical); `rg -n '[—–…]' src/` (zero em/en/ellipsis in rendered copy); `rg -niE '(seamless|elevate|robust|unlock|empower|streamline|supercharge|revolutionize|cutting-edge|next-level|bespoke|journey|synergy|paradigm|blazing-fast|lightning-fast|unparalleled|stunning|gorgeous|sleek|vibrant|intuitive|exquisite|timeless|luxurious)' src/` (zero banned buzzwords); `rg -niE 'backdrop-filter|bg-clip-text|text-transparent|@keyframes|animate-(spin|bounce|ping|pulse)' src/` (investigate every hit). Browser: keyboard tab order logical, focus visible.
5. **Exit** — Report VERIFIED (gates ran clean, build succeeds) vs ASSUMED (visual QA, cross-browser, real screen reader). Screenshot if available.

## Reference Guide

| Topic | Reference | Load When |
|-------|-----------|-----------|
| Astro 5 core: islands, collections, server islands, Actions, View Transitions, middleware, endpoints, env vars | `references/astro-core.md` | Building pages, islands, layouts, Actions, middleware, endpoints |
| Content Layer API: `glob()` loader, `src/content.config.ts`, zod schemas, `getCollection`, `render()` | `references/content-collections.md` | Defining collections, MDX/MD authoring, querying content |
| View Transitions: `ClientRouter`, `transition:name`, `transition:animate`, morph/slide/fade, persistence | `references/view-transitions.md` | SPA-like navigation, animated route changes, persisted UI state |
| Integrations: `@astrojs/react/vue/svelte/solid/mdx/node/tailwind`, `add()` CLI, compatibility | `references/integrations.md` | Installing framework adapters, SSR adapter, MDX, Tailwind v3 bridge |
| Performance: image optimization (`astro:assets`), script loading, partial hydration, caching, Astro DB | `references/performance.md` | Lighthouse tuning, image pipelines, script strategies, caching |
| Tailwind v4 in Astro: `@import "tailwindcss"`, `@theme`, `@layer`, Vite plugin, design tokens | `references/tailwind-v4.md` | Setting up Tailwind v4, token mapping, CSS-first config |
| Four archetypes (Industrial / Luxury / Tech / Clinical): color, texture, shadow, radii, motion | `references/design-archetypes.md` | Locking a page archetype, applying haptics, swapping tokens |
| Anti-slop rules: banned punctuation, words, phrases, CSS crimes, audit greps | `references/anti-slop-rules.md` | De-slopifying, copy rewriting, CSS cleanup, audit gauntlet |
| WCAG 2.1 AA, SEO schema, forms, cookie consent, NAP, JSON-LD | `references/accessibility-compliance.md` | Hardening a11y, legal/SEO compliance, forms, mobile menus |
| **110 design templates** (button / hero / nav / section / card / form / background / SVG / typography / badge / footer / motion) | `templates/*.md` | **110 template files; see Recipe Index below** |

## Recipe Index

Glob patterns relative to `templates/`. Slugs derive from H3 headings inside each file. Open the file before writing markup for any non-trivial component — do NOT inline from memory.

| Category | Glob | Notes |
|---|---|---|
| Buttons | `button-*.md` | across all 4 archetypes + universal |
| Canonical CTA | `e-the-cta-button-*.md` | single source-of-truth primary CTA |
| Nested CTA pill | `f-the-nested-cta-*.md` | Luxury/Tech pill w/ nested arrow |
| Eyebrow tag | `g-eyebrow-tag.md` | microscopic pill badge |
| Haptics — focal | `a-the-stamped-block-*.md`, `b-the-double-bezel-*.md` | Stamped (Industrial), Double-Bezel (Luxury/Tech) |
| Haptics — secondary | `c-the-standard-card-*.md` | Standard Card for grids |
| Checkbox / Diagnostic | `d-the-bespoke-checkbox-*.md`, `d2-the-diagnostic-component-*.md` | lead magnet |
| Hero variants | `h1-*.md` … `h5-*.md` | Editorial, Bento, Terminal, Cinematic, Stacked |
| Nav variants | `n1-*.md` … `n4-*.md` | Mega-menu, scroll-shrink, split bar, mobile sheet |
| Section layouts | `l1-*.md` … `l6-*.md` | Bento, alternating, timeline, stepper, sticky, marquee |
| Content components | `c1-*.md` … `c14-*.md` | Accordion, tabs, modal, slide-over, toast, carousel, before/after, matrix, pricing, stat, avatar, logo wall, terminal, code block |
| Forms | `f1-*.md` … `f5-*.md` | Floating label, textarea/select, multi-step, validation, form shell |
| Card variants | `v1-*.md` … `v6-*.md` | Glow, Glass, Editorial, Data, Quote, Pricing |
| Backgrounds | `b1-*.md` … `b6-*.md` | Mesh, conic, aurora, grain, dividers, faux thumbnail |
| SVG Textures | `svg-textures.md` | 25 JIT-safe archetype textures |
| Typography | `t1-*.md` … `t6-*.md` | Outline, gradient, split-color, vertical, kinetic, mono label |
| Badges & chips | `g1-*.md` … `g5-*.md` | Status, category, filter, icon, numeric |
| Footers | `u1-*.md` … `u4-*.md` | Multi-column, minimal CTA, sitemap, mega bento |
| Motion micro | `m1-*.md` … `m8-*.md` | Hover lift/glow/shift, stagger, count-up, marquee, typewriter, magnetic nudge |
| Motion core | `a-alpine-x-transition-*.md`, `b-scroll-reveal-*.md`, `c-hamburger-morph-*.md` | state transitions, reveal CSS, hamburger |
| Section mandates | `mandate-1-*.md`, `mandate-3-*.md`, `mandate-4-*.md`, `mandate-5-*.md` | layout rules w/ code |
| Atmospheric | `the-global-texture-layer-*.md`, `the-section-shell-*.md`, `oversized-background-typography-*.md` | global grid, section shell, ghost text |

## Constraints

### MUST DO
- Target Astro 5+. Use `astro:actions` (`defineAction`, `action`, `isActionError`) for typed server mutations — not deprecated REST endpoint conventions when an Action fits.
- Define content collections in `src/content.config.ts` using the Content Layer API: `glob({ pattern: '**/*.md', base: './src/content/posts' })` + zod schema. Query with `getCollection('posts')`; render with `render(entry)`.
- Use `class` (not `className`), `class:list` for conditional classes, `set:html` for raw HTML, `define:vars` for passing server values into a `<script>`.
- Add `<ClientRouter />` from `astro:transitions` in `<head>` to enable View Transitions. Annotate persisted elements with `transition:persist`; paired singletons with `transition:name="..."`.
- Tailwind v4: `@import "tailwindcss";` in `src/styles/global.css`, then `@theme { --color-*: ...; --font-*: ...; }`. No `tailwind.config.js` unless bridging a v3 dependency.
- Apply the **Template Loading Protocol** for every non-trivial component. Swap structural tokens to the project's actual token names.
- Lock ONE design archetype per page. State archetype + theme in a one-line comment before markup. No `dark:` variants — commit to one mode.
- `lucide-astro` only for UI icons (PascalCase imports). No text glyphs (`→`, `✓`) for UI. Raw SVG only for brand logos and JIT-safe textures.
- Honor `prefers-reduced-motion` on every animation. One global `IntersectionObserver` (`is:inline`) in the layout for `.reveal`/`.reveal.visible`. No `window.addEventListener('scroll')`. Use `min-h-[100dvh]`, never `h-screen`.

### MUST NOT DO
- Use React-isms (`className`, `useState`, JSX) inside `.astro` templates. `.astro` is JSX-like but uses `class`, scoped scripts, no JSX runtime.
- Use deprecated endpoints-only patterns (`export const POST` in `pages/api/*`) when an Action fits the typed-mutation use case. Endpoints are still correct for raw JSON / webhooks / non-form mutations.
- Define content collections in `src/content/config.ts` (Astro 4 path). Astro 5 lives in `src/content.config.ts` and uses the Content Layer API.
- Inline template markup from memory. Open the `templates/*.md` file first.
- Mix archetypes on one page. Ship `dark:` variants. Use Tailwind v4 syntax inside a Tailwind v3 project (or vice versa).
- Use `astro-icon` for UI icons, emoji as UI, native checkboxes (use `sr-only peer` + sibling div), stock shadows (`shadow-md`/`lg`), or pure `#000000` (tint to `#0A0A0B` / `zinc-950`).
- Ship em dashes (`—`), en dashes (`–`) as pauses, ellipsis (`…`), or exclamations (`!`) in marketing copy. Slugs/identifiers are exempt.
- Use banned buzzwords (seamless, elevate, robust, unlock, empower, streamline, supercharge, revolutionize, cutting-edge, next-level, bespoke, journey, synergy, paradigm, blazing-fast, etc. — full list in `references/anti-slop-rules.md`).
- Use gradient text (`bg-clip-text` + `text-transparent`) or decorative `backdrop-filter: blur()` for non-fixed/non-sticky elements. Glassmorphism only over a textured background, fixed/sticky position, or the Clinical archetype's signature glass card.
- Ship `window.addEventListener('scroll')`, multiple continuous animations per viewport, or mousemove magnets.

## Code Examples

### `.astro` component — frontmatter script + template + slot

```astro
---
// src/components/SectionCard.astro
import { ArrowRight } from 'lucide-astro';
interface Props {
  title: string; body: string; href: string;
  archetype?: 'industrial' | 'luxury' | 'tech' | 'clinical';
}
const { title, body, href, archetype = 'industrial' } = Astro.props;
const radius = archetype === 'luxury' ? 'rounded-[2rem]' : 'rounded-none';
---
<section class="reveal relative overflow-hidden py-24 md:py-32">
  <div class={`relative z-10 max-w-6xl mx-auto px-6 md:px-8 lg:px-12 border-2 border-text-main ${radius} bg-surface p-8`}>
    <h2 class="font-heading text-3xl md:text-4xl leading-[1.05] text-balance">{title}</h2>
    <p class="mt-4 text-text-muted leading-relaxed max-w-prose">{body}</p>
    <a href={href} class="group inline-flex items-center gap-2 mt-6 font-heading uppercase tracking-widest text-sm">
      Read more
      <ArrowRight class="w-4 h-4 transition-transform group-hover:translate-x-1" />
    </a>
    <slot />
  </div>
</section>
```

### Content collection — `src/content.config.ts` (Astro 5 Content Layer API)

```ts
import { defineCollection, z } from 'astro:content';
import { glob } from 'astro/loaders';

const posts = defineCollection({
  loader: glob({ pattern: '**/*.{md,mdx}', base: './src/content/posts' }),
  schema: z.object({
    title: z.string(),
    pubDate: z.coerce.date(),
    updatedDate: z.coerce.date().optional(),
    tags: z.array(z.string()).default([]),
    draft: z.boolean().default(false),
  }),
});

export const collections = { posts };
```

```astro
---
// src/pages/blog/index.astro
import { getCollection } from 'astro:content';
const posts = (await getCollection('posts')).filter((p) => !p.data.draft);
---
<ul>{posts.map((p) => <li><a href={`/blog/${p.id}/`}>{p.data.title}</a></li>)}</ul>
```

### Action — typed server mutation (`astro:actions`)

```ts
// src/actions/index.ts
import { defineAction, z, isActionError } from 'astro:actions';

export const subscribe = defineAction({
  input: z.object({ email: z.string().email() }),
  handler: async ({ email }, ctx) => {
    const ok = await ctx.locals.mailer.add(email);
    if (!ok) return isActionError({ code: 'CONFLICT', message: 'Already subscribed' });
    return { status: 'subscribed' as const };
  },
});
```

```astro
---
// src/pages/index.astro — form posts to the Action automatically
import { actions } from '../actions';
---
<form method="POST" action={actions.subscribe}>
  <input type="email" name="email" autocomplete="email" required />
  <button type="submit">Subscribe</button>
</form>
```

### View Transitions — layout + persisted nav + paired morph

```astro
---
// src/layouts/Layout.astro
import { ClientRouter } from 'astro:transitions';
const { title } = Astro.props;
---
<!doctype html>
<html lang="en">
  <head>
    <meta charset="utf-8" /><title>{title}</title>
    <ClientRouter />
  </head>
  <body>
    <header transition:persist><nav><!-- stays mounted across navigations --></nav></header>
    <main><slot /></main>
  </body>
</html>
```

```astro
<!-- src/pages/about.astro — paired transition:name morphs across routes -->
<h1 transition:animate="slide">About</h1>
<img src="/logo.svg" transition:name="brand-logo" alt="Brand" />
```

### Tailwind v4 tokens (`@theme`) — see `references/tailwind-v4.md` for the full archetype swap. Reduced-motion fallback is mandatory:

```css
@media (prefers-reduced-motion: reduce) {
  *, *::before, *::after {
    animation-duration: 0.01ms !important;
    transition-duration: 0.01ms !important;
  }
}
```

## Output Template

When implementing an Astro task, deliver:

1. **Archetype declaration** — one-line comment naming the locked archetype (Industrial / Luxury / Tech / Clinical) + theme (LIGHT default, DARK only for Tech).
2. **`.astro` components** — frontmatter script (imports + props + `Astro.props`), JSX-like template (`class` not `className`), `<slot/>`, scoped `<script is:inline>` where needed.
3. **Content collection config** — `src/content.config.ts` with `glob()` loader + zod schema (if authoring content).
4. **Layout** — `<head>` with `<ClientRouter />`, `<meta>`, JSON-LD, global reveal observer (`is:inline`), `[x-cloak]` global CSS, skip link, semantic landmarks.
5. **Sections** — transparent `<section>` shells (`relative overflow-hidden py-24 md:py-32`, `z-10` content), archetype haptics (Stamped / Double-Bezel / Standard Card / Glow / Glass / Editorial), no two consecutive sections sharing a grid.
6. **Tailwind v4 tokens** — `@theme` block with color/type/spacing tokens; contrast ratios documented; archetype textures as JIT-safe URL-encoded `background-image` (no base64).
7. **Forms** — `@submit.prevent` + `fetch()`, `aria-live="polite"`, honeypot `type="text" class="sr-only"` (not `type="hidden"`), `autocomplete` on every input, `<template x-if="submitted">` for success.
8. **Verification log** — `astro check` / `astro build` / `tsc --noEmit` output, Lighthouse + axe scores, `rg` greps for banned patterns. Mark visual UX ASSUMED if not screenshot-tested.

## Knowledge Reference

Astro 5 APIs: `astro:content` (`getCollection`, `getEntry`, `render`, `reference`), `astro:actions` (`defineAction`, `action`, `z`, `isActionError`), `astro:transitions` (`ClientRouter`, `transition:name/animate/persist`, `astro:before-preparation`/`astro:after-swap`), `astro:assets` (`<Image>`, `<Picture>`, `getImage`), `astro:i18n`, `astro:env` (`getSecret`, `import.meta.env.PUBLIC_*`). Content Layer API (`src/content.config.ts`, `glob`/`file`/`fetch` loaders). Middleware (`defineMiddleware`, `sequence`, `context.locals`). Endpoints (`export const GET/POST`, `APIContext`). Integrations (`@astrojs/{react,vue,svelte,solid-js,mdx,node,cloudflare,vercel,netlify,deno,tailwind}`). Tailwind v4 CSS-first (`@import "tailwindcss"`, `@theme`, `@layer`, `@utility`). Alpine.js (`x-data`/`x-show`/`x-cloak`/`@submit.prevent`, CDN `<script defer>`, no `import 'alpinejs'`). `lucide-astro` PascalCase. Four design archetypes (color/texture/shadow/radii/motion per `references/design-archetypes.md`). WCAG 2.1 AA (4.5:1 text, 3:1 UI, semantic landmarks, focus-visible, skip link, ARIA binding). Anti-slop discipline (`references/anti-slop-rules.md`). JSON-LD schema.org. `IntersectionObserver` + `prefers-reduced-motion`. `100dvh` viewport unit.

## VERIFIED vs ASSUMED (honest exit)

Before reporting done, separate:

- **VERIFIED** — gates you actually ran with real output: `astro check`, `astro build`, `tsc --noEmit`, Lighthouse, axe, `rg` greps for banned patterns. Paste actual terminal output, not intended behavior. If a check was skipped, say so.
- **ASSUMED** — anything you did not directly verify: visual appearance (unless screenshot-tested), cross-browser rendering, real screen reader pass, real-user load performance, archetype color contrast on the actual project tokens (must be re-checked after token swap).

Report both lists. "It should work" is not verification. If the build fails after your edits but passed at baseline, fix it before reporting. If the baseline already failed, the bar is no new errors vs baseline.

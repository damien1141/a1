# Visual Hierarchy & Layout

How to compose a page that does not look like a template. Asymmetric layouts, varied section rhythms, type scale, responsive design.

## The variance principle

> No two consecutive sections may share a grid structure.

If Section 1 is a 3-column grid, Section 2 must be a 2-column split or full-width. If Section 1 is `7/5`, Section 2 must not be `7/5` or `5/7`. This is the single biggest defense against template slop.

## Hero (asymmetric editorial, premiere pattern)

```html
<section class="min-h-[78dvh] grid grid-cols-1 md:grid-cols-12 gap-8 items-end pt-16 md:pt-24 pb-24 md:pb-32">
  <div class="md:col-span-7">
    <p class="text-text-muted text-sm uppercase tracking-[0.2em] mb-4">Eyebrow (one per 3 sections)</p>
    <h1 class="text-balance text-display font-sans">Harrison Roofing replaces roofs in Leeds.</h1>
    <p class="mt-6 text-body text-text-muted max-w-prose">Quotes in 48 hours. Most jobs done in two days.</p>
    <a href="/quote" class="mt-8 inline-flex items-center rounded-md bg-accent text-accent-contrast px-6 py-3 focus-visible:ring-2 focus-visible:ring-offset-2">Get a quote</a>
  </div>
  <aside class="md:col-span-5 border-l border-black/10 pl-6 font-mono text-sm text-text-muted">
    <dl class="grid grid-cols-2 gap-y-2 tabular-nums">
      <dt>Lead time</dt><dd>2 days</dd>
      <dt>Service area</dt><dd>Leeds + 25mi</dd>
      <dt>Warranty</dt><dd>10 years</dd>
    </dl>
  </aside>
</section>
```

Rules:
- Headline ≤ 2 lines. Subtext ≤ 20 words. CTA visible without scroll. Top padding ≤ `pt-24`.
- Hero stack: max 4 elements (eyebrow, headline, subtext, CTA group). No taglines, no trust micro-strips, no feature lists.
- One CTA. The "primary + ghost `learn more`" two-button pair is banned.
- Logo wall, if any, lives UNDER the hero, never inside it.

Banned hero patterns:
- Centered hero (3-card row underneath, top-aligned multi-col).
- "Left big headline + right small explainer paragraph" — stack them vertically.
- Eyebrow overload — max 1 eyebrow per 3 sections.

## Section shell

```html
<section class="reveal relative overflow-hidden py-24 md:py-32">
  <div class="relative z-10 max-w-6xl mx-auto px-6 md:px-8 lg:px-12">
    <!-- content -->
  </div>
</section>
```

Sections are transparent (no per-section bg glow/grid). The atmospheric texture lives in `Layout.astro` only: `-z-30` surface, `-z-20` grid, `-z-10` `.section-glow`. `<slot/>` in `<main class="relative z-0">`.

## Section rhythm (don't repeat)

| Section N | Pattern |
|-----------|---------|
| 1 (Hero) | Asymmetric editorial 7/5 split |
| 2 | Full-width feature (single column, max-w-3xl, centered text + off-grid number) |
| 3 | Bento mosaic (1 large + 4 small, mixed cell sizes) |
| 4 | Alternating spotlight (7/5 then 5/7 on the next item) |
| 5 | Vertical timeline or sticky scroll-storyteller |
| 6 | Asymmetric FAQ (single column max-w-4xl) or `c1` accordion |
| 7 (CTA) | Asymmetric editorial split (intro + contact ledger left, form card right) — beats centered block |

No two consecutive sections share a grid family.

## Bento mosaic

```html
<section class="py-24 md:py-32">
  <div class="max-w-6xl mx-auto px-6 grid gap-4 md:grid-cols-6 md:grid-rows-4">
    <div class="md:col-span-4 md:row-span-2 bg-surface-elevated rounded-lg p-8">Big focal point</div>
    <div class="md:col-span-2 md:row-span-1 bg-surface-muted rounded-lg p-6">Stat</div>
    <div class="md:col-span-2 md:row-span-1 bg-surface-muted rounded-lg p-6">Stat</div>
    <div class="md:col-span-3 md:row-span-2 bg-surface-elevated rounded-lg p-8">Medium feature</div>
    <div class="md:col-span-3 md:row-span-2 bg-surface-elevated rounded-lg p-8">Medium feature</div>
  </div>
</section>
```

N items = N cells. No empty cells in the middle. At least 2–3 cells must have real visual variation, not just white-on-white text.

## Long lists (avoid `divide-y` > 5 items)

```html
<!-- ❌ Bad: 12-item <ul> with divide-y -->
<ul class="divide-y">
  <li>…</li> × 12
</ul>

<!-- ✅ Good: 2-col grid or grouped chunks -->
<div class="grid gap-4 sm:grid-cols-2">
  <div>…</div> × 12
</div>

<!-- ✅ Or tabs (Alpine) -->
<div x-data="{ tab: 'a' }">
  <button @click="tab = 'a'" :aria-expanded="(tab === 'a').toString()">A</button>
  <button @click="tab = 'b'" :aria-expanded="(tab === 'b').toString()">B</button>
  <div x-show="tab === 'a'">…group a items…</div>
  <div x-show="tab === 'b'">…group b items…</div>
</div>
```

## Button contrast (WCAG AA)

- No white-on-white. Transparent buttons need a backdrop or stroke.
- CTA text fits on one line at desktop (max 3 words).
- No duplicate CTA intent on one page ("Get in touch" + "Contact us" is a fail). Pick one.

```html
<!-- Primary -->
<a href="/quote" class="inline-flex items-center rounded-md bg-accent text-accent-contrast px-6 py-3 focus-visible:ring-2 focus-visible:ring-offset-2">Get a quote</a>

<!-- Secondary outline -->
<a href="/work" class="inline-flex items-center rounded-md border border-black/40 px-6 py-3 focus-visible:ring-2">See the work</a>
```

## Logo wall (real SVGs, never text wordmarks)

```html
<section class="py-12 border-y">
  <p class="text-center text-sm uppercase tracking-[0.2em] text-text-muted mb-8">Trusted by</p>
  <div class="max-w-5xl mx-auto px-6 grid grid-cols-2 md:grid-cols-4 gap-8 items-center justify-items-center">
    <!-- Real inline SVGs of real client logos. No "Acme" wordmarks. No category labels. -->
    <svg>…</svg>
    <svg>…</svg>
    <svg>…</svg>
    <svg>…</svg>
  </div>
</section>
```

If real, verifiable logos aren't available, cut the section. A fake logo wall is worse than none.

## Responsive design

**Mobile-first.** Default styles target the smallest viewport; `sm:` / `md:` / `lg:` progressively enhance.

| Breakpoint | Tailwind | Width |
|------------|----------|-------|
| Mobile (default) | (none) | < 640px |
| Small | `sm:` | ≥ 640px |
| Medium | `md:` | ≥ 768px |
| Large | `lg:` | ≥ 1024px |
| XL | `xl:` | ≥ 1280px |
| 2XL | `2xl:` | ≥ 1536px |

### Mobile collapse rules

- Asymmetric grids → single column below `md`.
- Drop rotations and negative margins below `md`.
- Modal/slide-over: full-width, no scale.
- Marquee: slow + smaller gap on mobile.
- `col-span-*` → `col-span-1` below `md`.

### Viewport

```html
<!-- ✅ Good: dynamic viewport height (handles mobile URL bar) -->
<div class="min-h-[100dvh]">

<!-- ❌ Bad: legacy 100vh (mobile URL bar cuts off content) -->
<div class="h-screen">
```

### Container width

```html
<div class="max-w-6xl mx-auto px-6 md:px-8 lg:px-12">
```

Body copy max width: `max-w-prose` (~65ch) for readability. Display headlines can be wider.

### Flex safety

Every flex child needs `min-w-0` to prevent text overflow:
```html
<div class="flex gap-4">
  <div class="min-w-0 flex-1">
    <p class="truncate">Long text that would otherwise break the layout</p>
  </div>
  <button class="shrink-0">Action</button>
</div>
```

### Images

Use `next/image` (Next), `<NuxtImg>` (Nuxt), or native `<img loading="lazy" decoding="async" width="…" height="…">` with explicit dimensions to prevent layout shift.

```html
<img src="/hero.jpg" alt="Crew installing shingles on a two-story house" width="1200" height="675" loading="eager" fetchpriority="high" />
<img src="/chart.png" alt="" width="800" height="450" loading="lazy" decoding="async" />
```

Decorative images/SVGs: `alt=""`. Content images: descriptive `alt`.

## Micro-typography

- Body: `leading-relaxed` (1.625) or `leading-[1.6]`.
- Headlines: `leading-[1.05]` (Industrial `0.95`).
- Eyebrows: `tracking-[0.2em]`, `text-sm`, uppercase.
- Stats/prices: `tabular-nums`, `font-mono`.
- Headlines: `text-balance` to prevent orphans.
- No em dashes (`—`) in body. Use periods, commas, or regular hyphens.
- `font-mono` only for data, terminal, code, indices — never body copy.

## Quick Reference

| Pattern | Rule |
|---------|------|
| Hero | Asymmetric editorial 7/5 split; max 4 elements; one CTA |
| Sections | No two consecutive share a grid |
| Bento | N items = N cells; ≥2–3 cells with visual variation |
| Long lists | >5 items: 2-col grid, tabs, or grouped chunks (not `divide-y`) |
| Buttons | One intent per page; primary + outline only |
| Logo wall | Real inline SVGs only; under hero, never inside |
| Viewport | `min-h-[100dvh]`, never `h-screen` |
| Container | `max-w-6xl mx-auto px-6 md:px-8 lg:px-12` |
| Body copy | `max-w-prose` (~65ch) |
| Flex children | `min-w-0` to prevent overflow |
| Images | Explicit `width`/`height`, `loading="lazy"`, `decoding="async"` |
| Headlines | `text-balance`, `leading-[1.05]` |
| Stats | `tabular-nums`, `font-mono` |
| Breakpoints | Mobile-first; `md:` for tablet, `lg:` for desktop |

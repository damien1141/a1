# The Four Design Archetypes

Apply all six axes (color, texture, shadow, radii, motion, typography) per archetype. No `dark:` variants — commit to ONE mode per page. State archetype + theme in a one-line comment before markup. The token names are STRUCTURAL — swap the actual color values per archetype; templates don't change.

## Variance Engine (pick by copy vibe + price, not industry)

| Archetype | Copy vibe | Price signal | Theme |
|---|---|---|---|
| Rugged Industrial | utility, durability, urgency | utility, trade, local service | LIGHT |
| Organic Luxury | bespoke, concierge, high-ticket | premium, by-appointment, edition sizes | LIGHT |
| Precision Tech | scale, speed, developer | dev tools, infra, SaaS | DARK (only dark archetype) |
| Clinical Trust | compliance, precision, board-certified | medical, legal, finance, compliance | LIGHT |

Subversion = interest. A rugged industrial site for a luxury hotel subverts the archetype expectation and earns a second look. Pick by what the copy is doing, not the industry tag.

## A. Rugged Industrial — LIGHT

| Axis | Spec |
|---|---|
| Color | `zinc-100` bg / `zinc-900` text / hazard `amber-400` accent |
| Texture | blueprint grid `stroke=%23000` `opacity-[0.08]` |
| Shadow | hard offset: `border-2 border-zinc-900 shadow-[8px_8px_0px_0px_rgba(24,24,27,1)]` |
| Radii | ZERO (`rounded-none`) |
| Motion | `cubic-bezier(0.7,0,0.3,1)` `duration-150` |
| Typography | headlines `leading-[0.95]`, weight 700-800, uppercase tracking-wide. Eyebrows `tracking-[0.2em]`. Body `leading-relaxed`. `font-mono` for data/indices. |

Haptics: **Stamped Block** — zero radius, 2px border, solid offset shadow. Industrial focal points only.

## B. Organic Luxury — LIGHT

| Axis | Spec |
|---|---|
| Color | `#FDFBF7` bg / `#1C1917` text / Sage `#7C8471` or Espresso `#3D2C1E` accent |
| Texture | topographic contour `opacity-[0.04]` + soft mesh + noise `opacity-[0.03]` |
| Shadow | layered soft: `shadow-[0_4px_24px_-8px_rgba(28,25,23,0.12)]` |
| Radii | `rounded-[2rem]` / `rounded-[3rem]` |
| Motion | `cubic-bezier(0.16,1,0.3,1)` `duration-700–1000`, blur-focus reveal |
| Typography | headlines `leading-[1.05]`, weight 300-500, display serif or refined sans. `text-balance`. Eyebrows `tracking-[0.2em]` small-caps. |

Haptics: **Double-Bezel** — outer shell + inset core. Luxury/Tech focal points.

## C. Precision Tech — DARK (only dark archetype)

| Axis | Spec |
|---|---|
| Color | near-black base (`#020203` OLED or charcoal `#0F0F11` — brand choice), `#FAFAFA` text, accent electric blue (`#3B82F4` default; `#8AB4F8` or any on-brand blue allowed) |
| Texture | node-network SVG `stroke=%23fff` `opacity-[0.08]` + glow halos |
| Shadow | `shadow-[0_0_40px_-10px_rgba(59,130,246,0.5)] ring-1 ring-white/10` |
| Radii | `rounded-xl` / `rounded-2xl` |
| Motion | `cubic-bezier(0.32,0.72,0,1)` `duration-500`, blur-focus |
| Typography | headlines `leading-[1.05]`, weight 500-600, sans or mono. Eyebrows `tracking-[0.2em]`. `font-mono` for code/terminal/data. |

**Light-accent contrast (CRITICAL):** if the accent is a LIGHT blue (`#8AB4F8`, `#93C5FD`), button text MUST be DARK (`text-surface`), never white — white-on-light-blue fails WCAG AA. Hard-offset stamps are invisible on OLED; use light ink (`text-surface`). Checkbox checks `text-surface`.

Haptics: **Double-Bezel** (Luxury/Tech focal) or **Glow Card** (`v1-glow-card-tech-signature.md`).

## D. Clinical Trust — LIGHT

| Axis | Spec |
|---|---|
| Color | `#FAFAFA` base / `#0F172A` text / `#0D9488` accent (teal) |
| Texture | micro dot grid `fill=%230F172A` `opacity-[0.05]` + frosted glass + pulse dots |
| Shadow | layered soft: `shadow-[0_4px_24px_-8px_rgba(15,23,42,0.08)]` |
| Radii | `rounded-xl` / `rounded-2xl` |
| Motion | `cubic-bezier(0.4,0,0.2,1)` `duration-400` (no spring/blur) |
| Typography | headlines `leading-[1.05]`, weight 500-600, sans. Eyebrows `tracking-[0.2em]`. `font-mono` for clinical data, stats, dosages. |

Haptics: **Glass Card** (`v2-glass-card-clinical-signature.md`) over a textured background. Fixed/sticky only for `backdrop-blur` usage.

## Haptic Architecture (cross-archetype)

| Haptic | Archetype | File |
|---|---|---|
| Stamped Block | Industrial focal only | `a-the-stamped-block-industrial-focal-points-only.md` |
| Double-Bezel | Luxury/Tech focal | `b-the-double-bezel-luxury-tech-focal-points.md` |
| Standard Card | All archetypes, secondary grid items | `c-the-standard-card-secondary-elements.md` |
| Glow Card | Tech signature | `v1-glow-card-tech-signature.md` |
| Glass Card | Clinical signature | `v2-glass-card-clinical-signature.md` |
| Editorial Card | Luxury signature | `v3-editorial-card-luxury-signature.md` |
| Data Card | All (stats/KPIs) | `v4-data-card.md` |
| Quote Card | All (testimonials) | `v5-quote-card.md` |
| Pricing Card | All | `v6-pricing-card-standalone.md` |

## Token Substitution

Templates use structural tokens. Swap to actual values per archetype in `@theme`:

```css
/* Industrial */
@theme {
  --color-surface: oklch(0.97 0 0);            /* zinc-100 */
  --color-surface-elevated: oklch(1 0 0);
  --color-surface-muted: oklch(0.92 0 0);
  --color-text-main: oklch(0.18 0 0);          /* zinc-900 */
  --color-text-muted: oklch(0.40 0 0);
  --color-accent: oklch(0.78 0.18 75);         /* amber-400 */
  --color-accent-contrast: oklch(0.18 0 0);    /* dark text on amber */
  --font-heading: "Inter", system-ui, sans-serif;
  --font-mono: "JetBrains Mono", ui-monospace, monospace;
  --radius-card: 0;                            /* Industrial: ZERO */
  --shadow-stamp: 8px 8px 0px 0px rgba(24,24,27,1);
}
```

```css
/* Luxury */
@theme {
  --color-surface: oklch(0.98 0.005 80);       /* #FDFBF7 */
  --color-surface-elevated: oklch(1 0 0);
  --color-surface-muted: oklch(0.96 0.005 80);
  --color-text-main: oklch(0.15 0.005 60);     /* #1C1917 */
  --color-text-muted: oklch(0.38 0.005 60);
  --color-accent: oklch(0.55 0.04 140);        /* Sage #7C8471 */
  --color-accent-contrast: oklch(0.98 0 0);
  --font-heading: "Playfair Display", "Inter", serif;
  --font-mono: "JetBrains Mono", ui-monospace, monospace;
  --radius-card: 2rem;                          /* Luxury: large radii */
  --shadow-card: 0 4px 24px -8px rgba(28,25,23,0.12);
}
```

```css
/* Tech (DARK) */
@theme {
  --color-surface: oklch(0.10 0.005 270);      /* #020203 OLED */
  --color-surface-elevated: oklch(0.14 0.005 270); /* #0F0F11 */
  --color-surface-muted: oklch(0.18 0.005 270);
  --color-text-main: oklch(0.98 0 0);          /* #FAFAFA */
  --color-text-muted: oklch(0.65 0.005 270);
  --color-accent: oklch(0.62 0.20 250);        /* #3B82F4 electric blue */
  --color-accent-contrast: oklch(0.10 0.005 270); /* DARK text on light blue */
  --font-heading: "Inter", system-ui, sans-serif;
  --font-mono: "JetBrains Mono", ui-monospace, monospace;
  --radius-card: 0.75rem;
  --shadow-glow: 0 0 40px -10px rgba(59,130,246,0.5);
}
```

```css
/* Clinical */
@theme {
  --color-surface: oklch(0.98 0 0);            /* #FAFAFA */
  --color-surface-elevated: oklch(1 0 0);
  --color-surface-muted: oklch(0.95 0 0);
  --color-text-main: oklch(0.18 0.02 250);     /* #0F172A */
  --color-text-muted: oklch(0.42 0.02 250);
  --color-accent: oklch(0.60 0.10 180);        /* #0D9488 teal */
  --color-accent-contrast: oklch(0.98 0 0);
  --font-heading: "Inter", system-ui, sans-serif;
  --font-mono: "JetBrains Mono", ui-monospace, monospace;
  --radius-card: 0.75rem;
  --shadow-card: 0 4px 24px -8px rgba(15,23,42,0.08);
}
```

## Section Spacing (all archetypes)

- Interior: `py-24 md:py-32`
- Hero exception: `pt-16 md:pt-24 pb-24 md:pb-32` + `md:min-h-[78dvh]` on inner grid. No `min-h-[100dvh]` on hero.
- No two consecutive sections share a grid structure.

## Mobile Collapse (all archetypes)

- Asymmetric → `w-full` / `px-6` / single column below `md`.
- No `h-screen` (use `min-h-[100dvh]`).
- `col-span-*` → `col-span-1` below `md`.
- Drop rotations / negative margins below `md`.
- Map iframe in bounded box.
- Marquee: slow + smaller gap on mobile.
- Modal / slide-over: full-width, no scale.

## Atmospheric Depth

In `Layout.astro` only:
- `-z-30` surface (base color)
- `-z-20` grid (JIT-safe URL-encoded SVG, NO base64)
- `-z-10` `.section-glow` (radial highlight)
- `<slot/>` in `<main class="relative z-0">`

Sections transparent, `relative overflow-hidden`, content `z-10`, no per-section glow/grid.

## Absolute Zero (any one = instant fail)

- React-isms (`className`, `useState`, JSX) → use `class`.
- Vanilla JS for UI logic (global observer + scoped `x-init` observers excepted).
- Pure `#000000` → tinted (`#0A0A0B`, `#020203`, `zinc-950`).
- Stock shadows (`shadow-sm/md/lg/xl/2xl`) → custom layered/stamped.
- `astro-icon`; text glyphs; raw SVG UI icons (Google logo / brand / texture exempt).
- Native checkboxes → `sr-only peer` + sibling div.
- Template clichés: `grid-cols-3` feature rows, centered heroes, top-aligned multi-col without offset.
- Samey grids: no two consecutive sections share a 7/5 or 5/7.
- `linear`/`ease-in-out` for state transitions (continuous loops exempt).
- Edge-to-edge nav; flat sections (no texture/ghost/mesh); `dark:` variants; Tailwind v4 syntax inside a v3 project; `@alpinejs/*` plugins; mousemove magnets; multiple continuous animations; emoji as UI; `backdrop-blur` on scrolling cards.

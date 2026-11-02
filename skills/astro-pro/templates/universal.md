### Universal

- Any asymmetric layout above `md:` MUST collapse to `w-full`, `px-6`, single-column below `md`.
- Never use `h-screen` for full-height sections. Always `min-h-[100dvh]` to prevent iOS Safari viewport jumping.
- All `col-span-*` overrides reset to `col-span-1` below `md`.
- Remove all rotations (`-2deg`, `3deg`) and negative-margin overlaps below `md` — they cause touch-target conflicts.
- **Map + address rows:** Never let a `h-full` iframe expand to cover a sibling address/label row. Wrap the iframe in its own bounded box (e.g., `h-[280px] lg:flex-1 lg:min-h-0`) and place the address as a separate sibling below it, so it always renders visible.
- **Marquee on mobile:** Reduce `gap` and slow the duration (e.g., `40s`) so the motion doesn't nauseate.
- **Modal/slide-over on mobile:** Full-width, full-height, no scale transform (scale clips on small viewports).

---

## 23. PERFORMANCE GUARDRAILS

- **GPU-safe animation:** Never animate `top`, `left`, `width`, `height`. Animate exclusively `transform` and `opacity`. Use `will-change: transform` sparingly, only on actively animating elements.
- **Blur constraints:** Apply `backdrop-blur` only to fixed or sticky elements (nav, overlays, modal backdrops). Never to scrolling containers — it causes continuous GPU repaints and mobile frame drops. Glass Cards (§14.V2) are the narrow exception: only when the card sits over a richly textured/mesh background.
- **Noise/grain overlays:** Only on fixed, `pointer-events-none` pseudo-elements (`fixed inset-0 z-50`). Never on scrolling containers.
- **Z-index discipline:** Reserve for systemic layers: `z-50` sticky nav / modal, `z-40` overlays/sheet, `z-30` cascade-top, `z-20` / `z-10` local stacking contexts, `-z-10` / `-z-20` backgrounds. No scattered `z-[9999]`.
- **Pure Alpine:** No `window.addEventListener('scroll')` in vanilla `<script>`. Use `@scroll.window` Alpine directive (passive) or the global `IntersectionObserver`. Count-up observers (§19.M5) MUST `disconnect()` after firing.
- **Observer budget:** One global reveal observer + at most a handful of scoped count-up observers. Never one observer per element across a 50-item list.
- **Aurora/marquee budget:** At most ONE continuous keyframe animation per viewport. Two marquees on screen simultaneously will drop frames on mid-range mobiles.

---

## 24. MICRO-TYPOGRAPHY & HIERARCHY

- **Line height:** Body text uses `leading-relaxed`. Headlines use `leading-[1.05]` (Industrial can push to `leading-[0.95]` for brutal impact).
- **Tracking:** Eyebrows use `tracking-[0.2em]` or `tracking-widest`. Large headlines use `tracking-tight`.
- **Alignment:** Use `text-balance` on all headings. Use `tabular-nums` on all stats and numbers.
- **Banned punctuation:** Em-dashes (`—`) are STRICTLY BANNED in body copy. Use periods or regular hyphens.
- **Mono usage:** `font-mono` is for data labels, terminal output, code blocks, and numeric indices — never for body copy.

---

## 25. THE ABSOLUTE ZERO DIRECTIVE (Strict Anti-Patterns)

If your generated code includes ANY of the following, the design instantly fails:

- **Banned React-isms:** `className`, `useState`, JSX in `.astro` files. Use `class`, not `className`.
- **Banned vanilla JS:** No `<script>` tags with `document.querySelector` or `addEventListener` for UI logic. Use Alpine.js directives (`x-data`, `x-show`). The ONE exception is the global `IntersectionObserver` reveal script in `Layout.astro`, which is exempt by design. Alpine `x-init` with a scoped observer is also permitted (§0).
- **Banned pure black:** Pure `#000000`. Use tinted darkness (`#0A0A0B`, `#050505`, `#020203`, `zinc-950`).
- **Banned shadows:** `shadow-sm`, `shadow-md`, `shadow-lg`, `shadow-xl`, `shadow-2xl`. Use exact custom layered shadows or hard stamped offsets.
- **Banned images:** No `<img>` tags. No `public/` directory images. 100% CSS/SVG only (SEO/Niche requirement). Use Faux Thumbnails (§15.B6), mesh gradients (§15.B1), monogram avatars (§12.C11), and CSS logo walls (§12.C12).
- **Banned `astro-icon`:** DO NOT use `astro-icon` or `@iconify-json`. Use `lucide-astro` exclusively.
- **Banned Text Glyphs:** NEVER use text characters for UI elements (`→`, `▼`, `✓`, `★`). Use `lucide-astro` components.
- **Banned raw SVG icons:** No raw inline `<svg>` paths for UI glyphs. Use `<ArrowRight />` etc. The ONLY exception is the Google "G" logo and the JIT-safe texture/divider SVGs in §5/§15.
- **Banned native checkboxes:** Never style `<input type="checkbox">` directly. Use the `sr-only peer` + sibling div pattern (§8.D).
- **Banned template layouts:** No `grid-cols-3` for features. No perfectly centered hero sections. No top-aligned multi-column grids without vertical offsets.
- **Banned "Samey" Grids:** NO TWO CONSECUTIVE SECTIONS can use the same 7/5 or 5/7 split. You must vary the layout structure.
- **Banned motion:** `linear`, `ease-in-out`, `transition-default` for STATE TRANSITIONS. Instant state changes without interpolation. (Continuous keyframe loops — marquee §19.M6, aurora §15.B3 — are the sanctioned `linear` exception.)
- **Banned edge-to-edge nav:** Sticky navbars glued to the top with no margin. Use the Fluid Island pattern (§8.H) or the Split Bar with explicit bottom border (§10.N3).
- **Banned flat sections:** Flat background colors without texture, ghost text, or mesh gradients.
- **Banned dual-mode design:** Do not build sites trying to support both light and dark mode simultaneously via `dark:` variants. Commit to the Archetype's default theme.
- **Banned Tailwind v4 syntax:** Do not use `@apply` with bare CSS variables or `@screen` in arbitrary values. Stick to v3-compliant JIT strings.
- **Banned soft radii (Industrial):** If deploying Rugged Industrial, `rounded-md`, `rounded-lg`, `rounded-xl` are STRICTLY BANNED. Use `rounded-none` exclusively.
- **Banned scroll listeners:** No `window.addEventListener('scroll')` in `<script>` tags. Use `@scroll.window` (Alpine, passive) or the global `IntersectionObserver`.
- **Banned `@alpinejs/*` plugins:** No `@alpinejs/intersect`, `@alpinejs/collapse`, `@alpinejs/focus`. The CDN core only. Use the grid-rows trick for accordions (§12.C1), the global observer for reveals (§20.B), and `@keydown.escape.window` for modal dismissal (§12.C3).
- **Banned mousemove magnets:** No `mousemove` listeners for cursor-following effects. Use the CSS-only Magnetic Nudge (§19.M8).
- **Banned multiple continuous animations:** Never stack aurora + marquee + typewriter in the same viewport. One continuous animation per viewport (§23).
- **Banned emoji as UI:** No emoji (`🔥`, `✅`) in UI elements. Use lucide icons.
- **Banned `backdrop-blur` on scrolling cards:** Glass Cards (§14.V2) only over textured backgrounds; never on plain-scrolling content.

---

## 26. PRE-OUTPUT CHECKLIST

Run this matrix before delivering. This is the last filter.

- [ ] Archetype locked and stated in one line at the top of output.
- [ ] **Stack Verified:** Pure Astro/Tailwind v3/Alpine. No React/JSX, no vanilla JS UI logic.
- [ ] No React/JSX syntax. Using `class`, not `className`.
- [ ] Pure `#000000` is not used; tinted blacks are applied.
- [ ] **Icons:** `lucide-astro` used for all UI glyphs (`<ArrowRight />`). No `astro-icon`, no text glyphs, no raw SVGs (except Google logo and texture/divider SVGs). Every icon is imported in the frontmatter.
- [ ] **Haptics:** Focal points use Double-Bezel (Luxury), Glow Card (Tech), Glass Card (Clinical), or Stamped Block (Industrial). Secondary cards use Standard Card or an archetype variant (§14).
- [ ] **Anti-Template Strategy:** Bento Mosaic, Vertical Narrative, Asymmetric List, Z-Axis Cascade, Sticky Scroll, or Alternating Spotlight used. No 3-card grids.
- [ ] **Layout Rhythm:** No two consecutive sections share the same grid structure.
- [ ] **Hero:** ONE hero variant from §7.Mandate 1 or §9. Hero spacing uses `pt-16 md:pt-24 pb-24 md:pb-32` + `md:min-h-[78dvh]` (the documented exception).
- [ ] **Global Texture Layer:** Background grid is applied to a `fixed` wrapper in the global shell, NOT inside individual sections.
- [ ] **Section Shell:** Sections are transparent; the radial glow is owned by the global shell, not by the section.
- [ ] **Atmospheric Depth:** Ghost text (5–8% opacity) and archetype-specific SVG textures/mesh gradients present, not flat colors.
- [ ] **Tailwind v3 JIT Safe:** All arbitrary SVG URLs use strict URL encoding (`%3C`, `%3E`, `%27`, no raw spaces). No base64.
- [ ] **Button recipe:** Every primary CTA uses the canonical Button recipe (§8.E) with `relative z-10`, `items-center`, `shrink-0`, and `w-full sm:inline-flex`.
- [ ] **Motion:** All state transitions use archetype-specific cubic-bezier curves. No `linear`/`ease-in-out` (continuous keyframe loops are the only exception).
- [ ] **Scroll reveal:** global `IntersectionObserver` script present in `Layout.astro` with `is:inline` and `prefers-reduced-motion` fallback. Each `<section>` carries `class="reveal"`.
- [ ] **`[x-cloak]` rule:** present in global CSS so Alpine `x-show` state doesn't flash.
- [ ] **Mobile collapse:** Asymmetric layouts reset to `col-span-1` / `w-full` below `md`. `min-h-[100dvh]`, not `h-screen`. Rotations and negative margins removed below `md`.
- [ ] **Performance:** `backdrop-blur` only on fixed/sticky or Glass Cards over texture. No animating layout properties. Count-up observers disconnect. At most one continuous animation per viewport.
- [ ] **Spatial rhythm:** Section padding `py-24`+, grid gaps `gap-8`+, line heights premium, text balanced.
- [ ] **NO IMAGES.** 100% CSS/SVG only. Faux Thumbnails, mesh gradients, monogram avatars, CSS logo walls.
- [ ] **No font family names** referenced. Typography family is handled by the typography skill.
- [ ] **Theme committed:** No `dark:` variants used. LIGHT for Industrial / Luxury / Clinical; DARK for Precision Tech only.
- [ ] **No `@alpinejs/*` plugins:** accordions use grid-rows, reveals use the global observer, modals use `@keydown.escape.window`.
- [ ] **Observer hygiene:** scoped observers (count-up) disconnect after firing; no per-element observers across long lists.
````

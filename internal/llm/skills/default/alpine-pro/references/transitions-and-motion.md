# Transitions & Motion

`x-transition` is the canonical way to animate `x-show`/`x-if` state changes. **Default transitions are forbidden** — they ship with `linear`-ish easing that looks amateur. Always specify curve + duration.

## The law: cubic-bezier, never `linear` for state

Inherited verbatim from the high-end visual-design library:

> The `linear` ban is for *state transitions*. Marquees/aurora/canvas fields correctly use `linear` (continuous loops are the exception).

For state changes (open/close, show/hide, tab switch, modal in/out), use one of these archetype curves:

| Curve | Vibe | Use for |
|---|---|---|
| `cubic-bezier(0.16, 1, 0.3, 1)` | Organic Luxury — slow-out | Modals, dropdowns, drawers |
| `cubic-bezier(0.32, 0.72, 0, 1)` | Precision Tech — blur-focus | Tech UI reveals |
| `cubic-bezier(0.4, 0, 0.2, 1)` | Clinical Trust — standard | Forms, toggles |
| `cubic-bezier(0.7, 0, 0.3, 1)` | Rugged Industrial — mechanical | Hard stamps, instant snaps |

Durations (Tailwind-style): `duration-150` (micro), `duration-200` (dropdown), `duration-300` (modal), `duration-500` (full reveal), `duration-700`–`duration-1000` (luxury slow-out).

## Anatomy of `x-transition`

```html
<div x-show="open" x-cloak
     x-transition:enter="transition ease-[cubic-bezier(0.16,1,0.3,1)] duration-300"
     x-transition:enter-start="opacity-0 translate-y-4 blur-sm"
     x-transition:enter-end="opacity-100 translate-y-0 blur-0"
     x-transition:leave="transition ease-[cubic-bezier(0.16,1,0.3,1)] duration-200"
     x-transition:leave-start="opacity-100 translate-y-0 blur-0"
     x-transition:leave-end="opacity-0 translate-y-4 blur-sm">
</div>
```

Six phases:
- `x-transition:enter` — classes applied for the whole enter transition (the transition declaration).
- `x-transition:enter-start` — applied on the first frame, removed next frame (initial state).
- `x-transition:enter-end` — applied after `enter-start` is removed (target state).
- `x-transition:leave` / `:leave-start` / `:leave-end` — symmetric for the exit.

Shorthand (single curve for both directions — fine if symmetric):
```html
<div x-show="open" x-cloak
     x-transition:enter="transition ease-[cubic-bezier(0.16,1,0.3,1)] duration-300"
     x-transition:enter-start="opacity-0"
     x-transition:enter-end="opacity-100">
```

## CSS-only transition (no `x-transition`)
```html
<div x-show="open" x-cloak
     style="transition: opacity 0.3s cubic-bezier(0.16,1,0.3,1), transform 0.3s cubic-bezier(0.16,1,0.3,1)">
```
Works for simple opacity/transform cases. `x-transition` is preferred because it integrates with Alpine's lifecycle (delays, JS hooks).

## JS hooks (`x-transition` + `@`)
```html
<div x-show="open"
     x-transition:enter="..." 
     @transitionend.self="onAfterEnter">
</div>
```
Or use `x-on:transitionend` for full control. For full custom timing, skip `x-transition` and drive classes via `$watch` + `$nextTick`.

## Animate `transform` and `opacity` only

Same rule as vanilla JS perf: animating `width`/`height`/`top`/`left` triggers layout and janks on mobile. Stick to:
- `opacity`
- `transform: translate/scale/rotate`
- `filter: blur` (sparingly — expensive on low-end mobile)

```html
<!-- Good -->
x-transition:enter-start="opacity-0 translate-y-4"
x-transition:enter-end="opacity-100 translate-y-0"

<!-- Bad — animates layout -->
x-transition:enter-start="h-0"
x-transition:enter-end="h-64"
```

## `prefers-reduced-motion` — mandatory fallback

```css
@media (prefers-reduced-motion: reduce) {
  [x-transition] { transition: none !important; }
  /* Or if you set transitions via inline classes, override the start/end classes: */
  .reveal { opacity: 1 !important; transform: none !important; transition: none !important; }
}
```
For Alpine components using `x-transition`, the cleanest pattern is to gate the directive itself:
```html
<div x-show="open" x-cloak
     x-transition:enter="transition ease-[cubic-bezier(0.16,1,0.3,1)] duration-300"
     x-transition:enter-start="opacity-0"
     x-transition:enter-end="opacity-100">
```
Then in CSS:
```css
@media (prefers-reduced-motion: reduce) {
  [x-transition\:enter], [x-transition\:leave] { transition-duration: 0.001ms !important; }
}
```

## Continuous animations — the `linear` exception

Marquees, aurora gradients, ambient particle fields — these are infinite loops where `linear` is correct (any easing would create visible hiccups at the loop seam). These are NOT state transitions:
```css
/* Aurora — continuous, linear is right */
.aurora {
  animation: aurora-drift 30s linear infinite;
}
@keyframes aurora-drift {
  0%   { transform: translate(0, 0) rotate(0deg); }
  50%  { transform: translate(-5%, 3%) rotate(180deg); }
  100% { transform: translate(0, 0) rotate(360deg); }
}

/* Marquee */
.marquee-track { animation: marquee 40s linear infinite; }
@keyframes marquee { to { transform: translateX(-50%); } }
```

## Scroll reveals — global `IntersectionObserver`, NOT `x-intersect`

Per the design library: scroll reveals use ONE global `IntersectionObserver` in `Layout.astro` plus `.reveal`/`.reveal.visible` CSS — not the `x-intersect` plugin. Reason: a single observer is more performant than dozens of Alpine-bound observers, and it's framework-agnostic (works for non-Alpine DOM too).

```css
.reveal { opacity: 0; transform: translateY(24px); transition: opacity 0.6s ease-out, transform 0.6s ease-out; }
.reveal.visible { opacity: 1; transform: translateY(0); }
@media (prefers-reduced-motion: reduce) {
  .reveal { opacity: 1; transform: none; filter: none; transition: none; }
}
[x-cloak] { display: none !important; }
```
```astro
<script is:inline>
  const io = new IntersectionObserver((entries, obs) => {
    for (const e of entries) {
      if (e.isIntersecting) { e.target.classList.add('visible'); obs.unobserve(e.target); }
    }
  }, { rootMargin: '100px' });
  document.addEventListener('DOMContentLoaded', () =>
    document.querySelectorAll('.reveal').forEach((el) => io.observe(el)));
</script>
```
Use `class="reveal"` on any `<section>` you want to animate in. The Industrial archetype skips the blur for a sharper mechanical feel.

When IS `x-intersect` OK? Small one-off components where you can't change the global layout — e.g. a single chart that should load its data when scrolled into view. Default to the global observer.

## Stagger reveals

For staggered children inside a `.reveal` parent, use CSS animation-delay:
```css
.stagger > * { opacity: 0; transform: translateY(12px); transition: opacity 0.5s, transform 0.5s; }
.stagger.visible > * { opacity: 1; transform: translateY(0); }
.stagger.visible > *:nth-child(1) { transition-delay: 0ms; }
.stagger.visible > *:nth-child(2) { transition-delay: 80ms; }
.stagger.visible > *:nth-child(3) { transition-delay: 160ms; }
.stagger.visible > *:nth-child(4) { transition-delay: 240ms; }
```

## Transition anti-patterns

| Don't | Why | Do |
|---|---|---|
| Default `x-transition` (no args) | Ships with `linear`-ish ease | Specify `:enter` with cubic-bezier + duration |
| `linear`/`ease-in-out` for state | Looks sloppy, no character | Archetype cubic-bezier |
| Animate `width`/`height`/`top` | Triggers layout, janks | Animate `transform`/`opacity` |
| Skip `prefers-reduced-motion` | Accessibility regression | Mandatory CSS fallback |
| Per-section scroll observers | Performance hit at scale | Global `IntersectionObserver` |
| `x-intersect` for reveals | Tied to Alpine; framework lock-in | Global observer + `.reveal` class |
| `linear` for marquees? OK | Continuous loop — `linear` is correct | (Exception to the ban) |

## Quick reference

| Need | Use |
|---|---|
| State change (open/close) | `x-transition:enter` + `:enter-start` + `:enter-end` with cubic-bezier |
| Archetype curve | `cubic-bezier(0.16,1,0.3,1)` (luxury), `(0.32,0.72,0,1)` (tech), `(0.4,0,0.2,1)` (clinical), `(0.7,0,0.3,1)` (industrial) |
| Continuous loop | `linear` (the only exception) |
| Reduced motion | `@media (prefers-reduced-motion: reduce)` — disable transitions |
| Scroll reveal | Global `IntersectionObserver` + `.reveal`/`.reveal.visible` CSS (not `x-intersect`) |
| Stagger children | CSS `transition-delay` per `nth-child` |
| Animate properties | `transform` + `opacity` only (avoid `width`/`height`/`top`) |

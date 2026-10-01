### B. Scroll Reveal (Global `IntersectionObserver` — canonical for this pipeline)

This pipeline does NOT use Alpine `x-intersect`. Instead, `Layout.astro` ships a global `IntersectionObserver` script (`is:inline`) plus the `.reveal` / `.reveal.visible` CSS below. Put `class="reveal"` on each `<section>` wrapper. Industrial archetype skips the blur for a sharper mechanical feel.
```css
.reveal {
  opacity: 0;
  transform: translateY(24px);
  transition: opacity 0.6s ease-out, transform 0.6s ease-out;
}
.reveal.visible {
  opacity: 1;
  transform: translateY(0);
}
@media (prefers-reduced-motion: reduce) {
  .reveal { opacity: 1; transform: none; filter: none; transition: none; }
}
[x-cloak] { display: none !important; }
```
> The `is:inline` on the observer `<script>` is what keeps Astro from stripping it during build. Do not move this logic into a bundled module. `[x-cloak]` is added here so hidden Alpine state never flashes.


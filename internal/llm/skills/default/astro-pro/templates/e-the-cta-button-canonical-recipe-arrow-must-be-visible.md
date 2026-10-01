### E. The CTA Button (Canonical Recipe — arrow MUST be visible)

The SINGLE canonical button recipe for the whole pipeline. Use it for every primary CTA. The trailing arrow sits in a nested box; the box MUST carry `relative z-10` so the glyph always paints ABOVE the box fill. Use `items-center` (not `items-stretch`), `shrink-0` on the arrow box, and `w-full sm:inline-flex` so the button is full-width on mobile.
```astro
<!-- LIGHT THEME (Industrial / Luxury / Clinical) -->
<a href="#cta" class="group inline-flex w-full sm:inline-flex items-center border-2 border-text-main bg-accent text-white transition-all duration-150 ease-[cubic-bezier(0.7,0,0.3,1)] active:translate-x-1 active:translate-y-1 active:shadow-none shadow-[4px_4px_0px_0px_rgba(28,25,23,1)]">
  <span class="flex items-center px-6 py-3 font-heading font-bold uppercase tracking-widest text-sm">Book Now</span>
  <span class="relative z-10 flex items-center justify-center w-12 shrink-0 border-l-2 border-text-main bg-text-main transition-colors duration-150 group-hover:bg-accent group-hover:text-white">
    <ArrowRight class="w-5 h-5" />
  </span>
</a>

<!-- DARK THEME (Precision Tech only): light ink for the hard offset, surface fill on the arrow box so the white arrow stays visible -->
<a href="#cta" class="group inline-flex w-full sm:inline-flex items-center border-2 border-text-main bg-accent text-white transition-all duration-150 ease-[cubic-bezier(0.32,0.72,0,1)] active:translate-x-1 active:translate-y-1 active:shadow-none shadow-[4px_4px_0px_0px_rgba(244,244,245,1)]">
  <span class="flex items-center px-6 py-3 font-heading font-bold uppercase tracking-widest text-sm">Book Now</span>
  <span class="relative z-10 flex items-center justify-center w-12 shrink-0 border-l-2 border-text-main bg-surface transition-colors duration-150 group-hover:bg-accent group-hover:text-white">
    <ArrowRight class="w-5 h-5" />
  </span>
</a>
```
- **MOBILE SAFETY:** `items-center`, `shrink-0` on the arrow box, `w-full sm:inline-flex`, and `relative z-10` on the arrow box. Without these four, the button looks squashed and the arrow vanishes behind the fill on small screens.


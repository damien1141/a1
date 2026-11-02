### L2. Alternating Spotlight (7/5 then 5/7)

**Use when:** 3–4 features that each deserve narrative space. **Fits:** Luxury, Clinical, Tech.
Each feature is a two-column row with a Faux Thumbnail (§15.B6) or mesh panel on one side and copy on the other. Alternate the side each row so the eye zig-zags.
```astro
<div class="space-y-24 md:space-y-32">
  <!-- Row 1: visual left, copy right (7/5) -->
  <div class="grid grid-cols-1 md:grid-cols-12 gap-8 items-center">
    <div class="md:col-span-7"><!-- Faux Thumbnail (§15.B6) --></div>
    <div class="md:col-span-5">
      <p class="text-[10px] uppercase tracking-[0.2em] text-zinc-500 mb-3">01 — Assessment</p>
      <h3 class="text-3xl font-bold tracking-tight text-balance">Map the terrain.</h3>
      <p class="mt-3 text-text-muted leading-relaxed">We audit before we touch.</p>
    </div>
  </div>
  <!-- Row 2: copy left, visual right (5/7) — MIRROR the columns -->
  <div class="grid grid-cols-1 md:grid-cols-12 gap-8 items-center">
    <div class="md:col-span-5 md:order-1"><!-- copy --></div>
    <div class="md:col-span-7 md:order-2"><!-- Faux Thumbnail --></div>
  </div>
</div>
```


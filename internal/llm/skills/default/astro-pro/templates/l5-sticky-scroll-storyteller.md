### L5. Sticky Scroll Storyteller

**Use when:** a narrative benefits from reading-while-scrolling. **Fits:** Luxury, Tech, Clinical.
Left column is `sticky top-24` (the headline stays pinned); right column scrolls through 3–4 supporting blocks. Below `md`, the sticky column becomes static.
```astro
<div class="grid grid-cols-1 md:grid-cols-12 gap-12">
  <div class="md:col-span-5">
    <div class="md:sticky md:top-32">
      <p class="text-[10px] uppercase tracking-[0.2em] text-zinc-500 mb-4">The Method</p>
      <h2 class="text-4xl md:text-5xl font-bold tracking-tight leading-[1.02] text-balance">Slow, by design.</h2>
      <p class="mt-6 text-text-muted leading-relaxed max-w-sm">Each phase is deliberate. We don't skip.</p>
    </div>
  </div>
  <div class="md:col-span-7 space-y-24">
    <div class="bg-surface-elevated rounded-2xl ring-1 ring-black/5 p-8 min-h-[320px]">
      <h3 class="text-xl font-bold tracking-tight">Phase 01</h3>
    </div>
    <div class="bg-surface-elevated rounded-2xl ring-1 ring-black/5 p-8 min-h-[320px]">
      <h3 class="text-xl font-bold tracking-tight">Phase 02</h3>
    </div>
    <div class="bg-surface-elevated rounded-2xl ring-1 ring-black/5 p-8 min-h-[320px]">
      <h3 class="text-xl font-bold tracking-tight">Phase 03</h3>
    </div>
  </div>
</div>
```


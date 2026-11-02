### C10. Stat / KPI Display

**Use when:** 3–4 hard numbers prove the case. **Fits:** all archetypes.
Big tabular-nums numbers with tiny labels. Use Count-up (§19.M5) for the numbers. Arrange in an asymmetric row, NOT a uniform 4-col grid.
```astro
<div class="grid grid-cols-2 md:grid-cols-12 gap-8">
  <div class="md:col-span-5">
    <p class="text-6xl md:text-7xl font-bold tracking-tight tabular-nums text-accent" x-data="{ count: 0, target: 987 }" x-init="const io = new IntersectionObserver(([e]) => { if (e.isIntersecting) { let s = performance.now(); const t = (n) => { const p = Math.min((n - s) / 1500, 1); count = Math.floor(p * target); if (p < 1) requestAnimationFrame(t); }; requestAnimationFrame(t); io.disconnect(); } }, { threshold: 0.4 }); io.observe($el)" x-text="count.toLocaleString()"></p>
    <p class="mt-2 text-[10px] uppercase tracking-[0.2em] text-text-muted">Repairs completed</p>
  </div>
  <div class="md:col-span-3">
    <p class="text-5xl font-bold tracking-tight tabular-nums">4.9<span class="text-2xl text-text-muted">/5</span></p>
    <p class="mt-2 text-[10px] uppercase tracking-[0.2em] text-text-muted">Avg. rating</p>
  </div>
  <div class="md:col-span-4">
    <p class="text-5xl font-bold tracking-tight tabular-nums">90<span class="text-2xl text-text-muted">s</span></p>
    <p class="mt-2 text-[10px] uppercase tracking-[0.2em] text-text-muted">Median dispatch</p>
  </div>
</div>
```


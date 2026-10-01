### C12. Logo Wall (imageless, CSS monograms)

**Use when:** "as seen in" or partner trust signals. **Fits:** all archetypes.
Stylized text "logos" in hairline boxes — never images. Vary the weight and tracking so they read as distinct brands.
```astro
<div class="grid grid-cols-2 md:grid-cols-4 gap-6">
  <div class="aspect-[3/1] flex items-center justify-center rounded-xl ring-1 ring-black/5 bg-surface-elevated">
    <span class="text-lg font-bold tracking-tighter text-text-muted">Northwind</span>
  </div>
  <div class="aspect-[3/1] flex items-center justify-center rounded-xl ring-1 ring-black/5 bg-surface-elevated">
    <span class="text-lg font-serif italic text-text-muted">Acme&Co</span>
  </div>
  <div class="aspect-[3/1] flex items-center justify-center rounded-xl ring-1 ring-black/5 bg-surface-elevated">
    <span class="text-lg font-light tracking-[0.3em] uppercase text-text-muted">Vertex</span>
  </div>
  <div class="aspect-[3/1] flex items-center justify-center rounded-xl ring-1 ring-black/5 bg-surface-elevated">
    <span class="text-lg font-bold tracking-tight text-text-muted">Helix</span>
  </div>
</div>
```


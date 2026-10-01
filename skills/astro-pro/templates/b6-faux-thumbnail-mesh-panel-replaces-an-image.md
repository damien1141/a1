### B6. Faux Thumbnail (mesh panel replaces an image)

**Use when:** a layout needs a visual block but images are banned. **Fits:** all archetypes.
A mesh-gradient panel with a subtle ring and inset highlight. Can hold a floating label or stat chip.
```astro
<div class="relative rounded-2xl ring-1 ring-black/5 overflow-hidden aspect-[4/3] bg-[radial-gradient(circle_at_30%_30%,#7C8471,transparent_50%),radial-gradient(circle_at_70%_70%,#3D2C1E,transparent_50%),linear-gradient(135deg,#FDFBF7,#7C8471)] shadow-[0_1px_2px_rgba(0,0,0,0.04),0_8px_24px_rgba(0,0,0,0.06)]">
  <div class="absolute inset-0 shadow-[inset_0_1px_1px_rgba(255,255,255,0.4)]"></div>
  <div class="absolute bottom-4 left-4 bg-white/80 backdrop-blur rounded-lg px-3 py-1.5 text-xs font-semibold">Phase 01</div>
</div>
```

---

## 16. TYPOGRAPHY TREATMENT LIBRARY


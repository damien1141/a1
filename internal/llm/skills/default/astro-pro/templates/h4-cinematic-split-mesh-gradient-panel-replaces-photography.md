### H4. Cinematic Split (mesh-gradient panel replaces photography)

**Use when:** the right-side focal area needs visual richness but images are banned. **Fits:** Luxury, Clinical.
Left: headline + CTA. Right: a Double-Bezel wrapping a CSS mesh-gradient panel with a floating stat chip.
```astro
<section id="hero" class="reveal relative overflow-hidden pt-16 md:pt-24 pb-24 md:pb-32">
  <div class="relative z-10 max-w-6xl mx-auto px-6 md:px-8 lg:px-12">
    <div class="grid grid-cols-1 md:grid-cols-12 gap-8 items-center md:min-h-[78dvh]">
      <div class="md:col-span-7">
        <h1 class="text-5xl md:text-7xl font-bold tracking-tight leading-[0.98] text-balance">Concierge dental care, reimagined.</h1>
        <p class="mt-6 text-lg text-text-muted max-w-md leading-relaxed">Bespoke treatment plans. Glacial pacing. Zero pain.</p>
        <!-- Nested CTA pill (§8.F) -->
      </div>
      <div class="md:col-span-5 md:mt-24">
        <div class="p-1.5 rounded-[2rem] bg-black/5 ring-1 ring-black/5">
          <div class="rounded-[calc(2rem-0.375rem)] bg-[radial-gradient(circle_at_20%_20%,#7C8471,transparent_50%),radial-gradient(circle_at_80%_60%,#3D2C1E,transparent_50%),radial-gradient(circle_at_50%_100%,#FDFBF7,#FDFBF7)] p-8 min-h-[360px] relative shadow-[inset_0_1px_1px_rgba(255,255,255,0.8)]">
            <div class="absolute bottom-6 left-6 right-6 bg-white/90 backdrop-blur-xl rounded-2xl p-4 ring-1 ring-black/5">
              <p class="text-[10px] uppercase tracking-[0.2em] text-zinc-500">Patient satisfaction</p>
              <p class="text-3xl font-bold tabular-nums tracking-tight">98.7<span class="text-lg text-zinc-400">%</span></p>
            </div>
          </div>
        </div>
      </div>
    </div>
  </div>
</section>
```


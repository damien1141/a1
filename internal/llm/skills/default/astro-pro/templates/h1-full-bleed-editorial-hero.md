### H1. Full-Bleed Editorial Hero

**Use when:** the headline IS the hero (strong value-prop copy, no data card needed). **Fits:** Luxury, Clinical, Industrial (with `rounded-none`).
A massive left-aligned headline with an eyebrow, a single inline CTA, and a ghost-text word behind it. No right-side card — the type carries the page.
```astro
<section id="hero" class="reveal relative overflow-hidden pt-16 md:pt-24 pb-24 md:pb-32">
  <h2 class="absolute -top-8 left-0 text-[16rem] font-bold text-black/[0.05] tracking-tighter pointer-events-none select-none whitespace-nowrap -z-10">EXEMPLAR</h2>
  <div class="relative z-10 max-w-6xl mx-auto px-6 md:px-8 lg:px-12">
    <p class="text-[11px] uppercase tracking-[0.3em] font-medium text-zinc-500 mb-6">Est. 2014 — Certified</p>
    <h1 class="text-5xl md:text-7xl lg:text-8xl font-bold tracking-tight leading-[0.95] text-balance max-w-4xl">
      Boiler repair that doesn't <span class="italic font-light text-zinc-500">wait for Monday.</span>
    </h1>
    <div class="mt-10 flex flex-col sm:flex-row items-start sm:items-center gap-6">
      <!-- Canonical CTA Button (§8.E) -->
      <p class="text-sm text-text-muted max-w-xs leading-relaxed">Same-day dispatch across the metro. Engineers on call 24/7.</p>
    </div>
  </div>
</section>
```


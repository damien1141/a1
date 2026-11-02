### H2. Bento Mosaic Hero

**Use when:** you have 4–6 distinct value props and want to show range immediately. **Fits:** Tech, Clinical, Industrial (with `rounded-none` and hard borders).
A 12-column grid with mixed cell sizes. One large headline cell, one CTA cell, and 3–4 small feature cells of varying spans.
```astro
<section id="hero" class="reveal relative overflow-hidden pt-16 md:pt-24 pb-24 md:pb-32">
  <div class="relative z-10 max-w-6xl mx-auto px-6 md:px-8 lg:px-12">
    <div class="grid grid-cols-2 md:grid-cols-12 gap-4 md:gap-6">
      <!-- Headline cell (large) -->
      <div class="col-span-2 md:col-span-7 md:row-span-2 bg-surface-elevated p-8 md:p-10 rounded-2xl ring-1 ring-black/5">
        <p class="text-[10px] uppercase tracking-[0.2em] text-zinc-500 mb-4">Tier-1 Infrastructure</p>
        <h1 class="text-4xl md:text-6xl font-bold tracking-tight leading-[1.02] text-balance">Deploy in 90 seconds.</h1>
        <p class="mt-4 text-text-muted max-w-md leading-relaxed">Edge runtime with sub-millisecond cold starts.</p>
      </div>
      <!-- CTA cell -->
      <div class="md:col-span-5 bg-accent text-white p-6 rounded-2xl flex flex-col justify-between min-h-[140px]">
        <span class="text-xs uppercase tracking-widest font-bold">Start Free</span>
        <ArrowRight class="w-6 h-6 self-end" />
      </div>
      <!-- Small feature cells -->
      <div class="md:col-span-3 bg-surface-elevated p-5 rounded-2xl ring-1 ring-black/5">
        <Zap class="w-5 h-5 text-accent mb-3" />
        <p class="text-sm font-semibold">0.4ms p99</p>
      </div>
      <div class="md:col-span-2 bg-surface-elevated p-5 rounded-2xl ring-1 ring-black/5">
        <ShieldCheck class="w-5 h-5 text-accent mb-3" />
        <p class="text-sm font-semibold">SOC 2</p>
      </div>
      <div class="md:col-span-2 bg-surface-elevated p-5 rounded-2xl ring-1 ring-black/5">
        <Globe class="w-5 h-5 text-accent mb-3" />
        <p class="text-sm font-semibold">38 regions</p>
      </div>
    </div>
  </div>
</section>
```


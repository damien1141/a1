### V1. Glow Card (Tech signature)

**Use when:** Tech focal points that aren't the hero. **Fits:** Precision Tech only (DARK).
```astro
<div class="relative rounded-2xl ring-1 ring-white/10 bg-white/[0.02] p-8 shadow-[0_0_40px_-10px_rgba(59,130,246,0.5)] overflow-hidden">
  <div class="absolute -top-20 -right-20 w-40 h-40 rounded-full bg-accent/20 blur-3xl pointer-events-none"></div>
  <Zap class="w-6 h-6 text-accent relative z-10" />
  <h3 class="mt-4 text-xl font-bold tracking-tight relative z-10 text-white">Edge runtime</h3>
  <p class="mt-2 text-sm text-zinc-400 leading-relaxed relative z-10">Sub-millisecond cold starts.</p>
</div>
```


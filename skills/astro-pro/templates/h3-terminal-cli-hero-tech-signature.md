### H3. Terminal / CLI Hero (Tech signature)

**Use when:** Tech archetype and developer audience. **Fits:** Precision Tech only (DARK).
A simulated terminal window on one side, headline + CTA on the other. The terminal uses the Typewriter motion (§19.M7) for the prompt lines.
```astro
<section id="hero" class="reveal relative overflow-hidden pt-16 md:pt-24 pb-24 md:pb-32">
  <div class="relative z-10 max-w-6xl mx-auto px-6 md:px-8 lg:px-12">
    <div class="grid grid-cols-1 md:grid-cols-12 gap-8 items-center md:min-h-[78dvh]">
      <div class="md:col-span-6">
        <p class="text-[10px] uppercase tracking-[0.3em] font-mono text-accent mb-6">$ init deploy</p>
        <h1 class="text-5xl md:text-7xl font-bold tracking-tight leading-[0.98] text-balance text-white">Ship to the edge.</h1>
        <p class="mt-6 text-zinc-400 max-w-md leading-relaxed">A runtime built for engineers who measure latency in microseconds.</p>
        <!-- Canonical CTA (dark variant, §8.E) -->
      </div>
      <div class="md:col-span-6">
        <!-- Terminal Window (§12.C13) -->
        <div class="rounded-xl ring-1 ring-white/10 bg-[#020203] shadow-[0_0_40px_-10px_rgba(59,130,246,0.5)] overflow-hidden">
          <div class="flex items-center gap-2 px-4 py-3 border-b border-white/10">
            <span class="w-3 h-3 rounded-full bg-red-500/80"></span>
            <span class="w-3 h-3 rounded-full bg-yellow-500/80"></span>
            <span class="w-3 h-3 rounded-full bg-green-500/80"></span>
            <span class="ml-3 text-xs font-mono text-zinc-500">~/project</span>
          </div>
          <div class="p-6 font-mono text-sm leading-relaxed" x-data="{ lines: ['$ bun create edge', '→ Resolving runtime...', '→ Deploying to 38 regions...', '✓ Live in 1.2s'], shown: '', i: 0, j: 0 }" x-init="const tick = () => { if (i < lines.length) { if (j <= lines[i].length) { shown = lines.slice(0,i).join('\n') + '\n' + lines[i].slice(0,j); j++; setTimeout(tick, 28); } else { i++; j=0; setTimeout(tick, 400); } } }; tick()" x-text="shown"></div>
        </div>
      </div>
    </div>
  </div>
</section>
```


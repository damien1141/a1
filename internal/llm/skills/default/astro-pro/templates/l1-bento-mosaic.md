### L1. Bento Mosaic

**Use when:** 5–7 features of varying importance. **Fits:** Tech, Clinical, Industrial (`rounded-none`).
A 12-column grid with deliberately unequal cell sizes. The largest cell holds the hero feature; small cells hold stats or single-icon features.
```astro
<div class="grid grid-cols-1 md:grid-cols-12 gap-4 md:gap-6">
  <div class="md:col-span-6 md:row-span-2 bg-surface-elevated rounded-2xl ring-1 ring-black/5 p-8 min-h-[280px]">
    <h3 class="text-2xl font-bold tracking-tight">Primary feature</h3>
    <p class="mt-2 text-text-muted text-sm leading-relaxed max-w-sm">The load-bearing value prop lives here with the most real estate.</p>
  </div>
  <div class="md:col-span-3 bg-surface-elevated rounded-2xl ring-1 ring-black/5 p-6">
    <Zap class="w-5 h-5 text-accent mb-3" />
    <p class="text-sm font-semibold">Fast</p>
    <p class="text-xs text-text-muted mt-1">0.4ms p99</p>
  </div>
  <div class="md:col-span-3 bg-accent text-white rounded-2xl p-6">
    <ShieldCheck class="w-5 h-5 mb-3" />
    <p class="text-sm font-semibold">Secure</p>
    <p class="text-xs opacity-80 mt-1">SOC 2 Type II</p>
  </div>
  <div class="md:col-span-3 bg-surface-elevated rounded-2xl ring-1 ring-black/5 p-6">
    <Globe class="w-5 h-5 text-accent mb-3" />
    <p class="text-sm font-semibold">Global</p>
    <p class="text-xs text-text-muted mt-1">38 regions</p>
  </div>
  <div class="md:col-span-3 bg-surface-elevated rounded-2xl ring-1 ring-black/5 p-6">
    <GitBranch class="w-5 h-5 text-accent mb-3" />
    <p class="text-sm font-semibold">Versioned</p>
    <p class="text-xs text-text-muted mt-1">Atomic rolls</p>
  </div>
</div>
```


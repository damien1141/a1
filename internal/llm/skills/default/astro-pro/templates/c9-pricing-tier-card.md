### C9. Pricing Tier Card

**Use when:** SaaS or transparent service pricing. **Fits:** Tech, Clinical.
Three tiers in a grid, but the middle tier is elevated (scale, accent bg, "Most Popular" badge). NOT three identical cards.
```astro
<div class="grid grid-cols-1 md:grid-cols-3 gap-6 items-center">
  <div class="bg-surface-elevated rounded-2xl ring-1 ring-black/5 p-8">
    <h3 class="text-sm font-bold uppercase tracking-widest text-text-muted">Starter</h3>
    <p class="mt-4 text-4xl font-bold tracking-tight tabular-nums">$29<span class="text-base font-normal text-text-muted">/mo</span></p>
    <ul class="mt-6 space-y-3 text-sm text-text-muted">
      <li class="flex items-center gap-2"><Check class="w-4 h-4 text-accent" /> 1 project</li>
      <li class="flex items-center gap-2"><Check class="w-4 h-4 text-accent" /> Community support</li>
    </ul>
  </div>
  <div class="bg-accent text-white rounded-2xl p-8 shadow-[0_24px_60px_rgba(59,130,246,0.3)] md:scale-105 ring-1 ring-accent relative">
    <span class="absolute -top-3 left-1/2 -translate-x-1/2 bg-white text-accent text-[10px] font-bold uppercase tracking-widest px-3 py-1 rounded-full">Most Popular</span>
    <h3 class="text-sm font-bold uppercase tracking-widest opacity-90">Pro</h3>
    <p class="mt-4 text-4xl font-bold tracking-tight tabular-nums">$99<span class="text-base font-normal opacity-80">/mo</span></p>
    <ul class="mt-6 space-y-3 text-sm opacity-90">
      <li class="flex items-center gap-2"><Check class="w-4 h-4" /> Unlimited projects</li>
      <li class="flex items-center gap-2"><Check class="w-4 h-4" /> Priority support</li>
    </ul>
  </div>
  <div class="bg-surface-elevated rounded-2xl ring-1 ring-black/5 p-8">
    <h3 class="text-sm font-bold uppercase tracking-widest text-text-muted">Enterprise</h3>
    <p class="mt-4 text-4xl font-bold tracking-tight tabular-nums">Custom</p>
    <ul class="mt-6 space-y-3 text-sm text-text-muted">
      <li class="flex items-center gap-2"><Check class="w-4 h-4 text-accent" /> Dedicated engineer</li>
      <li class="flex items-center gap-2"><Check class="w-4 h-4 text-accent" /> 99.99% SLA</li>
    </ul>
  </div>
</div>
```


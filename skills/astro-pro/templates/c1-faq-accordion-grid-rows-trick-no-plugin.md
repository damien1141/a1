### C1. FAQ Accordion (grid-rows trick — no plugin)

**Use when:** FAQ needs to collapse/expand. **Fits:** all archetypes.
Pure CSS height animation via `grid-template-rows: 0fr → 1fr`. No `@alpinejs/collapse` plugin needed. Each item is its own Alpine scope.
```astro
<div class="max-w-3xl mx-auto divide-y divide-black/5" x-data="{ open: null }">
  <div>
    <button @click="open === 0 ? open = null : open = 0" class="w-full flex items-center justify-between py-6 text-left">
      <span class="text-lg font-semibold tracking-tight">How fast can you dispatch?</span>
      <ChevronDown class="w-5 h-5 shrink-0 transition-transform duration-300 ease-[cubic-bezier(0.16,1,0.3,1)]" :class="open === 0 ? 'rotate-180' : ''" />
    </button>
    <div class="grid transition-all duration-500 ease-[cubic-bezier(0.16,1,0.3,1)]" :class="open === 0 ? 'grid-rows-[1fr] opacity-100' : 'grid-rows-[0fr] opacity-0'">
      <div class="overflow-hidden">
        <p class="pb-6 text-text-muted leading-relaxed max-w-prose">Same-day for metro postcodes. Book before 11am for morning slots.</p>
      </div>
    </div>
  </div>
  <!-- repeat per question, incrementing the index -->
</div>
```


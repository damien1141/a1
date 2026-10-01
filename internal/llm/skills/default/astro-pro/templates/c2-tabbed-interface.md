### C2. Tabbed Interface

**Use when:** 3–5 related content panels (e.g., plan tiers, feature categories). **Fits:** Tech, Clinical, Industrial.
Alpine `x-data="{ tab: 'a' }"` with `@click` and `x-show`. Animated underline follows the active tab.
```astro
<div x-data="{ tab: 'a' }">
  <div class="flex gap-2 border-b border-black/5 mb-8">
    <button @click="tab = 'a'" :class="tab === 'a' ? 'border-accent text-text-main' : 'border-transparent text-text-muted'" class="px-4 py-3 text-sm font-semibold border-b-2 transition-colors">Overview</button>
    <button @click="tab = 'b'" :class="tab === 'b' ? 'border-accent text-text-main' : 'border-transparent text-text-muted'" class="px-4 py-3 text-sm font-semibold border-b-2 transition-colors">Specs</button>
    <button @click="tab = 'c'" :class="tab === 'c' ? 'border-accent text-text-main' : 'border-transparent text-text-muted'" class="px-4 py-3 text-sm font-semibold border-b-2 transition-colors">Pricing</button>
  </div>
  <div x-show="tab === 'a'" x-transition:enter="transition ease-[cubic-bezier(0.16,1,0.3,1)] duration-400" x-transition:enter-start="opacity-0 translate-y-2" x-transition:enter-end="opacity-100 translate-y-0" class="bg-surface-elevated rounded-2xl ring-1 ring-black/5 p-8">
    <h3 class="text-xl font-bold tracking-tight">Overview content</h3>
  </div>
  <div x-show="tab === 'b'" x-cloak x-transition:enter="transition ease-[cubic-bezier(0.16,1,0.3,1)] duration-400" x-transition:enter-start="opacity-0 translate-y-2" x-transition:enter-end="opacity-100 translate-y-0" class="bg-surface-elevated rounded-2xl ring-1 ring-black/5 p-8">
    <h3 class="text-xl font-bold tracking-tight">Specs content</h3>
  </div>
  <div x-show="tab === 'c'" x-cloak x-transition:enter="transition ease-[cubic-bezier(0.16,1,0.3,1)] duration-400" x-transition:enter-start="opacity-0 translate-y-2" x-transition:enter-end="opacity-100 translate-y-0" class="bg-surface-elevated rounded-2xl ring-1 ring-black/5 p-8">
    <h3 class="text-xl font-bold tracking-tight">Pricing content</h3>
  </div>
</div>
```


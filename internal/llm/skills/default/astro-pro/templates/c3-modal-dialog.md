### C3. Modal Dialog

**Use when:** a detail, video substitute, or form needs to overlay without a page change. **Fits:** all archetypes.
Alpine `x-show` + `x-transition` + `@keydown.escape.window`. Focus trap is basic; add `aria-modal="true"` and `role="dialog"`.
```astro
<div x-data="{ open: false }">
  <button @click="open = true" class="text-accent underline">View details</button>
  <div x-show="open" x-transition.opacity.duration.200ms class="fixed inset-0 z-50 bg-black/60 backdrop-blur-sm flex items-center justify-center p-4" x-cloak @keydown.escape.window="open = false">
    <div x-show="open" x-transition:enter="transition ease-[cubic-bezier(0.16,1,0.3,1)] duration-400" x-transition:enter-start="opacity-0 scale-95 translate-y-4" x-transition:enter-end="opacity-100 scale-100 translate-y-0" @click.outside="open = false" role="dialog" aria-modal="true" class="bg-surface-elevated rounded-2xl ring-1 ring-black/10 shadow-[0_24px_80px_rgba(0,0,0,0.2)] max-w-lg w-full p-8 relative">
      <button @click="open = false" class="absolute top-4 right-4 text-text-muted hover:text-text-main" aria-label="Close">
        <X class="w-5 h-5" />
      </button>
      <h3 class="text-2xl font-bold tracking-tight">Detail</h3>
      <p class="mt-2 text-text-muted leading-relaxed">Modal content.</p>
    </div>
  </div>
</div>
```


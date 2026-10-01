### C4. Slide-Over Panel

**Use when:** a form, filters, or nav drawer slides in from the right. **Fits:** all archetypes.
```astro
<div x-data="{ open: false }">
  <button @click="open = true" class="text-accent underline">Open panel</button>
  <div x-show="open" x-transition.opacity.duration.200ms class="fixed inset-0 z-50 bg-black/50" x-cloak @click="open = false"></div>
  <div x-show="open" x-transition:enter="transition ease-[cubic-bezier(0.16,1,0.3,1)] duration-500" x-transition:enter-start="translate-x-full" x-transition:enter-end="translate-x-0" x-transition:leave="transition ease-[cubic-bezier(0.16,1,0.3,1)] duration-300" x-transition:leave-start="translate-x-0" x-transition:leave-end="translate-x-full" class="fixed top-0 right-0 z-50 h-full w-full max-w-md bg-surface-elevated shadow-[0_0_80px_rgba(0,0,0,0.2)] p-8 overflow-y-auto" x-cloak>
    <button @click="open = false" class="absolute top-4 right-4"><X class="w-5 h-5" /></button>
    <h3 class="text-xl font-bold tracking-tight">Panel content</h3>
  </div>
</div>
```


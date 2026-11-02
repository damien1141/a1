### N2. Scroll-Shrink Island

**Use when:** the hero is tall and you want the nav to recede as the user scrolls. **Fits:** all archetypes.
The Fluid Island shrinks (smaller padding, smaller logo) after the user scrolls past a threshold. Uses `@scroll.window` (Alpine directive, passive, compliant — not a vanilla listener).
```astro
<header class="fixed top-6 left-1/2 -translate-x-1/2 z-50 transition-all duration-500 ease-[cubic-bezier(0.16,1,0.3,1)]" x-data="{ shrunk: false }" @scroll.window="shrunk = window.scrollY > 120" :class="shrunk ? 'top-3 scale-95' : 'top-6 scale-100'">
  <nav class="flex items-center gap-1 rounded-full bg-white/80 backdrop-blur-2xl ring-1 ring-black/5 px-2 py-2 transition-all duration-500" :class="shrunk ? 'shadow-[0_4px_16px_rgba(0,0,0,0.08)]' : 'shadow-[0_8px_32px_rgba(0,0,0,0.08)]'">
    <span class="px-3 text-sm font-bold tracking-tight transition-all" :class="shrunk ? 'text-sm' : 'text-base'">LOGO</span>
    <a href="#" class="rounded-full px-4 py-2 text-sm font-medium text-zinc-900 hover:bg-black/5">Home</a>
    <a href="#" class="rounded-full bg-zinc-900 px-4 py-2 text-sm font-medium text-white">Book</a>
  </nav>
</header>
```


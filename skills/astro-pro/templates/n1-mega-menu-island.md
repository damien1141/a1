### N1. Mega-Menu Island

**Use when:** the site has many service categories that need a rich preview. **Fits:** Industrial, Clinical, Tech.
A Fluid Island nav where one link opens a mega-panel (Alpine `x-data` + `x-show` + `x-transition`). The panel is a wide floating card with category columns.
```astro
<header class="fixed top-6 left-1/2 -translate-x-1/2 z-50" x-data="{ mega: false }">
  <nav class="flex items-center gap-1 rounded-full bg-white/80 backdrop-blur-2xl ring-1 ring-black/5 px-2 py-2 shadow-[0_8px_32px_rgba(0,0,0,0.08)]">
    <a href="#" class="rounded-full px-4 py-2 text-sm font-medium text-zinc-900 hover:bg-black/5">Home</a>
    <button @mouseenter="mega = true" @mouseleave="mega = false" class="relative rounded-full px-4 py-2 text-sm font-medium text-zinc-900 hover:bg-black/5 flex items-center gap-1.5">
      Services
      <ChevronDown class="w-3.5 h-3.5 transition-transform" :class="mega ? 'rotate-180' : ''" />
    </button>
    <a href="#" class="rounded-full bg-zinc-900 px-4 py-2 text-sm font-medium text-white">Book</a>
  </nav>
  <div x-show="mega" x-transition:enter="transition ease-[cubic-bezier(0.16,1,0.3,1)] duration-300" x-transition:enter-start="opacity-0 -translate-y-2" x-transition:enter-end="opacity-100 translate-y-0" x-transition:leave="transition ease-[cubic-bezier(0.16,1,0.3,1)] duration-200" x-transition:leave-start="opacity-100" x-transition:leave-end="opacity-0" @mouseenter="mega = true" @mouseleave="mega = false" class="absolute top-full left-1/2 -translate-x-1/2 mt-3 w-[640px] bg-white rounded-2xl ring-1 ring-black/5 shadow-[0_20px_60px_rgba(0,0,0,0.12)] p-6 grid grid-cols-2 gap-6" x-cloak>
    <div>
      <p class="text-[10px] uppercase tracking-[0.2em] text-zinc-400 mb-3">Residential</p>
      <a href="#" class="block py-1.5 text-sm text-zinc-900 hover:text-accent">Boiler repair</a>
      <a href="#" class="block py-1.5 text-sm text-zinc-900 hover:text-accent">Pipe relining</a>
    </div>
    <div>
      <p class="text-[10px] uppercase tracking-[0.2em] text-zinc-400 mb-3">Commercial</p>
      <a href="#" class="block py-1.5 text-sm text-zinc-900 hover:text-accent">Plant maintenance</a>
      <a href="#" class="block py-1.5 text-sm text-zinc-900 hover:text-accent">Compliance audits</a>
    </div>
  </div>
</header>
```
> Add `[x-cloak]{display:none!important}` to the global shell CSS so hidden Alpine state doesn't flash.


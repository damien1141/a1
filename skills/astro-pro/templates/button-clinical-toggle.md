### Button · Clinical Toggle

**Use when:** filter or preference toggle. Steady state swap.
**Imports:** `import { Check } from 'lucide-astro';`
```astro
<button x-data="{ on: false }" @click="on = !on" :class="on ? 'bg-[#0D9488] text-white ring-[#0D9488]' : 'bg-white text-slate-700 ring-slate-900/10'" class="inline-flex items-center gap-2 rounded-full ring-1 px-4 py-2 text-sm font-semibold transition-all duration-400 ease-[cubic-bezier(0.4,0,0.2,1)] active:scale-[0.96]">
  <Check x-show="on" x-cloak class="w-3.5 h-3.5" />
  <span x-text="on ? 'Enabled' : 'Enable alerts'"></span>
</button>
```


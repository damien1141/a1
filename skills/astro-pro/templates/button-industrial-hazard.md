### Button · Industrial Hazard

**Use when:** emergency / urgent CTA (24/7 dispatch). Amber fill, high contrast.
**Imports:** `import { ArrowRight, AlertTriangle } from 'lucide-astro';`
```astro
<a href="#cta" class="group inline-flex w-full sm:inline-flex items-center border-2 border-zinc-900 bg-amber-400 text-zinc-900 transition-all duration-150 ease-[cubic-bezier(0.7,0,0.3,1)] active:translate-x-1 active:translate-y-1 active:shadow-none shadow-[4px_4px_0px_0px_rgba(24,24,27,1)]">
  <span class="flex items-center gap-2 px-6 py-3 font-heading font-bold uppercase tracking-widest text-sm">
    <AlertTriangle class="w-4 h-4" /> Emergency Dispatch
  </span>
  <span class="relative z-10 flex items-center justify-center w-12 shrink-0 border-l-2 border-zinc-900 bg-zinc-900 text-amber-400 transition-colors duration-150 group-hover:bg-amber-400 group-hover:text-zinc-900">
    <ArrowRight class="w-5 h-5" />
  </span>
</a>
```


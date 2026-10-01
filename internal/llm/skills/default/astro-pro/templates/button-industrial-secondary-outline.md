### Button · Industrial Secondary Outline

**Use when:** secondary action alongside the primary stamp. Same mass, no fill.
**Imports:** `import { ArrowRight } from 'lucide-astro';`
```astro
<a href="#" class="group inline-flex w-full sm:inline-flex items-center border-2 border-text-main bg-transparent text-text-main transition-all duration-150 ease-[cubic-bezier(0.7,0,0.3,1)] active:translate-x-1 active:translate-y-1 active:shadow-none shadow-[4px_4px_0px_0px_rgba(28,25,23,1)] hover:bg-text-main hover:text-surface">
  <span class="flex items-center px-6 py-3 font-heading font-bold uppercase tracking-widest text-sm">Spec Sheet</span>
  <span class="relative z-10 flex items-center justify-center w-12 shrink-0 border-l-2 border-text-main transition-colors duration-150 group-hover:bg-text-main group-hover:text-surface">
    <ArrowRight class="w-5 h-5" />
  </span>
</a>
```


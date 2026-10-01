### Button · Industrial Primary Stamp

**Use when:** the primary CTA on an Industrial page (Hero, CTA section). Hard offset stamp that shifts on press.
**Imports:** `import { ArrowRight } from 'lucide-astro';`
```astro
<a href="#cta" class="group inline-flex w-full sm:inline-flex items-center border-2 border-text-main bg-accent text-white transition-all duration-150 ease-[cubic-bezier(0.7,0,0.3,1)] active:translate-x-1 active:translate-y-1 active:shadow-none shadow-[4px_4px_0px_0px_rgba(28,25,23,1)]">
  <span class="flex items-center px-6 py-3 font-heading font-bold uppercase tracking-widest text-sm">Book Now</span>
  <span class="relative z-10 flex items-center justify-center w-12 shrink-0 border-l-2 border-text-main bg-text-main transition-colors duration-150 group-hover:bg-accent group-hover:text-white">
    <ArrowRight class="w-5 h-5" />
  </span>
</a>
```


### Button · Tech Primary Glow

**Use when:** primary CTA on Tech (DARK). Springy, glow halo, nested arrow.
**Imports:** `import { ArrowRight } from 'lucide-astro';`
```astro
<a href="#cta" class="group inline-flex w-full sm:inline-flex items-center rounded-xl bg-[#3B82F4] text-white transition-all duration-300 ease-[cubic-bezier(0.32,0.72,0,1)] active:scale-[0.97] shadow-[0_0_30px_-5px_rgba(59,130,246,0.6)] ring-1 ring-white/10">
  <span class="flex items-center px-6 py-3 font-heading font-semibold tracking-tight text-sm">Deploy Now</span>
  <span class="relative z-10 flex items-center justify-center w-11 shrink-0 m-1 rounded-lg bg-white/10 transition-all duration-300 group-hover:bg-white/20">
    <ArrowRight class="w-4 h-4" />
  </span>
</a>
```


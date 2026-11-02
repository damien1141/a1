### Button · Tech Gradient Border

**Use when:** high-emphasis CTA needing premium accent. Gradient border via 1px padding + bg-clip.
**Imports:** `import { ArrowRight } from 'lucide-astro';`
```astro
<a href="#cta" class="group inline-flex items-center rounded-xl bg-[linear-gradient(110deg,#3B82F4,#10B981)] p-px transition-all duration-300 ease-[cubic-bezier(0.32,0.72,0,1)] active:scale-[0.97] hover:shadow-[0_0_40px_-8px_rgba(59,130,246,0.6)]">
  <span class="inline-flex items-center gap-2 rounded-[calc(0.75rem-1px)] bg-[#020203] px-6 py-3 text-white transition-colors duration-300 group-hover:bg-[#020203]/80">
    <span class="text-sm font-semibold tracking-tight">Start Free</span>
    <ArrowRight class="w-4 h-4 text-[#3B82F4] transition-transform group-hover:translate-x-0.5" />
  </span>
</a>
```


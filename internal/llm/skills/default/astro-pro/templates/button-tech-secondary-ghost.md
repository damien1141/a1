### Button · Tech Secondary Ghost

**Use when:** secondary action on dark. Border + glow on hover.
**Imports:** `import { ArrowRight } from 'lucide-astro';`
```astro
<a href="#" class="group inline-flex items-center gap-2 rounded-xl border border-white/10 bg-white/[0.02] text-white px-5 py-3 transition-all duration-300 ease-[cubic-bezier(0.32,0.72,0,1)] hover:border-[#3B82F4]/40 hover:bg-white/[0.05] hover:shadow-[0_0_30px_-10px_rgba(59,130,246,0.6)] active:scale-[0.97]">
  <span class="text-sm font-medium tracking-tight">View Docs</span>
  <ArrowRight class="w-4 h-4 opacity-60 transition-all group-hover:opacity-100 group-hover:translate-x-0.5" />
</a>
```


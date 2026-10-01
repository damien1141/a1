### Button · Tech Terminal

**Use when:** developer-facing action with CLI vibe. Mono font, $ prefix.
**Imports:** `import { ChevronRight } from 'lucide-astro';`
```astro
<a href="#" class="group inline-flex items-center gap-1.5 rounded-lg border border-white/10 bg-[#020203] text-zinc-300 px-4 py-2.5 font-mono text-sm transition-all duration-300 ease-[cubic-bezier(0.32,0.72,0,1)] hover:border-[#10B981]/40 hover:text-white hover:shadow-[0_0_24px_-8px_rgba(16,185,129,0.5)] active:scale-[0.97]">
  <span class="text-[#10B981]">$</span>
  <span>bun create edge</span>
  <ChevronRight class="w-4 h-4 opacity-40 transition-all group-hover:opacity-100 group-hover:translate-x-0.5" />
</a>
```


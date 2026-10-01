### Button · Clinical Icon Stat

**Use when:** CTA with embedded proof stat. Data-forward.
**Imports:** `import { Calendar } from 'lucide-astro';`
```astro
<a href="#book" class="group inline-flex items-center gap-3 rounded-xl bg-white ring-1 ring-slate-900/8 text-slate-900 pl-5 pr-3 py-3 transition-all duration-400 ease-[cubic-bezier(0.4,0,0.2,1)] hover:ring-[#0D9488]/40 hover:shadow-[0_8px_24px_rgba(15,23,42,0.06)] active:scale-[0.98]">
  <Calendar class="w-5 h-5 text-[#0D9488]" />
  <span class="text-sm font-semibold tracking-tight">Book</span>
  <span class="ml-2 flex items-center gap-1.5 rounded-lg bg-slate-100 px-2 py-1 text-[11px] font-mono font-medium text-slate-600">
    <span class="w-1.5 h-1.5 rounded-full bg-green-500"></span> 90s avg
  </span>
</a>
```


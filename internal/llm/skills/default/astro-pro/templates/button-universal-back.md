### Button · Universal Back

**Use when:** previous-page or multi-step back. Ghost with chevron.
**Imports:** `import { ChevronLeft } from 'lucide-astro';`
```astro
<button class="group inline-flex items-center gap-1 text-text-muted hover:text-text-main transition-colors duration-300 ease-[cubic-bezier(0.16,1,0.3,1)]">
  <ChevronLeft class="w-4 h-4 transition-transform duration-300 group-hover:-translate-x-0.5" />
  <span class="text-sm font-medium">Back</span>
</button>
```


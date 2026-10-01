### Button · Universal Scroll Cue

**Use when:** hero scroll hint. Bouncing chevron, links to next section.
**Imports:** `import { ChevronDown } from 'lucide-astro';`
```astro
<a href="#services" class="group inline-flex flex-col items-center gap-2 text-text-muted transition-colors duration-300 hover:text-text-main" aria-label="Scroll to content">
  <span class="text-[10px] uppercase tracking-[0.3em] font-medium">Scroll</span>
  <span class="flex h-9 w-9 items-center justify-center rounded-full ring-1 ring-black/10">
    <ChevronDown class="w-4 h-4 animate-bounce" />
  </span>
</a>
```


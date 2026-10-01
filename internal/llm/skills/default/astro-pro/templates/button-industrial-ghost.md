### Button · Industrial Ghost

**Use when:** tertiary text action with industrial restraint. No border, no shadow.
**Imports:** `import { ArrowRight } from 'lucide-astro';`
```astro
<a href="#" class="group relative inline-flex items-center gap-2 px-2 py-2 text-text-main transition-colors duration-150 ease-[cubic-bezier(0.7,0,0.3,1)]">
  <span class="font-heading font-bold uppercase tracking-widest text-sm">Read brief</span>
  <span class="absolute bottom-1 left-2 right-12 h-0.5 bg-text-main origin-left scale-x-0 group-hover:scale-x-100 transition-transform duration-150 ease-[cubic-bezier(0.7,0,0.3,1)]"></span>
  <ArrowRight class="w-4 h-4 transition-transform duration-150 group-hover:translate-x-1" />
</a>
```


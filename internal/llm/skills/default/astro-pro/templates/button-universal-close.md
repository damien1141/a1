### Button · Universal Close

**Use when:** dismiss modals, sheets, banners. Circular, rotate on hover.
**Imports:** `import { X } from 'lucide-astro';`
```astro
<button class="group inline-flex items-center justify-center w-9 h-9 rounded-full ring-1 ring-black/10 bg-surface-elevated text-text-muted transition-all duration-300 ease-[cubic-bezier(0.16,1,0.3,1)] hover:text-text-main hover:ring-black/20 active:scale-90" aria-label="Close">
  <X class="w-4 h-4 transition-transform duration-300 group-hover:rotate-90" />
</button>
```


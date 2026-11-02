### A. Alpine `x-transition` (State Changes)

Never use default transitions. Always specify curve + duration.
```astro
<div x-data="{ open: false }" x-show="open" x-transition:enter="transition ease-[cubic-bezier(0.16,1,0.3,1)] duration-500" x-transition:enter-start="opacity-0 translate-y-4 blur-sm" x-transition:enter-end="opacity-100 translate-y-0 blur-0">
  <!-- content -->
</div>
```


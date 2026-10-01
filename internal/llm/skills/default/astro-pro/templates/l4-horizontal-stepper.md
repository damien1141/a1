### L4. Horizontal Stepper

**Use when:** a short 3–4 step process with clear milestones. **Fits:** Tech, Clinical.
A horizontal row of numbered nodes connected by hairlines. Collapses to vertical below `md`.
```astro
<div class="grid grid-cols-1 md:grid-cols-4 gap-8">
  <div class="relative">
    <div class="flex items-center gap-3">
      <span class="flex items-center justify-center w-10 h-10 rounded-full bg-accent text-white font-bold tabular-nums">1</span>
      <div class="hidden md:block flex-1 h-px bg-black/10"></div>
    </div>
    <h3 class="mt-4 text-lg font-bold tracking-tight">Connect</h3>
    <p class="mt-1 text-sm text-text-muted leading-relaxed">Link your repo.</p>
  </div>
  <!-- repeat; last step omits the connecting line -->
</div>
```


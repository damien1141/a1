### B5. Section Dividers (4 sub-variants)

**Use when:** transitioning between sections without flat cuts.

**Angled cut:**
```astro
<div class="relative h-16 -mb-px bg-surface-elevated [clip-path:polygon(0_100%,100%_0,100%_100%,0_100%)]"></div>
```

**Wave (SVG, JIT-safe):**
```astro
<div class="h-16 bg-[url('data:image/svg+xml,%3Csvg%20xmlns=%27http://www.w3.org/2000/svg%27%20viewBox=%270_0_1440_80%27%20preserveAspectRatio=%27none%27%3E%3Cpath%20d=%27M0,40%20C240,80%20480,0%20720,40%20C960,80%201200,0%201440,40%20L1440,80%20L0,80%20Z%27%20fill=%27%23FDFBF7%27/%3E%3C/svg%3E')] bg-cover bg-bottom"></div>
```

**Gradient fade:**
```astro
<div class="h-24 bg-gradient-to-b from-transparent to-surface-elevated pointer-events-none"></div>
```

**Ghost-text divider:**
```astro
<div class="text-center py-8">
  <span class="text-[6rem] font-bold text-black/[0.04] tracking-tighter leading-none">◆</span>
</div>
```


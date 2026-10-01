### A. The Stamped Block (Industrial Focal Points Only)

Reserved **exclusively** for Hero components, primary CTAs, and major focal blocks in the Industrial archetype. Zero radius. Hard 2px borders. Solid offset shadows. Use design tokens (`bg-accent`, `text-text-main`, `border-text-main`) so the block inherits the chosen accent automatically — do NOT hardcode `amber-400` / `zinc-900`.
```astro
<div class="bg-surface-elevated border-2 border-text-main shadow-[8px_8px_0px_0px_rgba(28,25,23,1)] p-8">
  <h3 class="text-2xl font-bold uppercase tracking-tight text-text-main">System Diagnostics</h3>
  <p class="mt-2 text-text-muted text-sm leading-relaxed">Raw payload data.</p>
</div>
```


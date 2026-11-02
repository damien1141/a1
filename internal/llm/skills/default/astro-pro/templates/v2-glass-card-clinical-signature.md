### V2. Glass Card (Clinical signature)

**Use when:** Clinical frosted surfaces. **Fits:** Clinical (LIGHT), Tech (DARK with white/5).
```astro
<div class="rounded-2xl bg-white/60 backdrop-blur-xl ring-1 ring-black/5 shadow-[0_1px_3px_rgba(15,23,42,0.08),0_12px_24px_rgba(15,23,42,0.04)] p-8">
  <h3 class="text-xl font-bold tracking-tight">Sterile surface</h3>
  <p class="mt-2 text-sm text-text-muted leading-relaxed">Frosted, transparent, precise.</p>
</div>
```
> `backdrop-blur` on a non-fixed element violates §23. Use Glass Cards ONLY when the card sits over a richly textured/mesh background (so the blur has something to blur). Otherwise use V3.


### L6. Marquee Strip (logo or text)

**Use when:** trust signals or category words need to move. **Fits:** all archetypes.
A full-width infinite horizontal scroll. Content is duplicated for a seamless loop. Uses CSS `@keyframes` with `linear` (the continuous-animation exception, §0).
```astro
<div class="overflow-hidden py-8 border-y border-black/5 bg-surface">
  <div class="flex gap-16 animate-[marquee_28s_linear_infinite] whitespace-nowrap">
    <span class="text-2xl font-bold tracking-tight text-zinc-400">TRUSTED</span>
    <span class="text-2xl font-bold tracking-tight text-zinc-400">CERTIFIED</span>
    <span class="text-2xl font-bold tracking-tight text-zinc-400">EST. 2014</span>
    <span class="text-2xl font-bold tracking-tight text-zinc-400">24/7</span>
    <!-- duplicate the set for seamless loop -->
    <span class="text-2xl font-bold tracking-tight text-zinc-400">TRUSTED</span>
    <span class="text-2xl font-bold tracking-tight text-zinc-400">CERTIFIED</span>
    <span class="text-2xl font-bold tracking-tight text-zinc-400">EST. 2014</span>
    <span class="text-2xl font-bold tracking-tight text-zinc-400">24/7</span>
  </div>
</div>
<style is:inline>
  @keyframes marquee { from { transform: translateX(0); } to { transform: translateX(-50%); } }
</style>
```

---

## 12. CONTENT COMPONENT LIBRARY


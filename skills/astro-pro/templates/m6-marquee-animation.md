### M6. Marquee Animation

**Use when:** continuous horizontal scroll (§11.L6). **Fits:** all archetypes.
```astro
<div class="flex gap-16 animate-[marquee_28s_linear_infinite] whitespace-nowrap">
<style is:inline>
  @keyframes marquee { from { transform: translateX(0); } to { transform: translateX(-50%); } }
</style>
```
> Duplicate the content set so the `-50%` translate is seamless. `linear` is correct here (continuous-animation exception).


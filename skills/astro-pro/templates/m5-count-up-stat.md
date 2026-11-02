### M5. Count-up Stat

**Use when:** a big number should animate from 0 on scroll. **Fits:** all archetypes.
Alpine `x-init` creates a scoped `IntersectionObserver` that fires once and disconnects. One observer per stat — fine for a handful.
```astro
<p class="text-6xl font-bold tabular-nums tracking-tight"
   x-data="{ count: 0, target: 987 }"
   x-init="const io = new IntersectionObserver(([e]) => {
     if (e.isIntersecting) {
       const start = performance.now();
       const tick = (now) => {
         const p = Math.min((now - start) / 1500, 1);
         count = Math.floor(p * target);
         if (p < 1) requestAnimationFrame(tick);
       };
       requestAnimationFrame(tick);
       io.disconnect();
     }
   }, { threshold: 0.4 });
   io.observe($el)"
   x-text="count.toLocaleString()"></p>
```


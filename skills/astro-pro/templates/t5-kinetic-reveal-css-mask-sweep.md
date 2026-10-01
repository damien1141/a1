### T5. Kinetic Reveal (CSS mask sweep)

**Use when:** a headline animates in on load with a wipe. **Fits:** Tech, Luxury.
```astro
<h1 class="text-7xl font-bold tracking-tight bg-[linear-gradient(90deg,#1C1917_50%,transparent_50%)] bg-[length:200%_100%] bg-[position:100%_0] text-transparent bg-clip-text animate-[wipe_1.2s_cubic-bezier(0.16,1,0.3,1)_forwards]">Kinetic.</h1>
<style is:inline>
  @keyframes wipe { to { background-position: 0 0; } }
</style>
```


### B3. Aurora (animated gradient)

**Use when:** a slow-moving ambient background for hero or CTA. **Fits:** Luxury, Tech.
Animates `background-position` (GPU-cheap). Uses `linear` (continuous-animation exception).
```astro
<div class="bg-[linear-gradient(120deg,#7C8471,#3D2C1E,#FDFBF7,#7C8471)] bg-[length:300%_300%] animate-[aurora_18s_linear_infinite]"></div>
<style is:inline>
  @keyframes aurora { 0% { background-position: 0% 50%; } 50% { background-position: 100% 50%; } 100% { background-position: 0% 50%; } }
</style>
```
> Perf note: one aurora per page max. `will-change: background-position` if needed.


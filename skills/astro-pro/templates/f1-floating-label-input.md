### F1. Floating Label Input

**Use when:** any text input. **Fits:** all archetypes (use `rounded-none` for Industrial).
The label floats up on focus or when filled. Uses `peer` + `peer-placeholder-shown:`.
```astro
<div class="relative">
  <input type="text" id="name" placeholder=" " class="peer w-full bg-surface-muted border-2 border-black/10 focus:border-accent rounded-xl px-4 pt-6 pb-2 text-text-main outline-none transition-colors" />
  <label for="name" class="absolute left-4 top-4 text-text-muted text-sm transition-all duration-200 ease-[cubic-bezier(0.4,0,0.2,1)] peer-focus:top-2 peer-focus:text-[10px] peer-focus:uppercase peer-focus:tracking-widest peer-focus:text-accent peer-[:not(:placeholder-shown)]:top-2 peer-[:not(:placeholder-shown)]:text-[10px] peer-[:not(:placeholder-shown)]:uppercase peer-[:not(:placeholder-shown)]:tracking-widest">Full name</label>
</div>
```


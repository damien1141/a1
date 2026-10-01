### C6. Carousel

**Use when:** testimonials or visuals need rotation. **Fits:** all archetypes.
Alpine `active` index with prev/next and dot indicators. Translates a flex track. Auto-advances via `setInterval` in `x-init` (cleared on hover with `@mouseenter`/`@mouseleave`).
```astro
<div class="relative max-w-3xl mx-auto" x-data="{ active: 0, count: 3, timer: null, next() { this.active = (this.active + 1) % this.count; }, prev() { this.active = (this.active - 1 + this.count) % this.count; } }" x-init="timer = setInterval(next, 5000)" @mouseenter="clearInterval(timer)" @mouseleave="timer = setInterval(next, 5000)">
  <div class="overflow-hidden rounded-2xl ring-1 ring-black/5">
    <div class="flex transition-transform duration-700 ease-[cubic-bezier(0.16,1,0.3,1)]" :style="'transform: translateX(-' + active * 100 + '%)'">
      <div class="w-full shrink-0 bg-surface-elevated p-8 md:p-12">
        <blockquote class="text-xl md:text-2xl font-medium tracking-tight leading-relaxed text-balance">"Slide one."</blockquote>
      </div>
      <div class="w-full shrink-0 bg-surface-elevated p-8 md:p-12">
        <blockquote class="text-xl md:text-2xl font-medium tracking-tight leading-relaxed text-balance">"Slide two."</blockquote>
      </div>
      <div class="w-full shrink-0 bg-surface-elevated p-8 md:p-12">
        <blockquote class="text-xl md:text-2xl font-medium tracking-tight leading-relaxed text-balance">"Slide three."</blockquote>
      </div>
    </div>
  </div>
  <button @click="prev()" class="absolute left-2 top-1/2 -translate-y-1/2 w-10 h-10 rounded-full bg-surface-elevated ring-1 ring-black/10 flex items-center justify-center" aria-label="Previous"><ChevronLeft class="w-5 h-5" /></button>
  <button @click="next()" class="absolute right-2 top-1/2 -translate-y-1/2 w-10 h-10 rounded-full bg-surface-elevated ring-1 ring-black/10 flex items-center justify-center" aria-label="Next"><ChevronRight class="w-5 h-5" /></button>
  <div class="flex justify-center gap-2 mt-6">
    <template x-for="i in count" :key="i">
      <button @click="active = i - 1" :class="active === i - 1 ? 'w-8 bg-accent' : 'w-2 bg-black/20'" class="h-2 rounded-full transition-all duration-300" :aria-label="'Go to slide ' + i"></button>
    </template>
  </div>
</div>
```


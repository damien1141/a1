### C7. Before/After Comparison

**Use when:** a transformation is the proof point. **Fits:** Clinical, Luxury, Industrial.
Alpine range input drives `clip-path` inset on the "after" layer. No images — use two Faux Thumbnail mesh panels with different palettes.
```astro
<div class="relative max-w-4xl mx-auto rounded-2xl overflow-hidden ring-1 ring-black/5 aspect-[16/9]" x-data="{ pos: 50 }">
  <div class="absolute inset-0 bg-[radial-gradient(circle_at_30%_40%,#0F172A,#0D9488)]"></div>
  <div class="absolute inset-0 bg-[radial-gradient(circle_at_70%_60%,#FDFBF7,#7C8471)]" :style="'clip-path: inset(0 0 0 ' + pos + '%)'"></div>
  <input type="range" min="0" max="100" x-model="pos" class="absolute top-1/2 left-0 right-0 w-full -translate-y-1/2 opacity-0 cursor-ew-resize" aria-label="Compare">
  <div class="absolute top-1/2 -translate-y-1/2 pointer-events-none" :style="'left: ' + pos + '%'">
    <div class="w-1 h-16 bg-white shadow-[0_0_20px_rgba(0,0,0,0.3)] -translate-x-1/2"></div>
    <div class="absolute top-1/2 left-1/2 -translate-x-1/2 -translate-y-1/2 w-8 h-8 rounded-full bg-white flex items-center justify-center shadow-lg">
      <MoveHorizontal class="w-4 h-4 text-text-main" />
    </div>
  </div>
  <span class="absolute bottom-4 left-4 text-xs font-bold uppercase tracking-widest text-white/90">Before</span>
  <span class="absolute bottom-4 right-4 text-xs font-bold uppercase tracking-widest text-text-main/90">After</span>
</div>
```


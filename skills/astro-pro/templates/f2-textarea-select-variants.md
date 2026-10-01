### F2. Textarea & Select variants

**Use when:** long-form input or choice input. **Fits:** all archetypes.
Textarea uses a static top label (floating doesn't suit multi-line). Select uses a chevron overlay on a native `<select>` (styled).
```astro
<!-- Textarea -->
<div>
  <label for="msg" class="block text-[10px] uppercase tracking-widest text-text-muted mb-2">Message</label>
  <textarea id="msg" rows="4" class="w-full bg-surface-muted border-2 border-black/10 focus:border-accent rounded-xl px-4 py-3 text-text-main outline-none transition-colors resize-none"></textarea>
</div>

<!-- Select -->
<div class="relative">
  <select class="w-full appearance-none bg-surface-muted border-2 border-black/10 focus:border-accent rounded-xl px-4 py-3 pr-12 text-text-main outline-none transition-colors">
    <option>Boiler repair</option>
    <option>Pipe relining</option>
  </select>
  <ChevronDown class="w-5 h-5 absolute right-4 top-1/2 -translate-y-1/2 pointer-events-none text-text-muted" />
</div>
```


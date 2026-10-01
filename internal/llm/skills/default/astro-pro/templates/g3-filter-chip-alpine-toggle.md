### G3. Filter Chip (Alpine toggle)

**Use when:** a filterable list. **Fits:** Clinical, Tech.
```astro
<button x-data="{ on: false }" @click="on = !on" :class="on ? 'bg-accent text-white ring-accent' : 'bg-surface-muted text-text-muted ring-black/5'" class="inline-flex items-center gap-1.5 rounded-full ring-1 px-3 py-1.5 text-xs font-semibold transition-colors">
  <Check x-show="on" x-cloak class="w-3 h-3" /> Residential
</button>
```


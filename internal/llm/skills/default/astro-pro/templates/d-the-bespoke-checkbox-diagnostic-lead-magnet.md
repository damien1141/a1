### D. The Bespoke Checkbox (Diagnostic Lead Magnet)

NEVER use native `<input type="checkbox">` styling. Use Tailwind's `peer-checked` pattern. The input is visually hidden (`sr-only peer`), and the visual box is a sibling `<div>` that reacts to `peer-checked:`. The check icon MUST be a sibling of the visual box (not nested inside it) so `peer-checked:opacity-100` works.
- **Check icon color (THEME-CONDITIONAL):** `text-zinc-900` (or `text-text-main`) on LIGHT-theme archetypes (Industrial/Luxury/Clinical). On the DARK Precision Tech archetype, use `text-white` — a dark glyph on a dark-accent box is invisible. The box border is `border-zinc-900` on light, `border-white/10` on dark.
- **State coupling:** the checkbox MUST keep `@change="auditScore += $event.target.checked ? 1 : -1"` so it writes to the root `auditScore` state.
```astro
<!-- LIGHT THEME (Industrial / Luxury / Clinical) -->
<label class="flex items-start gap-3 cursor-pointer group">
  <div class="relative flex items-center justify-center mt-0.5">
    <input type="checkbox" class="sr-only peer" @change="auditScore += $event.target.checked ? 1 : -1">
    <div class="w-8 h-8 bg-surface-muted border-2 border-zinc-900 group-hover:border-zinc-900 peer-checked:bg-accent peer-checked:border-accent transition-colors flex items-center justify-center shadow-[3px_3px_0px_0px_rgba(28,25,23,1)]">
      <Check class="w-5 h-5 text-zinc-900 absolute opacity-0 peer-checked:opacity-100 transition-opacity pointer-events-none" />
    </div>
  </div>
  <span class="text-sm text-text-main leading-tight">Has your boiler failed during extreme cold?</span>
</label>

<!-- DARK THEME (Precision Tech only) -->
<label class="flex items-start gap-3 cursor-pointer group">
  <div class="relative flex items-center justify-center mt-0.5">
    <input type="checkbox" class="sr-only peer" @change="auditScore += $event.target.checked ? 1 : -1">
    <div class="w-8 h-8 bg-surface-muted border-2 border-white/10 group-hover:border-white/30 peer-checked:bg-accent peer-checked:border-accent transition-colors flex items-center justify-center shadow-[3px_3px_0px_0px_rgba(0,0,0,1)]">
      <Check class="w-5 h-5 text-white absolute opacity-0 peer-checked:opacity-100 transition-opacity pointer-events-none" />
    </div>
  </div>
  <span class="text-sm text-text-main leading-tight">Has your panel failed during a charge?</span>
</label>
```


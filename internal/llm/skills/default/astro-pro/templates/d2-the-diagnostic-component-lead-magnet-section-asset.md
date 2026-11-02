### D2. The Diagnostic Component (Lead-Magnet Section Asset)

A full selectable section asset, not a build mandate. The `architecture-designer` decides whether to use this layout. It is placed immediately after the Hero as the lead magnet. It binds to the root `auditScore` state via Alpine scope inheritance — the root wrapper declares `x-data="{ auditScore: 0 }"` and every checkbox mutates it.

**Structure:** full-width centered header → massive progress bar → 2-column grid of 5 bespoke checkboxes (the 5th may span full width with `md:col-span-2`).
```astro
<div class="max-w-4xl mx-auto text-center">
  <h2><!-- Diagnostic headline --></h2>
  <p><!-- one-line subcopy --></p>
</div>

<div class="mt-12 max-w-3xl mx-auto bg-surface-elevated p-8">
  <div class="flex items-center justify-between mb-2">
    <span class="text-sm font-medium text-text-muted">Risk Score</span>
    <span class="text-sm font-heading font-bold text-text-main" x-text="auditScore + '/5'"></span>
  </div>
  <div class="w-full max-w-full h-3 bg-surface-muted rounded-none overflow-hidden mb-8">
    <div
      class="h-full transition-all duration-500 ease-[cubic-bezier(0.7,0,0.3,1)]"
      :style="'width: ' + (auditScore / 5) * 100 + '%'"
      :class="(auditScore <= 2) ? 'bg-gradient-to-r from-green-500 to-yellow-400' : (auditScore <= 4) ? 'bg-gradient-to-r from-yellow-400 to-orange-500' : 'bg-gradient-to-r from-orange-500 to-red-500'"
    ></div>
  </div>

  <div class="grid grid-cols-1 md:grid-cols-2 gap-6">
    <label class="flex items-start gap-3 cursor-pointer group">
      <div class="relative flex items-center justify-center mt-0.5">
        <input type="checkbox" class="sr-only peer" @change="auditScore += $event.target.checked ? 1 : -1">
        <div class="w-8 h-8 bg-surface-muted border-2 border-zinc-900 group-hover:border-zinc-900 peer-checked:bg-accent peer-checked:border-accent transition-colors flex items-center justify-center shadow-[3px_3px_0px_0px_rgba(28,25,23,1)]"></div>
        <Check class="w-5 h-5 text-zinc-900 absolute opacity-0 peer-checked:opacity-100 transition-opacity pointer-events-none" />
      </div>
      <span class="text-sm text-text-main leading-tight"><!-- diagnostic question --></span>
    </label>
    <!-- repeat 4 more, swap icon color to text-white + border-white/10 on dark themes -->
  </div>
</div>
```
- **State coupling (required if used):** every checkbox keeps `@change="auditScore += $event.target.checked ? 1 : -1"`. The CTA form reads it back via `<input type="hidden" name="audit_score" :value="auditScore">`.
- **Icon color (theme-conditional):** `text-zinc-900` on light archetypes; `text-white` on the dark Precision Tech variant.
- **Traffic-light gradient stops** use default Tailwind palette utilities, not design tokens — gradient stop colors are exempt from the "tokens only" rule.


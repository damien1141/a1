### M4. Stagger Reveal

**Use when:** a grid of items reveals in sequence. **Fits:** all archetypes.
Uses CSS custom property `--delay` on each child + the global `.reveal` mechanism extended with `transition-delay`.
```astro
<div class="reveal grid grid-cols-1 md:grid-cols-3 gap-6">
  <div class="reveal" style="transition-delay: 0ms;">A</div>
  <div class="reveal" style="transition-delay: 100ms;">B</div>
  <div class="reveal" style="transition-delay: 200ms;">C</div>
</div>
```
> The global `.reveal` CSS already animates opacity + transform; adding inline `transition-delay` staggers them. Verify `transition: opacity 0.6s, transform 0.6s` includes delay in the global shell.


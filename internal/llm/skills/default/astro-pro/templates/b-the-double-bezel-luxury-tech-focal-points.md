### B. The Double-Bezel (Luxury / Tech Focal Points)

Reserved **exclusively** for Hero components, primary CTAs, and major focal blocks in Luxury/Tech archetypes.
- **Outer Shell:** Wrapper with subtle bg, hairline ring, small padding, large radius.
- **Inner Core:** Content container with its own bg, inset highlight, and mathematically smaller radius.
```astro
<div class="p-1.5 rounded-[2rem] bg-black/5 ring-1 ring-black/5">
  <div class="rounded-[calc(2rem-0.375rem)] bg-white p-8 shadow-[inset_0_1px_1px_rgba(255,255,255,0.8)]">
    <!-- content -->
  </div>
</div>
```


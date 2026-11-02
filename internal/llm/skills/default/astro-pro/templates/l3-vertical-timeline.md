### L3. Vertical Timeline

**Use when:** a process with 4–6 steps matters to the buyer. **Fits:** Industrial, Clinical, Tech.
A single column with a vertical hairline rail. Each step has a ghost number, a node dot, a title, and copy. The rail is a `border-l` on the container; nodes are positioned absolutely.
```astro
<div class="relative max-w-3xl mx-auto pl-8 md:pl-16">
  <div class="absolute left-2 md:left-6 top-2 bottom-2 w-px bg-black/10"></div>
  <div class="relative py-10">
    <span class="absolute -left-8 md:-left-16 top-0 text-[8rem] font-bold text-black/[0.04] tracking-tighter leading-none">01</span>
    <span class="absolute -left-[5px] md:-left-[13px] top-12 w-3 h-3 rounded-full bg-accent ring-4 ring-surface"></span>
    <h3 class="text-2xl font-bold tracking-tight">Discovery</h3>
    <p class="mt-2 text-text-muted leading-relaxed max-w-prose">We map the existing terrain before proposing a single change.</p>
  </div>
  <!-- repeat per step -->
</div>
```


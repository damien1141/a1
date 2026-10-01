### The Section Shell (Isolation & Glows)

Because the global grid AND the global radial glow are fixed in `Layout.astro`, individual `<section>` tags MUST NOT have an opaque background and MUST NOT contain their own glow/grid div. Sections use `relative` and `overflow-hidden` only to scope stacking context; the glow lives globally. Content MUST be `z-10`.

```astro
{/* ⚠️ DO NOT COPY THE INLINE GLOW DIV BELOW — it is shown ONLY to illustrate what is forbidden.
    The glow is applied once, globally, in Layout.astro (`.section-glow`). */}
<section id="..." class="reveal relative overflow-hidden py-24 md:py-32">
  {/* FORBIDDEN in this pipeline — glow is global:
  <div class="absolute inset-0 z-0 bg-[radial-gradient(ellipse_80%_50%_at_50%_0%,rgb(var(--accent)/0.15),transparent_70%)] pointer-events-none"></div> */}

  <!-- Content (z-10) -->
  <div class="relative z-10 max-w-6xl mx-auto px-6 md:px-8 lg:px-12">
    <!-- content -->
  </div>
</section>
```


### Mandate 1: The Hero (Asymmetric 7/5 Split)

Best for establishing hierarchy and focal points. **Hero spacing is the ONE exception to the uniform `py-24 md:py-32` rule** (see §6): use `pt-16 md:pt-24 pb-24 md:pb-32` and `md:min-h-[78dvh]` on the inner grid so the hero starts HIGH under the sticky header (no oversized top gap) while still filling the desktop viewport.
```astro
<section id="hero" class="reveal relative overflow-hidden pt-16 md:pt-24 pb-24 md:pb-32">
  <div class="relative z-10 max-w-6xl mx-auto px-6 md:px-8 lg:px-12">
    <div class="grid grid-cols-1 md:grid-cols-12 gap-8 items-center md:min-h-[78dvh]">
      <div class="md:col-span-7"><!-- Headline & Copy --></div>
      <div class="md:col-span-5 md:mt-24"><!-- Focal Data Card --></div>
    </div>
  </div>
</section>
```
> For alternative hero structures, see §9 (Hero Library). Do not use two hero variants on one page.


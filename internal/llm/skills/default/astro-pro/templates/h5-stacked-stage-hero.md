### H5. Stacked Stage Hero

**Use when:** the copy is short and declarative; you want a theatrical, gallery-stage feel. **Fits:** Luxury, Clinical.
Oversized centered-aligned-to-left type with a floating "CTA island" card detached below. Avoids the banned "centered hero slop" by using radical left-alignment and an asymmetric floating card.
```astro
<section id="hero" class="reveal relative overflow-hidden pt-16 md:pt-24 pb-24 md:pb-32">
  <div class="relative z-10 max-w-6xl mx-auto px-6 md:px-8 lg:px-12">
    <div class="md:pl-12">
      <h1 class="text-6xl md:text-8xl lg:text-9xl font-bold tracking-tight leading-[0.92] text-balance">Precision<br/>in form.</h1>
    </div>
    <div class="mt-10 md:mt-16 grid grid-cols-1 md:grid-cols-12 gap-6">
      <div class="md:col-span-5 md:col-start-2 bg-surface-elevated rounded-[2rem] p-6 ring-1 ring-black/5 shadow-[0_20px_40px_rgba(0,0,0,0.06)]">
        <p class="text-sm text-text-muted leading-relaxed">A studio practice for patients who refuse compromise.</p>
        <div class="mt-6"><!-- Nested CTA pill --></div>
      </div>
      <div class="md:col-span-3 md:col-start-9 self-end">
        <p class="text-[10px] uppercase tracking-[0.2em] text-zinc-500">Next availability</p>
        <p class="text-2xl font-bold tracking-tight">Thu, 14 Nov</p>
      </div>
    </div>
  </div>
</section>
```

---

## 10. NAVIGATION RECIPE LIBRARY


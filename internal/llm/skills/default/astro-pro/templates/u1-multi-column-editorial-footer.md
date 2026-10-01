### U1. Multi-Column Editorial Footer

**Use when:** a rich footer with nav columns + brand. **Fits:** Luxury, Clinical.
```astro
<footer class="border-t border-black/5 bg-surface py-16">
  <div class="max-w-6xl mx-auto px-6 md:px-8 lg:px-12 grid grid-cols-2 md:grid-cols-12 gap-8">
    <div class="col-span-2 md:col-span-4">
      <p class="font-heading font-bold text-lg tracking-tight">Brand</p>
      <p class="mt-3 text-sm text-text-muted leading-relaxed max-w-xs">Concierge care, reimagined.</p>
    </div>
    <div class="md:col-span-2">
      <p class="text-[10px] uppercase tracking-[0.2em] text-text-muted mb-3">Services</p>
      <ul class="space-y-2 text-sm">
        <li><a href="#" class="hover:text-accent">Boiler</a></li>
        <li><a href="#" class="hover:text-accent">Pipes</a></li>
      </ul>
    </div>
    <div class="md:col-span-2">
      <p class="text-[10px] uppercase tracking-[0.2em] text-text-muted mb-3">Company</p>
      <ul class="space-y-2 text-sm">
        <li><a href="#" class="hover:text-accent">About</a></li>
        <li><a href="#" class="hover:text-accent">Reviews</a></li>
      </ul>
    </div>
    <div class="md:col-span-4">
      <p class="text-[10px] uppercase tracking-[0.2em] text-text-muted mb-3">NAP</p>
      <p class="text-sm text-text-muted leading-relaxed">123 Main St, Metro 00000<br/>Mon–Sun · 24/7</p>
    </div>
  </div>
  <div class="max-w-6xl mx-auto px-6 md:px-8 lg:px-12 mt-12 pt-6 border-t border-black/5 flex justify-between text-xs text-text-muted">
    <span>© 2025 Brand</span>
    <span>Lic. #12345</span>
  </div>
</footer>
```


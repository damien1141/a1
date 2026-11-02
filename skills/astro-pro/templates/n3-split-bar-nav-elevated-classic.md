### N3. Split Bar Nav (elevated classic)

**Use when:** the brand needs a stronger, edge-to-edge presence (e.g., board-certified Clinical or established Industrial). **Fits:** Clinical, Industrial.
A non-floating sticky bar: logo left, links right, hairline bottom border, no backdrop-blur (it's opaque). Pairs with §10.N4 for mobile.
```astro
<header class="sticky top-0 z-50 bg-surface/95 backdrop-blur-xl border-b border-black/5">
  <div class="max-w-6xl mx-auto px-6 md:px-8 lg:px-12 flex items-center justify-between h-16">
    <a href="#" class="font-heading font-bold tracking-tight text-lg">Brand</a>
    <nav class="hidden md:flex items-center gap-8">
      <a href="#services" class="text-sm font-medium text-text-main hover:text-accent transition-colors">Services</a>
      <a href="#reviews" class="text-sm font-medium text-text-main hover:text-accent transition-colors">Reviews</a>
      <a href="#faq" class="text-sm font-medium text-text-main hover:text-accent transition-colors">FAQ</a>
    </nav>
    <a href="#cta" class="hidden md:inline-flex"><!-- Canonical CTA (§8.E) --></a>
    <button class="md:hidden" x-data="{ open: false }" @click="open = !open" :aria-expanded="open"><!-- Mobile toggle (§10.N4) --></button>
  </div>
</header>
```


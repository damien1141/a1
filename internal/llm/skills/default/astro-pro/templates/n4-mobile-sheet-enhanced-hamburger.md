### N4. Mobile Sheet (enhanced hamburger)

Full-screen Alpine sheet with staggered link reveals. Use with any nav variant. The hamburger lines morph into an X (see §20.C for the morph).
```astro
<header x-data="{ open: false }">
  <button @click="open = !open" :aria-expanded="open" class="relative h-10 w-10 md:hidden">
    <span class="absolute left-1/2 top-1/2 h-0.5 w-5 -translate-x-1/2 -translate-y-1/2 bg-current transition-all duration-300 ease-[cubic-bezier(0.16,1,0.3,1)]" :class="open ? 'rotate-45' : '-translate-y-[6px]'"></span>
    <span class="absolute left-1/2 top-1/2 h-0.5 w-5 -translate-x-1/2 -translate-y-1/2 bg-current transition-all duration-300 ease-[cubic-bezier(0.16,1,0.3,1)]" :class="open ? 'opacity-0' : 'opacity-100'"></span>
    <span class="absolute left-1/2 top-1/2 h-0.5 w-5 -translate-x-1/2 -translate-y-1/2 bg-current transition-all duration-300 ease-[cubic-bezier(0.16,1,0.3,1)]" :class="open ? '-rotate-45' : 'translate-y-[6px]'"></span>
  </button>
  <div x-show="open" x-transition.opacity.duration.300ms @keydown.escape.window="open = false" class="fixed inset-0 z-40 bg-surface/95 backdrop-blur-3xl flex items-center justify-center md:hidden" x-cloak>
    <nav class="flex flex-col items-center gap-8">
      <a href="#services" @click="open = false" x-show="open" x-transition:enter="transition ease-[cubic-bezier(0.16,1,0.3,1)] duration-500" x-transition:enter-start="opacity-0 translate-y-6" x-transition:enter-end="opacity-100 translate-y-0" x-transition:enter-delay.80ms class="text-3xl font-medium">Services</a>
      <a href="#reviews" @click="open = false" x-show="open" x-transition:enter="transition ease-[cubic-bezier(0.16,1,0.3,1)] duration-500" x-transition:enter-start="opacity-0 translate-y-6" x-transition:enter-end="opacity-100 translate-y-0" x-transition:enter-delay.160ms class="text-3xl font-medium">Reviews</a>
      <a href="#faq" @click="open = false" x-show="open" x-transition:enter="transition ease-[cubic-bezier(0.16,1,0.3,1)] duration-500" x-transition:enter-start="opacity-0 translate-y-6" x-transition:enter-end="opacity-100 translate-y-0" x-transition:enter-delay.240ms class="text-3xl font-medium">FAQ</a>
      <a href="#cta" @click="open = false" x-show="open" x-transition:enter="transition ease-[cubic-bezier(0.16,1,0.3,1)] duration-500" x-transition:enter-start="opacity-0 translate-y-6" x-transition:enter-end="opacity-100 translate-y-0" x-transition:enter-delay.320ms class="text-3xl font-medium text-accent">Book</a>
    </nav>
  </div>
</header>
```

---

## 11. SECTION LAYOUT RECIPE LIBRARY


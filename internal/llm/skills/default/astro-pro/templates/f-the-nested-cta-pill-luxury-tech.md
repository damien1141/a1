### F. The Nested CTA (Pill / Luxury / Tech)

Primary buttons are fully rounded pills. If a button has an arrow, it NEVER sits naked — it must be nested in its own circular wrapper that translates diagonally on hover. The arrow wrapper MUST carry `relative z-10`.
```astro
<a href="#book" class="group flex items-center gap-3 rounded-full bg-zinc-900 px-6 py-3 text-white transition-all duration-300 ease-[cubic-bezier(0.16,1,0.3,1)] hover:bg-zinc-800 active:scale-[0.98]">
  <span>Book Diagnostic</span>
  <span class="relative z-10 flex h-8 w-8 items-center justify-center rounded-full bg-white/10 transition-transform duration-300 ease-[cubic-bezier(0.16,1,0.3,1)] group-hover:translate-x-1 group-hover:-translate-y-[1px] group-hover:scale-105">
    <ArrowRight class="w-4 h-4" />
  </span>
</a>
```


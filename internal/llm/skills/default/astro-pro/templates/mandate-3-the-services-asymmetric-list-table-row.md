### Mandate 3: The Services (Asymmetric List / Table Row)

Do not use cards. Use a wide, table-like layout with varying row heights and hover expansions. **CRITICAL:** Do NOT put the haptic shadow on the individual `<a>` rows. Put the `shadow-[8px_8px_0px_0px_rgba(28,25,23,1)]` on the outer wrapper div that contains the `divide-y` list.
```astro
<div class="shadow-[8px_8px_0px_0px_rgba(28,25,23,1)] border-2 border-white/5">
  <div class="divide-y divide-white/5">
    <a href="#" class="group grid grid-cols-12 gap-4 items-center py-8 transition-all duration-300 hover:bg-black/[0.02] px-4">
      <span class="col-span-1 text-xs font-mono text-zinc-400">01</span>
      <h3 class="col-span-7 text-2xl font-bold tracking-tight">Pipe Burst Lining</h3>
      <p class="col-span-3 text-sm text-zinc-500 opacity-0 group-hover:opacity-100 transition-opacity">Replace broken lines from inside.</p>
      <span class="col-span-1 text-right transition-transform group-hover:translate-x-2">
        <ArrowRight class="w-5 h-5 inline-block" />
      </span>
    </a>
  </div>
</div>
```


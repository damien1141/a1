### C13. Terminal Window (Tech signature)

**Use when:** Tech archetype needs a code/output frame. **Fits:** Precision Tech only (DARK).
A window chrome (three dots) + a monospace body. Pair with Typewriter (§19.M7) for animated output.
```astro
<div class="rounded-xl ring-1 ring-white/10 bg-[#020203] shadow-[0_0_40px_-10px_rgba(59,130,246,0.5)] overflow-hidden">
  <div class="flex items-center gap-2 px-4 py-3 border-b border-white/10">
    <span class="w-3 h-3 rounded-full bg-red-500/80"></span>
    <span class="w-3 h-3 rounded-full bg-yellow-500/80"></span>
    <span class="w-3 h-3 rounded-full bg-green-500/80"></span>
    <span class="ml-3 text-xs font-mono text-zinc-500">~/project — zsh</span>
  </div>
  <pre class="p-6 font-mono text-sm leading-relaxed text-zinc-300 overflow-x-auto"><code><span class="text-green-400">$</span> bun run dev
<span class="text-zinc-500">→ ready in 0.4s</span>
<span class="text-blue-400">→ localhost:4321</span></code></pre>
</div>
```


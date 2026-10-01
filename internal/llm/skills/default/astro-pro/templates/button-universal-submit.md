### Button · Universal Submit

**Use when:** form submit with loading + success state. Alpine-driven. Wire `@click` to your real handler in production.
**Imports:** `import { Loader2, Check } from 'lucide-astro';`
```astro
<button type="submit" x-data="{ loading: false, done: false }" @click.prevent="loading = true; setTimeout(() => { loading = false; done = true; }, 1500)" :disabled="loading" class="group inline-flex w-full sm:inline-flex items-center justify-center gap-2 rounded-xl bg-accent text-white px-6 py-3.5 font-heading font-semibold tracking-tight text-sm transition-all duration-300 ease-[cubic-bezier(0.16,1,0.3,1)] hover:brightness-110 active:scale-[0.98] disabled:opacity-70 disabled:cursor-not-allowed">
  <Loader2 x-show="loading" x-cloak class="w-4 h-4 animate-spin" />
  <Check x-show="done" x-cloak class="w-4 h-4" />
  <span x-show="!loading && !done">Submit Request</span>
  <span x-show="loading" x-cloak>Sending…</span>
  <span x-show="done" x-cloak>Sent</span>
</button>
```

### C5. Toast Notification

**Use when:** confirming a form submission or action without a redirect. **Fits:** all archetypes.
Alpine root with a timeout. Toast is fixed bottom-right, dismissible.
```astro
<div x-data="{ show: false, message: '' }" @toast.window="message = $event.detail; show = true; setTimeout(() => show = false, 4000)" class="fixed bottom-6 right-6 z-50">
  <div x-show="show" x-transition:enter="transition ease-[cubic-bezier(0.16,1,0.3,1)] duration-400" x-transition:enter-start="opacity-0 translate-y-4" x-transition:enter-end="opacity-100 translate-y-0" x-transition:leave="transition ease-[cubic-bezier(0.16,1,0.3,1)] duration-300" x-transition:leave-start="opacity-100" x-transition:leave-end="opacity-0 translate-y-4" class="flex items-center gap-3 bg-surface-elevated rounded-2xl ring-1 ring-black/10 shadow-[0_12px_40px_rgba(0,0,0,0.12)] px-5 py-4" x-cloak>
    <CheckCircle class="w-5 h-5 text-green-500 shrink-0" />
    <span class="text-sm font-medium" x-text="message"></span>
    <button @click="show = false" class="text-text-muted hover:text-text-main"><X class="w-4 h-4" /></button>
  </div>
</div>
```
> Trigger from anywhere with `window.dispatchEvent(new CustomEvent('toast', { detail: 'Saved' }))`.


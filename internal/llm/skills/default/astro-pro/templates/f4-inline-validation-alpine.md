### F4. Inline Validation (Alpine)

**Use when:** forms need immediate feedback. **Fits:** all archetypes.
Bind input to state, validate on `@blur`, show a message with `x-show` and a check/error icon.
```astro
<div x-data="{ email: '', valid: null, check() { this.valid = /^[^@]+@[^@]+\.[^@]+$/.test(this.email); } }">
  <div class="relative">
    <input type="email" x-model="email" @blur="check()" placeholder="you@example.com" class="w-full bg-surface-muted border-2 rounded-xl px-4 py-3 pr-12 outline-none transition-colors" :class="valid === false ? 'border-red-500' : valid === true ? 'border-green-500' : 'border-black/10 focus:border-accent'" />
    <span x-show="valid === true" x-cloak class="absolute right-4 top-1/2 -translate-y-1/2 text-green-500"><Check class="w-5 h-5" /></span>
    <span x-show="valid === false" x-cloak class="absolute right-4 top-1/2 -translate-y-1/2 text-red-500"><AlertCircle class="w-5 h-5" /></span>
  </div>
  <p x-show="valid === false" x-cloak class="mt-1.5 text-xs text-red-500">Enter a valid email.</p>
</div>
```


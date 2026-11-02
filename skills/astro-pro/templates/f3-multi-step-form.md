### F3. Multi-step Form

**Use when:** a lead form has 3+ logical stages. **Fits:** all archetypes.
Alpine `step` state with a Stepper (§11.L4) header. `x-show` per step. Validate before advancing.
```astro
<form x-data="{ step: 1, max: 3 }">
  <div class="grid grid-cols-3 gap-4 mb-10">
    <template x-for="n in max" :key="n">
      <div class="flex items-center gap-2">
        <span class="flex items-center justify-center w-8 h-8 rounded-full text-xs font-bold tabular-nums transition-colors" :class="step >= n ? 'bg-accent text-white' : 'bg-surface-muted text-text-muted'" x-text="n"></span>
        <div class="hidden md:block flex-1 h-px" :class="step > n ? 'bg-accent' : 'bg-black/10'"></div>
      </div>
    </template>
  </div>
  <div x-show="step === 1"><!-- F1 inputs --></div>
  <div x-show="step === 2" x-cloak><!-- F1 inputs --></div>
  <div x-show="step === 3" x-cloak><!-- F1 inputs --></div>
  <div class="mt-8 flex justify-between">
    <button type="button" @click="step = Math.max(1, step - 1)" x-show="step > 1" x-cloak class="text-text-muted hover:text-text-main">Back</button>
    <button type="button" @click="step = Math.min(max, step + 1)" x-show="step < max" class="bg-accent text-white px-6 py-3 rounded-xl font-semibold">Continue</button>
    <button type="submit" x-show="step === max" x-cloak class="bg-accent text-white px-6 py-3 rounded-xl font-semibold">Submit</button>
  </div>
</form>
```


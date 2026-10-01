### C8. Feature Matrix Table

**Use when:** comparing 3 plans or 3 tiers across 5–8 features. **Fits:** Tech, Clinical.
A semantic `<table>` with hairline rules, check/dash icons, and one elevated column.
```astro
<div class="overflow-x-auto">
  <table class="w-full border-collapse">
    <thead>
      <tr class="border-b-2 border-black/10">
        <th class="text-left py-4 px-4 text-sm font-semibold text-text-muted">Feature</th>
        <th class="text-center py-4 px-4 text-sm font-bold text-text-main">Starter</th>
        <th class="text-center py-4 px-4 text-sm font-bold text-white bg-accent rounded-t-xl">Pro</th>
        <th class="text-center py-4 px-4 text-sm font-bold text-text-main">Enterprise</th>
      </tr>
    </thead>
    <tbody class="divide-y divide-black/5">
      <tr>
        <td class="py-4 px-4 text-sm">Engineers</td>
        <td class="text-center py-4 px-4"><Check class="w-4 h-4 text-green-500 inline-block" /></td>
        <td class="text-center py-4 px-4 bg-accent/5"><Check class="w-4 h-4 text-green-500 inline-block" /></td>
        <td class="text-center py-4 px-4"><Check class="w-4 h-4 text-green-500 inline-block" /></td>
      </tr>
      <tr>
        <td class="py-4 px-4 text-sm">SLA</td>
        <td class="text-center py-4 px-4"><Minus class="w-4 h-4 text-zinc-300 inline-block" /></td>
        <td class="text-center py-4 px-4 bg-accent/5"><Check class="w-4 h-4 text-green-500 inline-block" /></td>
        <td class="text-center py-4 px-4"><Check class="w-4 h-4 text-green-500 inline-block" /></td>
      </tr>
    </tbody>
  </table>
</div>
```


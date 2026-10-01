### M7. Typewriter

**Use when:** Tech hero terminal output (§9.H3). **Fits:** Tech.
```astro
<div x-data="{ lines: ['$ bun init', '→ ready', '✓ live'], shown: '', i: 0, j: 0 }"
     x-init="const tick = () => {
       if (i < lines.length) {
         if (j <= lines[i].length) {
           shown = lines.slice(0,i).join('\n') + '\n' + lines[i].slice(0,j);
           j++;
           setTimeout(tick, 28);
         } else { i++; j=0; setTimeout(tick, 400); }
       }
     }; tick()"
     x-text="shown"></div>
```


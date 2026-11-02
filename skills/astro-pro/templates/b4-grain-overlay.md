### B4. Grain Overlay

**Use when:** a filmic noise texture over flat colors. **Fits:** Luxury, Clinical.
MUST be `fixed` and `pointer-events-none` (§23). Use the JIT-safe noise SVG.
```astro
<div class="fixed inset-0 z-50 pointer-events-none opacity-[0.03] mix-blend-overlay bg-[url('data:image/svg+xml,%3Csvg%20xmlns=%27http://www.w3.org/2000/svg%27%20width=%27200%27%20height=%27200%27%3E%3Cfilter%20id=%27n%27%3E%3CfeTurbulence%20type=%27fractalNoise%27%20baseFrequency=%270.9%27%20numOctaves=%273%27/%3E%3C/filter%3E%3Crect%20width=%27200%27%20height=%27200%27%20filter=%27url(%23n)%27/%3E%3C/svg%3E')]"></div>
```


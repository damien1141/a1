### M8. Magnetic Nudge (CSS-only, no mousemove)

**Use when:** a subtle directional nudge on hover. **Fits:** Luxury, Tech.
A pure-CSS translate on hover — no JS, no mousemove. The element shifts toward the cursor conceptually using `:hover` + `active`.
```astro
<a href="#" class="group inline-flex items-center gap-3 transition-transform duration-300 ease-[cubic-bezier(0.16,1,0.3,1)] hover:-translate-y-0.5 active:translate-y-0">
  <span>Book</span>
  <span class="transition-transform duration-300 ease-[cubic-bezier(0.16,1,0.3,1)] group-hover:translate-x-0.5"><ArrowRight class="w-4 h-4" /></span>
</a>
```

---

## 20. MOTION CHOREOGRAPHY (Core)

All motion simulates real-world mass. The standard curve is `ease-[cubic-bezier(0.16,1,0.3,1)]` (Clinical Trust uses `ease-[cubic-bezier(0.4,0,0.2,1)]`, Industrial uses `ease-[cubic-bezier(0.7,0,0.3,1)]`, Tech uses `ease-[cubic-bezier(0.32,0.72,0,1)]`).

**Stack Lock:** Use Alpine.js exclusively for all motion. Never inject vanilla `<script>` tags for UI logic or scroll listeners.


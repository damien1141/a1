### Oversized Background Typography (Ghost Text)

Massive, faint text behind section content. Push opacity to **5–8%** so it registers as texture, not decoration.
```astro
<h2 class="absolute -top-10 left-0 text-[12rem] font-bold text-black/[0.06] tracking-tighter pointer-events-none select-none -z-10 whitespace-nowrap">SERVICES</h2>
```

---

## 6. SECTION SPACING (one rhythm, no "weird" gaps)

- **Shared rhythm:** Every INTERIOR section (Diagnostic, Services, Reviews, FAQ, CTA) uses the SAME vertical padding `py-24 md:py-32` so the page reads as one consistent vertical beat. Do NOT hardcode a different `py-*` per section.
- **Hero exception:** The Hero MUST start HIGH under the sticky header, not pushed down by a large top gap. Use `pt-16 md:pt-24 pb-24 md:pb-32` and `md:min-h-[78dvh]` on the inner grid (see §7.Mandate 1). Do NOT put `py-24 md:py-32` on the hero, and do NOT use `min-h-[100dvh]` on the hero grid — that combination produced an oversized top gap and a low-centered hero in v1.
- The generic Section Shell example in §5 shows `py-24 md:py-32`; that applies to interior sections only.

---

## 7. SECTION-SPECIFIC LAYOUT MANDATES (Killing the "Samey" Slop)

The generic "3 identical cards horizontally" feature row is **STRICTLY BANNED**. Furthermore, you cannot use the same 7/5 asymmetric grid for every section. You MUST vary the structural rhythm of the page.


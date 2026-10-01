### F5. Form Shell (CTA section form)

**Use when:** the CTA section's left column. **Fits:** all archetypes.
A Double-Bezel or Stamped wrapper containing the form. Hidden `audit_score` input binds to root state (§8.D2).
```astro
<div class="bg-surface-elevated border-2 border-text-main shadow-[8px_8px_0px_0px_rgba(28,25,23,1)] p-8 md:p-10">
  <h3 class="text-2xl font-bold tracking-tight">Book your diagnostic</h3>
  <p class="mt-2 text-sm text-text-muted">We'll call back within 90 minutes.</p>
  <form class="mt-8 space-y-4">
    <input type="hidden" name="audit_score" :value="auditScore" />
    <!-- F1 / F2 / F4 inputs -->
    <!-- Canonical CTA Button (§8.E) as submit -->
  </form>
</div>
```

---

## 14. CARD VARIANT LIBRARY


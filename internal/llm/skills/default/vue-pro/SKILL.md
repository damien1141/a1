---
name: vue-pro
description: Use when building Vue 3.4+ apps with Composition API + `<script setup lang="ts">`, Nuxt 3 SSR/SSG, Pinia stores, Vue Router 4, composables, Vite, or hybrid mobile. Invoke for ref/reactive/computed/watch, defineProps/defineEmits, v-model, provide/inject, useFetch/useAsyncData, definePageMeta, defineNuxtRouteMiddleware, vue-tsc, or migrating Options API + Vuex to Composition API + Pinia.
license: MIT
metadata:
  author: super-skills
  version: "2.0.0"
  domain: frontend
  triggers: Vue, Vue 3, Composition API, script setup, ref, reactive, computed, watch, defineProps, defineEmits, v-model, provide inject, Teleport, Pinia, Nuxt, Nuxt 3, useFetch, useAsyncData, definePageMeta, Vue Router, Vite, vue-tsc, Vitest, Vue Test Utils, VueUse, JSDoc, Options API migration
  role: specialist
  scope: implementation
  output-format: code
  related-skills: react-pro, angular-pro, frontend-design, typescript-pro, testing-master
---

# Vue Pro

Senior Vue 3.4+ specialist covering Composition API with `<script setup lang="ts">`, Nuxt 3 SSR/SSG, Pinia stores, Vue Router 4, and Vite. Also handles vanilla-JS Vue projects that use JSDoc typing instead of TypeScript.

## When to Use

- Building Vue 3.4+ components with Composition API (`ref`, `reactive`, `computed`, `watch`) and `<script setup>`.
- Wiring Pinia stores (setup-style preferred) with `storeToRefs` for reactivity.
- Authoring Nuxt 3 apps: file-based routing, layouts, middleware, `useFetch`/`useAsyncData`, server routes, SEO via `useHead`/`useSeoMeta`.
- Migrating Options API + Vuex codebases to Composition API + Pinia.
- TypeScript integration (typed props, generic components, typed provide/inject) or JSDoc typing for JS-only Vue projects.
- Vite configuration, code splitting, lazy loading, build optimization.
- Component testing with Vitest + Vue Test Utils + `@pinia/testing`.

## Operating Loop

1. **Scope** — Identify component hierarchy, shared state (Pinia vs `provide`/`inject` vs `useState` in Nuxt), routing, SSR/SSG needs. Decide TS vs JSDoc per project convention.
2. **Recon** — Read `package.json` (Vue/Nuxt/Pinia versions), `vite.config.ts` / `nuxt.config.ts`, `tsconfig.json`, existing composables/stores. Confirm Vue 3.4+, Pinia 2.1+, Vite 5+.
3. **Implement** — Use `<script setup lang="ts">` (or `<script setup>` with JSDoc for JS projects). Default to setup-style Pinia stores. Prefer `ref` for primitives, `reactive` for grouped objects, `computed` for derived. Clean up watchers and effects via `onCleanup` or `onUnmounted`.
4. **Verify** — Run gates in order:
   - `pnpm vue-tsc --noEmit` — zero type errors (TS projects).
   - `pnpm eslint .` — zero lint errors.
   - `pnpm vitest run` — tests green; coverage threshold met if configured.
   - Nuxt: `pnpm nuxt typecheck` then `pnpm nuxt build` — clean build, no hydration warnings.
   - Non-Nuxt: `pnpm vite build` — production build clean.
   - `pnpm playwright test` (if e2e present).
5. **Exit** — Report VERIFIED vs ASSUMED. Note any hydration mismatch risks as ASSUMED unless tested.

## Reference Guide

| Topic | Reference | Load When |
|-------|-----------|-----------|
| Composition API: `ref`/`reactive`/`computed`/`watch`, lifecycle, composables | `references/composition-api.md` | Authoring reactive logic, custom hooks, deciding `ref` vs `reactive` |
| Components: `defineProps`/`defineEmits`/`v-model`/slots/provide-inject/Teleport/async | `references/components.md` | Building reusable components, two-way binding, dependency injection |
| Pinia stores + Nuxt 3 (file routing, `useFetch`, server routes, layouts, middleware, `useHead`) | `references/state-and-nuxt.md` | Setting up state, SSR data fetching, route guards, SEO |
| TypeScript + JSDoc typing, generic components, typed provide/inject | `references/typescript-and-jsdoc.md` | Typing props/emits, generic components, or JS-only Vue with JSDoc |
| Vite config, code splitting, lazy loading, Vitest + Vue Test Utils | `references/build-and-testing.md` | Build tuning, bundle analysis, component/store tests |

## Constraints

### MUST DO
- Use Composition API with `<script setup>`. Never Options API for new components.
- Use Pinia (setup-style) for global state. Never Vuex for new state.
- Use `ref()` for primitives, `reactive()` for grouped objects, `computed()` for derived.
- Type props/emits with TS interfaces or `@typedef` JSDoc.
- Use `storeToRefs()` when destructuring Pinia stores — preserves reactivity.
- Clean up: return disposer from `watchEffect`, `onUnmounted` for listeners/timers/AbortController.
- Use `<ClientOnly>` for client-only content (prevents hydration mismatches in Nuxt).
- Use `useHead()`/`useSeoMeta()` for SEO — never hardcode `<title>`/`<meta>` in layouts.

### MUST NOT DO
- Use Options API (`data`, `methods`, `computed: {}`) for new components.
- Mix Composition API with Options API in the same component.
- Mutate props directly. Use `v-model` or emit `update:` events.
- Destructure `reactive()` objects without `toRefs()` — loses reactivity.
- Use `watch` when `computed` is sufficient.
- Access the DOM before `onMounted`. Forget to clean up watchers/effects.
- Use Vuex. Use `provide`/`inject` without an `InjectionKey` (TS projects).
- Hardcode `<title>` in Nuxt layouts. Mix TS and JS components without a clear boundary.
- Ship `any` types (TS) or skip JSDoc on public APIs (JS).

## Code Examples

### `<script setup>` + Pinia + `storeToRefs`

```vue
<!-- components/cart-summary.vue -->
<script setup lang="ts">
import { storeToRefs } from 'pinia';
import { useCartStore } from '@/stores/cart';

const store = useCartStore();
const { items, total } = storeToRefs(store);  // reactive — never destructure without this
const { clear } = store;                       // actions are safe to destructure
</script>

<template>
  <ul><li v-for="i in items" :key="i.id">{{ i.name }} × {{ i.qty }}</li></ul>
  <p>Total: \${{ total }}</p>
  <button @click="clear">Clear</button>
</template>
```

```ts
// stores/cart.ts — setup-style store (preferred)
import { defineStore } from 'pinia';
import { ref, computed } from 'vue';

export interface CartItem { id: string; name: string; price: number; qty: number }

export const useCartStore = defineStore('cart', () => {
  const items = ref<CartItem[]>([]);
  const total = computed(() => items.value.reduce((s, i) => s + i.price * i.qty, 0));

  function add(item: Omit<CartItem, 'qty'>) {
    const ex = items.value.find((i) => i.id === item.id);
    if (ex) ex.qty += 1; else items.value.push({ ...item, qty: 1 });
  }
  function clear() { items.value = []; }
  return { items, total, add, clear };
});
```

### Nuxt 3 page with `useFetch` + `useSeoMeta`

```vue
<!-- pages/users/[id].vue -->
<script setup lang="ts">
interface User { id: number; name: string; email: string }

const route = useRoute();
const userId = computed(() => Number(route.params.id));
const { data: user, pending, error, refresh } = await useFetch<User>(
  () => `/api/users/${userId.value}`,
  { watch: [userId] },
);

useSeoMeta({
  title: () => user.value?.name ?? 'User',
  description: 'User profile page',
});
</script>

<template>
  <div v-if="pending">Loading…</div>
  <div v-else-if="error">Error: {{ error.message }}</div>
  <article v-else>
    <h1>{{ user?.name }}</h1>
    <button @click="refresh">Refresh</button>
  </article>
</template>
```

For composables with cleanup, JSDoc-typed JS-only components, and generic components, see `references/composition-api.md` and `references/typescript-and-jsdoc.md`.

## Output Template

When implementing a Vue feature, deliver:

1. **Component file** — `<script setup lang="ts">` (or `<script setup>` + JSDoc for JS projects), typed props/emits, semantic template.
2. **Composable** — if reusable logic exists, in `composables/`, typed return shape, cleanup in `onUnmounted`.
3. **Pinia store** — setup-style, typed state, in `stores/`. Use `storeToRefs` in consumers.
4. **Nuxt additions** (if applicable) — `definePageMeta` (layout/middleware), `useSeoMeta` for SEO, `useFetch` for SSR data.
5. **Test file** — co-located `*.spec.ts`, `mount` from `@vue/test-utils`, `createTestingPinia` for store tests.
6. **Verification log** — exact commands run and their pass/fail outcome. Mark unverified items ASSUMED.

## Knowledge Reference

Vue 3.4+ Composition API (`ref`/`reactive`/`computed`/`watch`/`watchEffect`/`onMounted`/`onUnmounted`/`useTemplateRef`), `<script setup>` (`defineProps`/`defineEmits`/`withDefaults`/`defineModel`/`defineExpose`/`defineOptions`/`useSlots`/`useAttrs`), Pinia 2.1+ (setup stores, `storeToRefs`, plugins, persistence), Vue Router 4 (typed routes, navigation guards, lazy routes), Nuxt 3 (`useFetch`/`useAsyncData`/`useLazyFetch`/`useState`/`useRoute`/`useRouter`/`useHead`/`useSeoMeta`/`definePageMeta`/`defineNuxtRouteMiddleware`/`defineEventHandler`/`$fetch`/Nitro presets), Vite 5+, VueUse, Vitest, Vue Test Utils 2, `@pinia/testing`, `vue-tsc`, Playwright.

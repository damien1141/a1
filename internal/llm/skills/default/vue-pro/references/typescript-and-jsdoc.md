# TypeScript + JSDoc typing for Vue 3

Vue 3.4+ ships first-class TypeScript support via `<script setup lang="ts">`. For projects on vanilla JS, comprehensive JSDoc typing is the alternative — both approaches reach the same type safety with `vue-tsc` / `tsc --checkJs`.

## TS: typed props/emits

```vue
<script setup lang="ts">
interface Props {
  title: string;
  count?: number;
  items?: string[];
  user: { id: number; name: string };
  callback?: (id: number) => void;
  config?: Record<string, unknown>;
  status?: 'success' | 'error' | 'warning';
}
const props = withDefaults(defineProps<Props>(), {
  count: 0,
  items: () => [],
  status: 'success',
});

type Emits = {
  update: [value: string];
  delete: [id: number];
  submit: [payload: { name: string; email: string }];
};
const emit = defineEmits<Emits>();

emit('update', 'x');              // OK
// emit('update', 123);           // TS error
```

## TS: `ref` / `reactive` / `computed`

```ts
import { ref, reactive, computed, type Ref, type ComputedRef } from 'vue';

const count = ref(0);                              // Ref<number>
const user = ref<User | null>(null);               // explicit nullable
const items = ref<string[]>([]);

const state = reactive<State>({ count: 0, user: { name: '' } });

const doubled = computed(() => count.value * 2);   // ComputedRef<number>
const explicit = computed<number>(() => count.value * 3);

const writable = computed<string>({
  get: () => `${user.value?.name ?? ''}`,
  set: (v) => { if (user.value) user.value.name = v; },
});
```

## TS: template refs

```vue
<script setup lang="ts">
import { ref, onMounted } from 'vue';
import ChildComponent from './Child.vue';

const inputRef = ref<HTMLInputElement | null>(null);
const childRef = ref<InstanceType<typeof ChildComponent> | null>(null);

onMounted(() => {
  inputRef.value?.focus();
  childRef.value?.someMethod();
});
</script>

<template>
  <input ref="inputRef" />
  <ChildComponent ref="childRef" />
</template>
```

Vue 3.5+ adds `useTemplateRef`:
```ts
import { useTemplateRef } from 'vue';
const inputEl = useTemplateRef<HTMLInputElement>('inputEl');
// template: <input ref="inputEl" />
```

## TS: generic components

```vue
<!-- GenericList.vue -->
<script setup lang="ts" generic="T extends { id: string }">
interface Props {
  items: T[];
  selected?: T;
}
interface Emits {
  (e: 'select', item: T): void;
}
const props = defineProps<Props>();
const emit = defineEmits<Emits>();

function select(item: T) { emit('select', item); }
</script>

<template>
  <ul>
    <li v-for="item in items" :key="item.id" @click="select(item)">
      <slot :item="item" />
    </li>
  </ul>
</template>
```

```vue
<!-- Usage -->
<script setup lang="ts">
import GenericList from './GenericList.vue';
interface User { id: string; name: string }
const users: User[] = [{ id: '1', name: 'John' }];

function handleSelect(u: User) { /* u typed as User */ }
</script>

<template>
  <GenericList :items="users" @select="handleSelect">
    <template #default="{ item }">{{ item.name }}</template>
  </GenericList>
</template>
```

## TS: typed provide/inject

```ts
// keys.ts
import type { InjectionKey, Ref } from 'vue';
export interface UserCtx {
  user: Ref<User | null>;
  updateUser: (u: User) => void;
}
export const userKey: InjectionKey<UserCtx> = Symbol('user');
```

```vue
<!-- Parent.vue -->
<script setup lang="ts">
import { provide, ref } from 'vue';
import { userKey } from './keys';
const user = ref<User | null>(null);
function updateUser(u: User) { user.value = u; }
provide(userKey, { user, updateUser });
</script>
```

```vue
<!-- Child.vue -->
<script setup lang="ts">
import { inject } from 'vue';
import { userKey, type UserCtx } from './keys';

const ctx = inject(userKey);
if (!ctx) throw new Error('userKey not provided');
// ctx.user: Ref<User | null>; ctx.updateUser: (u: User) => void

// With default
const fallback: UserCtx = { user: ref(null), updateUser: () => {} };
const ctx2 = inject(userKey, fallback);
</script>
```

## TS: typed Pinia store

```ts
// stores/user.ts
import { defineStore } from 'pinia';
import { ref, computed } from 'vue';

export interface User { id: number; name: string; role: 'admin' | 'user' }

export const useUserStore = defineStore('user', () => {
  const user = ref<User | null>(null);
  const users = ref<User[]>([]);
  const isAdmin = computed(() => user.value?.role === 'admin');

  async function fetchUser(id: number): Promise<User> {
    const u = await $fetch<User>(`/api/users/${id}`);
    user.value = u;
    return u;
  }
  function logout() { user.value = null; }

  return { user, users, isAdmin, fetchUser, logout };
});

export type UserStore = ReturnType<typeof useUserStore>;
```

## TS: augmenting Nuxt app / global properties

```ts
// types/app.d.ts
declare module '#app' {
  interface NuxtApp {
    $api: {
      get<T>(url: string): Promise<T>;
      post<T>(url: string, body: unknown): Promise<T>;
    };
  }
}

declare module 'vue' {
  interface ComponentCustomProperties {
    $api: {
      get<T>(url: string): Promise<T>;
      post<T>(url: string, body: unknown): Promise<T>;
    };
  }
}

export {};
```

## JSDoc: JS-only Vue components (no `lang="ts"`)

```vue
<script setup>
/**
 * @typedef {Object} UserCardProps
 * @property {string} name - Display name
 * @property {number} age
 * @property {boolean} [isAdmin=false]
 */
const props = defineProps({
  name: { type: String, required: true },
  age: { type: Number, required: true },
  isAdmin: { type: Boolean, default: false },
});

/**
 * @typedef {Object} UserCardEmits
 * @property {(id: string) => void} select
 */
const emit = defineEmits(['select']);

/** @param {string} id */
function handleSelect(id) {
  emit('select', id);
}
</script>
```

## JSDoc: composable with full type coverage

```js
// composables/use-counter.mjs
import { ref, computed } from 'vue';

/**
 * @typedef {Object} CounterState
 * @property {import('vue').Ref<number>} count
 * @property {import('vue').ComputedRef<boolean>} isPositive
 * @property {() => void} increment
 * @property {() => void} reset
 */

/**
 * @param {number} [initial=0]
 * @param {number} [step=1]
 * @returns {CounterState}
 */
export function useCounter(initial = 0, step = 1) {
  /** @type {import('vue').Ref<number>} */
  const count = ref(initial);
  const isPositive = computed(() => count.value > 0);

  function increment() { count.value += step; }
  function reset() { count.value = initial; }

  return { count, isPositive, increment, reset };
}
```

## JSDoc: shared `@typedef` across files

```js
// types/user.mjs
/**
 * @typedef {Object} User
 * @property {string}   id
 * @property {string}   name
 * @property {string}   email
 * @property {'admin'|'viewer'} role
 */
export {};
```

```js
// usage in another file
/** @type {import('./types/user.mjs').User} */
const user = { id: '1', name: 'John', email: 'j@e.com', role: 'admin' };
```

## Verification

```bash
# TS projects
pnpm vue-tsc --noEmit          # type-check .vue + .ts
pnpm nuxt typecheck            # Nuxt projects (wraps vue-tsc)

# JS-only projects with JSDoc (tsconfig.json: { checkJs: true, allowJs: true })
pnpm tsc --noEmit --checkJs
pnpm eslint . --ext .js,.vue   # eslint-plugin-jsdoc for annotation coverage
```

## Quick Reference

| Pattern | TS | JSDoc (JS) |
|---------|----|------------|
| Props | `defineProps<T>()` | `@typedef` + `defineProps({...})` |
| Defaults | `withDefaults(p, defaults)` | `default:` in runtime decl |
| Emits | `defineEmits<{ ... }>()` or tuple | `defineEmits(['x'])` + `@typedef` |
| Two-way | `defineModel<T>()` | `defineModel()` + `@type` |
| Generic comp | `<script setup lang="ts" generic="T">` | not available — use `unknown` + casts |
| Provide/Inject | `InjectionKey<T>` | string key + `@type` annotation |
| Pinia store | setup store with TS types | setup store with JSDoc-typed returns |
| Type-check | `vue-tsc --noEmit` | `tsc --noEmit --checkJs` |

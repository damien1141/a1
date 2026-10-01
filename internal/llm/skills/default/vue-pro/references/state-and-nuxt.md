# Pinia + Nuxt 3: state, routing, data fetching, server routes, SEO

## Pinia (setup stores preferred)

```ts
// stores/counter.ts
import { defineStore } from 'pinia';
import { ref, computed } from 'vue';

export const useCounterStore = defineStore('counter', () => {
  const count = ref(0);
  const doubleCount = computed(() => count.value * 2);
  const isEven = computed(() => count.value % 2 === 0);

  function increment() { count.value++; }
  function decrement() { count.value--; }
  async function incrementAsync() {
    await new Promise((r) => setTimeout(r, 1000));
    count.value++;
  }
  return { count, doubleCount, isEven, increment, decrement, incrementAsync };
});
```

```vue
<script setup lang="ts">
import { storeToRefs } from 'pinia';
import { useCounterStore } from '@/stores/counter';

const store = useCounterStore();
const { count, doubleCount, isEven } = storeToRefs(store);  // reactive
const { increment, decrement } = store;                      // actions: safe to destructure
</script>
```

### Cross-store access

```ts
import { defineStore } from 'pinia';
import { ref, computed } from 'vue';
import { useUserStore } from './user';
import { useProductStore } from './product';

export const useCartStore = defineStore('cart', () => {
  const userStore = useUserStore();
  const productStore = useProductStore();
  const items = ref<{ productId: number; qty: number }[]>([]);

  const total = computed(() =>
    items.value.reduce((sum, i) => {
      const p = productStore.getById(i.productId);
      return sum + (p?.price ?? 0) * i.qty;
    }, 0),
  );

  async function checkout() {
    if (!userStore.isLoggedIn) throw new Error('Login required');
    await fetch('/api/checkout', { method: 'POST', body: JSON.stringify({ items: items.value, userId: userStore.user?.id }) });
    items.value = [];
  }

  return { items, total, checkout };
});
```

### Persistence

```ts
// main.ts
import { createPinia } from 'pinia';
import piniaPluginPersistedstate from 'pinia-plugin-persistedstate';

const pinia = createPinia();
pinia.use(piniaPluginPersistedstate);

// stores/settings.ts
export const useSettingsStore = defineStore('settings', () => {
  const theme = ref<'light' | 'dark'>('light');
  function setTheme(t: 'light' | 'dark') { theme.value = t; }
  return { theme, setTheme };
}, {
  persist: true,                  // localStorage
});

export const useAuthStore = defineStore('auth', () => {
  const token = ref<string | null>(null);
  const user = ref<User | null>(null);
  return { token, user };
}, {
  persist: {
    key: 'auth',
    storage: sessionStorage,
    pick: ['token'],              // only persist token
  },
});
```

### Testing Pinia stores

```ts
import { setActivePinia, createPinia } from 'pinia';
import { describe, it, expect, beforeEach } from 'vitest';
import { useCounterStore } from '../counter';

describe('counter store', () => {
  beforeEach(() => setActivePinia(createPinia()));

  it('increments', () => {
    const s = useCounterStore();
    expect(s.count).toBe(0);
    s.increment();
    expect(s.count).toBe(1);
    expect(s.doubleCount).toBe(2);
  });
});
```

Or in components: `createTestingPinia({ initialState: { cart: { items: [...] } } })`.

## Nuxt 3 file-based routing

```
pages/
├── index.vue              # /
├── about.vue              # /about
├── users/
│   ├── index.vue          # /users
│   └── [id].vue           # /users/:id
├── blog/
│   └── [...slug].vue      # /blog/a/b/c (catch-all)
└── (marketing)/           # group, no URL segment
    └── pricing.vue
```

```vue
<!-- pages/users/[id].vue -->
<script setup lang="ts">
definePageMeta({ layout: 'admin', middleware: 'auth' });

const route = useRoute();
const router = useRouter();
const id = computed(() => route.params.id as string);

function goNext() { router.push(`/users/${Number(id) + 1}`); }
</script>
```

## Layouts

```vue
<!-- layouts/default.vue -->
<template>
  <header><nav>…</nav></header>
  <main><slot /></main>
  <footer>…</footer>
</template>
```

```vue
<!-- pages/admin/dashboard.vue -->
<script setup lang="ts">
definePageMeta({ layout: 'admin' });
</script>
```

## Data fetching

```vue
<!-- pages/users/[id].vue -->
<script setup lang="ts">
interface User { id: number; name: string }

const route = useRoute();
const userId = computed(() => Number(route.params.id));

// useFetch: SSR-safe, deduped, auto-imported
const { data: user, pending, error, refresh } = await useFetch<User>(
  () => `/api/users/${userId.value}`,
  {
    method: 'GET',
    query: { include: 'posts' },
    watch: [userId],             // refetch when userId changes
    transform: (u) => ({ ...u, fullName: `${u.name} (${u.id})` }),
  },
);

// useAsyncData: full control, custom key
const { data: stats } = await useAsyncData(`user-${userId.value}-stats`, () =>
  $fetch(`/api/users/${userId.value}/stats`),
);

// Lazy (non-blocking) fetch
const { data: posts } = useLazyFetch<Post[]>(`/api/users/${userId.value}/posts`);
</script>
```

| Composable | Use |
|------------|-----|
| `useFetch(url, opts)` | Standard SSR-safe GET/POST with auto key |
| `useAsyncData(key, fn)` | Custom fetcher, manual key |
| `useLazyFetch` / `useLazyAsyncData` | Non-blocking; data arrives after hydration |
| `$fetch(url, opts)` | Imperative fetch (server routes, plugins) |

## Server routes (Nitro)

```ts
// server/api/users.get.ts
import { defineEventHandler, getQuery } from 'h3';

export default defineEventHandler(async (event) => {
  const { page = 1, limit = 20 } = getQuery(event);
  const users = await prisma.user.findMany({
    skip: (Number(page) - 1) * Number(limit),
    take: Number(limit),
  });
  return users;
});
```

```ts
// server/api/users/[id].get.ts
import { defineEventHandler, getRouterParam, createError } from 'h3';

export default defineEventHandler(async (event) => {
  const id = getRouterParam(event, 'id');
  const user = await prisma.user.findUnique({ where: { id: Number(id) } });
  if (!user) throw createError({ statusCode: 404, message: 'User not found' });
  return user;
});

// server/api/users.post.ts
export default defineEventHandler(async (event) => {
  const body = await readBody(event);
  if (!body.email) throw createError({ statusCode: 400, message: 'email required' });
  return prisma.user.create({ data: body });
});
```

## Middleware

```ts
// middleware/auth.ts
export default defineNuxtRouteMiddleware((to, from) => {
  const auth = useAuthStore();
  if (!auth.isLoggedIn) {
    return navigateTo(`/login?redirect=${encodeURIComponent(to.path)}`);
  }
});

// middleware/logger.global.ts — global, runs on every navigation
export default defineNuxtRouteMiddleware((to) => {
  console.log(`Navigating to ${to.path}`);
});
```

Attach per-route via `definePageMeta({ middleware: 'auth' })` or `middleware: ['auth', 'logger']`.

## Composables (auto-imported)

```ts
// composables/use-auth.ts
export function useAuth() {
  const user = useState<User | null>('user', () => null);
  const isLoggedIn = computed(() => user.value !== null);

  async function login(email: string, password: string) {
    const { data } = await useFetch('/api/auth/login', {
      method: 'POST',
      body: { email, password },
    });
    if (data.value) user.value = data.value.user;
  }

  async function logout() {
    await $fetch('/api/auth/logout', { method: 'POST' });
    user.value = null;
    navigateTo('/login');
  }

  return { user, isLoggedIn, login, logout };
}
```

`useState` is Nuxt's SSR-safe shared state — preferable to module-level refs for cross-component state in Nuxt.

## Plugins

```ts
// plugins/api.ts
export default defineNuxtPlugin(() => {
  const api = $fetch.create({
    baseURL: '/api',
    onRequest({ options }) {
      const token = useCookie('token');
      if (token.value) {
        options.headers = { ...options.headers, Authorization: `Bearer ${token.value}` };
      }
    },
    onResponseError({ response }) {
      if (response.status === 401) navigateTo('/login');
    },
  });
  return { provide: { api } };
});

// usage
const { $api } = useNuxtApp();
const users = await $api<User[]>('/users');
```

## SEO

```vue
<script setup lang="ts">
useSeoMeta({
  title: 'My Page',
  ogTitle: 'My Page',
  description: 'Page description',
  ogDescription: 'Page description',
  ogImage: 'https://example.com/image.png',
});

// Dynamic
useSeoMeta({
  title: () => product.value?.name ?? 'Loading…',
});

// Lower-level useHead
useHead({
  htmlAttrs: { lang: 'en' },
  link: [{ rel: 'icon', type: 'image/x-icon', href: '/favicon.ico' }],
  script: [{ src: '/analytics.js', defer: true }],
});
</script>
```

## Config

```ts
// nuxt.config.ts
export default defineNuxtConfig({
  devtools: { enabled: true },
  modules: ['@pinia/nuxt', '@nuxtjs/tailwindcss', '@vueuse/nuxt'],
  runtimeConfig: {
    apiSecret: process.env.API_SECRET,           // server-only
    public: { apiBase: process.env.API_BASE || '/api' },
  },
  app: {
    head: {
      title: 'My App',
      meta: [
        { charset: 'utf-8' },
        { name: 'viewport', content: 'width=device-width, initial-scale=1' },
      ],
    },
  },
  css: ['~/assets/css/main.css'],
  typescript: { strict: true, typeCheck: true },
  nitro: { preset: 'vercel' },                   // or 'node-server', 'cloudflare'
});
```

## Hydration safety

```vue
<script setup lang="ts">
import { ref, onMounted } from 'vue';

const now = ref<string | null>(null);   // SSR renders null, client fills in
onMounted(() => { now.value = new Date().toLocaleTimeString(); });
</script>

<template>
  <ClientOnly>
    <HeavyChart />
    <template #fallback><div class="chart-skeleton">Loading…</div></template>
  </ClientOnly>
</template>
```

## Quick Reference

| Concern | Tool |
|---------|------|
| Global state | Pinia setup store + `storeToRefs` |
| SSR-safe state | Nuxt `useState` |
| Data fetching | `useFetch` / `useAsyncData` / `$fetch` |
| Routing | File-based `pages/`, `definePageMeta`, `useRoute`/`useRouter` |
| Guards | `defineNuxtRouteMiddleware` |
| Server API | `server/api/*.ts` (Nitro) |
| SEO | `useSeoMeta` / `useHead` |
| Plugins | `plugins/*.ts` via `defineNuxtPlugin` |
| Hydration-safe UI | `<ClientOnly>` |
| Persistence | `pinia-plugin-persistedstate` |

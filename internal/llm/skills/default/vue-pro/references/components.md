# Components: `defineProps`/`defineEmits`/`v-model`/slots/provide-inject/Teleport/async

## Props

```vue
<script setup lang="ts">
interface Props {
  title: string;
  count?: number;
  items?: string[];
  status?: 'success' | 'error' | 'warning';
}
const props = withDefaults(defineProps<Props>(), {
  count: 0,
  items: () => [],                // object/array defaults must be factory functions
  status: 'success',
});
</script>
```

Runtime declaration (for JS projects without TS):
```vue
<script setup>
import { type PropType } from 'vue';
const props = defineProps({
  title: { type: String, required: true },
  count: { type: Number, default: 0 },
  items: { type: Array as PropType<string[]>, default: () => [] },
  status: { type: String as PropType<'success'|'error'|'warning'>, default: 'success' },
});
</script>
```

## Emits

```vue
<script setup lang="ts">
// Call-signature syntax
interface Emits {
  (e: 'update', value: string): void;
  (e: 'delete', id: number): void;
  (e: 'submit', payload: { name: string; email: string }): void;
}
const emit = defineEmits<Emits>();

// Alternative: tuple syntax (Vue 3.3+)
type EmitsAlt = {
  update: [value: string];
  delete: [id: number];
};
const emit2 = defineEmits<EmitsAlt>();

function handleUpdate() { emit('update', 'new value'); }
</script>
```

Runtime with validation:
```vue
<script setup>
const emit = defineEmits({
  update: (value) => typeof value === 'string' && value.length > 0,
  delete: (id) => typeof id === 'number' && id > 0,
});
</script>
```

## `v-model` (single + multiple)

`v-model` on a component binds `modelValue` prop + `update:modelValue` emit. Vue 3.4+ supports `defineModel()` for two-way binding without manual prop/emit wiring.

```vue
<!-- CustomInput.vue — Vue 3.4+ idiomatic -->
<script setup lang="ts">
const model = defineModel<string>();
</script>
<template>
  <input :value="model" @input="model = ($event.target as HTMLInputElement).value" />
</template>
```

```vue
<!-- Parent.vue -->
<script setup lang="ts">
import { ref } from 'vue';
import CustomInput from './CustomInput.vue';
const query = ref('');
</script>
<template>
  <CustomInput v-model="query" />
</template>
```

Multiple v-models:
```vue
<!-- FilterPanel.vue -->
<script setup lang="ts">
const category = defineModel<string>('category');
const price = defineModel<number>('price');
</script>

<!-- Parent -->
<FilterPanel v-model:category="filters.category" v-model:price="filters.price" />
```

Pre-3.4 manual form (still valid):
```vue
<script setup lang="ts">
interface Props { modelValue: string }
interface Emits { (e: 'update:modelValue', v: string): void }
const props = defineProps<Props>();
const emit = defineEmits<Emits>();
</script>
<template>
  <input :value="modelValue" @input="emit('update:modelValue', ($event.target as HTMLInputElement).value)" />
</template>
```

## Slots

```vue
<!-- Card.vue -->
<script setup lang="ts">
import { useSlots } from 'vue';
const slots = useSlots();
const hasHeader = computed(() => !!slots.header);
</script>

<template>
  <div class="card">
    <div v-if="hasHeader" class="card-header"><slot name="header" /></div>
    <div class="card-body"><slot /></div>             <!-- default -->
    <div class="card-footer"><slot name="footer" :close="close" /></div>  <!-- scoped -->
  </div>
</template>
```

```vue
<!-- Parent -->
<Card>
  <template #header><h2>Title</h2></template>
  <p>Body content</p>
  <template #footer="{ close }">
    <button @click="close">Close</button>
  </template>
</Card>
```

Generic list with scoped slot:
```vue
<!-- GenericList.vue -->
<script setup lang="ts" generic="T extends { id: string }">
defineProps<{ items: T[] }>();
</script>
<template>
  <ul>
    <li v-for="(item, i) in items" :key="item.id">
      <slot :item="item" :index="i" />
    </li>
  </ul>
</template>
```

## Provide / Inject (typed)

```ts
// keys.ts
import type { InjectionKey, Ref } from 'vue';
import { inject, provide, readonly, ref } from 'vue';

export interface UserCtx {
  user: Ref<User | null>;
  updateUser: (u: User) => void;
}
export const userKey: InjectionKey<UserCtx> = Symbol('user');

// Parent
const user = ref<User | null>(null);
function updateUser(u: User) { user.value = u; }
provide(userKey, { user: readonly(user), updateUser });

// Child
const ctx = inject(userKey);
if (!ctx) throw new Error('userKey not provided');
ctx.user.value;                  // Ref<User | null>
ctx.updateUser(newUser);
```

## Teleport

```vue
<script setup lang="ts">
import { ref } from 'vue';
const show = ref(false);
</script>

<template>
  <button @click="show = true">Open</button>
  <Teleport to="body">
    <div v-if="show" class="modal" role="dialog" aria-modal="true">
      <h2>Modal</h2>
      <button @click="show = false">Close</button>
    </div>
  </Teleport>
  <!-- Conditional teleport -->
  <Teleport to="body" :disabled="!isMobile">
    <div>Only teleported on mobile</div>
  </Teleport>
</template>
```

## Dynamic components + `KeepAlive`

```vue
<script setup lang="ts">
import { shallowRef, type Component } from 'vue';
import HomeView from './HomeView.vue';
import AboutView from './AboutView.vue';

const views = { home: HomeView, about: AboutView } as const;
const current = shallowRef<Component>(HomeView);

function switchView(name: keyof typeof views) { current.value = views[name]; }
</script>

<template>
  <button v-for="(c, k) in views" :key="k" @click="switchView(k)">{{ k }}</button>
  <KeepAlive>
    <component :is="current" />
  </KeepAlive>
</template>
```

Use `shallowRef` (not `ref`) for component references — avoids unnecessary deep reactivity.

## Async components + Suspense

```ts
import { defineAsyncComponent } from 'vue';

const HeavyChart = defineAsyncComponent(() => import('./HeavyChart.vue'));

const AdminPanel = defineAsyncComponent({
  loader: () => import('./AdminPanel.vue'),
  loadingComponent: () => import('./LoadingSpinner.vue'),
  errorComponent: () => import('./ErrorDisplay.vue'),
  delay: 200,
  timeout: 10_000,
});
```

```vue
<Suspense>
  <template #default><HeavyChart /></template>
  <template #fallback><div>Loading…</div></template>
</Suspense>
```

## `defineExpose` (parent access via template ref)

```vue
<!-- ChildForm.vue -->
<script setup lang="ts">
import { ref } from 'vue';
const validate = () => { /* ... */ return true; };
const reset = () => { /* ... */ };
defineExpose({ validate, reset });
</script>

<!-- Parent -->
<script setup lang="ts">
import { ref, onMounted } from 'vue';
import ChildForm from './ChildForm.vue';
const formRef = ref<InstanceType<typeof ChildForm> | null>(null);
onMounted(() => formRef.value?.validate());
</script>
<template>
  <ChildForm ref="formRef" />
</template>
```

## Quick Reference

| API | Purpose |
|-----|---------|
| `defineProps<T>()` | Typed props |
| `withDefaults(p, defaults)` | Defaults for TS-typed props |
| `defineEmits<T>()` | Typed emits |
| `defineModel<T>()` | Two-way binding (Vue 3.4+) |
| `defineExpose({...})` | Expose methods to parent via template ref |
| `defineOptions({...})` | Set name, inheritAttrs, etc. in `<script setup>` |
| `useSlots()` / `useAttrs()` | Access slots/attrs in `<script setup>` |
| `<Teleport to>` | Render outside component tree |
| `<KeepAlive>` | Cache component instances |
| `<Suspense>` | Async component fallback |
| `defineAsyncComponent` | Lazy load |
| `provide`/`inject` + `InjectionKey` | Typed cross-tree DI |

# Composition API: `ref`/`reactive`/`computed`/`watch`, lifecycle, composables

## `<script setup>` syntax

```vue
<script setup lang="ts">
import { ref, reactive, computed, watch, watchEffect, onMounted, onUnmounted } from 'vue';
import UserCard from './UserCard.vue';   // auto-registered, no `components` option

interface Props { userId: number; optional?: string }
const props = withDefaults(defineProps<Props>(), { optional: 'default' });

interface Emits {
  (e: 'update', value: string): void;
  (e: 'delete', id: number): void;
}
const emit = defineEmits<Emits>();

const count = ref(0);
const user = reactive({ name: 'John', age: 30 });
const doubled = computed(() => count.value * 2);

function increment() {
  count.value++;
  emit('update', count.value.toString());
}

onMounted(() => console.log('mounted'));
onUnmounted(() => console.log('unmounted'));
</script>
```

## `ref` vs `reactive`

```ts
import { ref, reactive, toRefs, toValue, type MaybeRefOrGetter, type Ref } from 'vue';

// ref: primitives, reassignable values, composable returns
const count = ref(0);            // Ref<number>
count.value++;

// reactive: grouped objects with nested reactivity
const state = reactive({ count: 0, user: { name: 'John' } });
state.count++;
state.user.name = 'Jane';

// Destructure reactive → loses reactivity; use toRefs
const { count: refCount, user: refUser } = toRefs(state);
refCount.value++;                // still reactive

// Unwrap ref OR return plain value (Vue 3.3+)
function double(v: MaybeRefOrGetter<number>) {
  return toValue(v) * 2;
}
double(count);                   // ref
double(() => 5);                 // getter
double(7);                       // plain
```

**Rule of thumb:** `ref` for primitives and composable returns; `reactive` for grouped object state. Never destructure `reactive` without `toRefs`.

## Computed

```ts
const firstName = ref('John');
const lastName = ref('Doe');

// Read-only (cached until deps change)
const fullName = computed(() => `${firstName.value} ${lastName.value}`);

// Writable
const fullNameWritable = computed({
  get: () => `${firstName.value} ${lastName.value}`,
  set: (v: string) => {
    const [f, l] = v.split(' ');
    firstName.value = f;
    lastName.value = l;
  },
});
```

## Watchers

```ts
// Single source
watch(count, (n, o) => console.log(`${o} → ${n}`));

// Multiple sources
watch([count, user], ([nc, nu], [oc, ou]) => { /* ... */ });

// Getter for nested reactive
watch(
  () => user.name,
  (n) => console.log(`name: ${n}`),
  { immediate: true, deep: true },
);

// watchEffect — auto-tracks deps
watchEffect(() => console.log(`count is ${count.value}`));

// Cleanup
const stop = watchEffect((onCleanup) => {
  const t = setInterval(() => console.log('tick'), 1000);
  onCleanup(() => clearInterval(t));
});
stop();                          // stop watching
```

| Watcher | Use |
|---------|-----|
| `watch(source, cb)` | React to specific value change, access old value |
| `watchEffect(fn)` | Auto-tracked side effect; runs immediately |
| `watchPostEffect` | `watchEffect` with `flush: 'post'` (after DOM update) |
| `watchSyncEffect` | `watchEffect` with `flush: 'sync'` |

## Lifecycle hooks

```ts
onBeforeMount(() => {});
onMounted(() => { /* DOM is ready */ });
onBeforeUpdate(() => {});
onUpdated(() => {});
onBeforeUnmount(() => { /* cleanup */ });
onUnmounted(() => {});
onErrorCaptured((err, instance, info) => {
  console.error(err, info);
  return false;                  // prevent propagation
});
```

## Composables

### Counter

```ts
// composables/use-counter.ts
import { ref, computed } from 'vue';

export function useCounter(initial = 0) {
  const count = ref(initial);
  const doubled = computed(() => count.value * 2);
  function increment() { count.value++; }
  function reset() { count.value = initial; }
  return { count, doubled, increment, reset };
}
```

### Event listener with cleanup

```ts
// composables/use-event-listener.ts
import { onMounted, onUnmounted, toValue, type MaybeRefOrGetter } from 'vue';

export function useEventListener<T extends EventTarget>(
  target: MaybeRefOrGetter<T>,
  event: string,
  handler: EventListener,
) {
  onMounted(() => toValue(target).addEventListener(event, handler));
  onUnmounted(() => toValue(target).removeEventListener(event, handler));
}
```

### Async state with cancellation

```ts
// composables/use-async-state.ts
import { ref, onUnmounted, type Ref } from 'vue';

export function useAsyncState<T>(fn: () => Promise<T>, immediate = true) {
  const data: Ref<T | null> = ref(null);
  const error = ref<Error | null>(null);
  const loading = ref(false);
  let cancelled = false;

  async function execute() {
    loading.value = true;
    error.value = null;
    try {
      const result = await fn();
      if (!cancelled) data.value = result;
    } catch (e) {
      if (!cancelled) error.value = e instanceof Error ? e : new Error(String(e));
    } finally {
      if (!cancelled) loading.value = false;
    }
  }

  onUnmounted(() => { cancelled = true; });
  if (immediate) execute();

  return { data, error, loading, execute };
}
```

### Shared singleton state (module-level)

```ts
// composables/use-notifications.ts
import { ref, readonly } from 'vue';

interface Notification { id: string; message: string }

const notifications = ref<Notification[]>([]);   // module-level = singleton

export function useNotifications() {
  function notify(message: string) {
    const id = Date.now().toString();
    notifications.value.push({ id, message });
    setTimeout(() => dismiss(id), 5000);
  }
  function dismiss(id: string) {
    notifications.value = notifications.value.filter((n) => n.id !== id);
  }
  return {
    notifications: readonly(notifications),
    notify,
    dismiss,
  };
}
```

## Migration: Options API → Composition API

| Options API | Composition API |
|-------------|-----------------|
| `data() { return { x } }` | `const x = ref(...)` |
| `computed: { foo() }` | `const foo = computed(() => ...)` |
| `methods: { bar() }` | `function bar() {}` |
| `watch: { x(n, o) }` | `watch(x, (n, o) => ...)` |
| `mounted()` | `onMounted(() => ...)` |
| `unmounted()` | `onUnmounted(() => ...)` |
| `props: { x: String }` | `defineProps<{ x: string }>()` |
| `emits: ['y']` | `defineEmits<{ (e: 'y'): void }>()` |
| `this.$emit` | `emit(...)` |
| `inject: ['k']` / `provide: { k }` | `inject(k)` / `provide(k, value)` |
| `mixins: [M]` | composables: `const m = useM()` |
| `this.$nextTick` | `import { nextTick } from 'vue'` |

## Quick Reference

| Pattern | Use |
|---------|-----|
| `ref()` | Primitives, composable returns, template refs |
| `reactive()` | Grouped object state |
| `toRefs()` | Destructure reactive, keep reactivity |
| `toValue()` | Unwrap `ref` / getter / plain |
| `computed()` | Cached derived state |
| `watch()` | Specific-value side effects |
| `watchEffect()` | Auto-tracked side effects |
| `onMounted` / `onUnmounted` | DOM-ready / cleanup |
| `provide` / `inject` | Cross-tree DI |
| Module-level `ref` | Singleton shared state |

# Hooks, Effects, and `useSyncExternalStore`

`useEffect` is an escape hatch for **synchronizing with external systems**. Most code does not need it. Load this reference when authoring custom hooks, debugging effect dependencies, or deciding whether to reach for an effect at all.

## Decision Tree

```
Need to respond to something?
├── User interaction (click, submit)?
│   └── EVENT HANDLER (not an effect)
├── Component appeared on screen?
│   └── EFFECT (external sync, analytics)
├── Props/state changed and need a derived value?
│   └── COMPUTE IN RENDER (useMemo if expensive)
└── Need to reset all state when a prop changes?
    └── KEY PROP on the component
```

## Anti-pattern: Derived state in an effect

```tsx
// ❌ Bad — extra render, stale intermediate state
function Form() {
  const [first, setFirst] = useState('Taylor');
  const [last, setLast] = useState('Swift');
  const [full, setFull] = useState('');
  useEffect(() => { setFull(`${first} ${last}`); }, [first, last]);
  return <output>{full}</output>;
}

// ✅ Good — compute during render
function Form() {
  const [first, setFirst] = useState('Taylor');
  const [last, setLast] = useState('Swift');
  const full = `${first} ${last}`;
  return <output>{full}</output>;
}
```

## Anti-pattern: Reset state on prop change

```tsx
// ❌ Bad
function Profile({ userId }: { userId: string }) {
  const [draft, setDraft] = useState('');
  useEffect(() => { setDraft(''); }, [userId]);   // resets, but extra render + effect
  return <Editor value={draft} onChange={setDraft} />;
}

// ✅ Good — remount via key
function ProfilePage({ userId }: { userId: string }) {
  return <Profile key={userId} userId={userId} />;
}
```

## Anti-pattern: Event logic in an effect

```tsx
// ❌ Bad — fires on every render where isInCart is true (including initial mount)
function ProductPage({ product, addToCart }: Props) {
  useEffect(() => {
    if (product.isInCart) showNotification(`Added ${product.name}`);
  }, [product]);

  function handleBuy() { addToCart(product); }
  return <button onClick={handleBuy}>Buy</button>;
}

// ✅ Good — event handler knows exactly what happened
function ProductPage({ product, addToCart }: Props) {
  function handleBuy() {
    addToCart(product);
    showNotification(`Added ${product.name}`);
  }
  return <button onClick={handleBuy}>Buy</button>;
}
```

## Anti-pattern: Notify parent via effect

```tsx
// ❌ Bad — extra render, data flowing up via effect
function Toggle({ onChange }: { onChange: (v: boolean) => void }) {
  const [on, setOn] = useState(false);
  useEffect(() => { onChange(on); }, [on, onChange]);
  return <button onClick={() => setOn(!on)}>{on ? 'ON' : 'OFF'}</button>;
}

// ✅ Good — update in the same event, batched
function Toggle({ onChange }: { onChange: (v: boolean) => void }) {
  const [on, setOn] = useState(false);
  function toggle() {
    const next = !on;
    setOn(next);
    onChange(next);
  }
  return <button onClick={toggle}>{on ? 'ON' : 'OFF'}</button>;
}

// ✅ Best — fully controlled
function Toggle({ on, onChange }: { on: boolean; onChange: (v: boolean) => void }) {
  return <button onClick={() => onChange(!on)}>{on ? 'ON' : 'OFF'}</button>;
}
```

## Anti-pattern: Race condition in fetch effect

```tsx
// ❌ Bad — late response from "hel" overwrites "hello"
function Search({ query }: { query: string }) {
  const [results, setResults] = useState<Item[]>([]);
  useEffect(() => {
    fetchResults(query).then(setResults);
  }, [query]);
  return <List items={results} />;
}

// ✅ Good — AbortController cancels stale requests
function Search({ query }: { query: string }) {
  const [results, setResults] = useState<Item[]>([]);
  useEffect(() => {
    const controller = new AbortController();
    fetchResults(query, { signal: controller.signal })
      .then(setResults)
      .catch((e) => { if (e.name !== 'AbortError') throw e; });
    return () => controller.abort();
  }, [query]);
  return <List items={results} />;
}
```

**Preferred**: Use TanStack Query / SWR / a Server Component. They handle cancellation, dedup, and race conditions for you.

## When you DO need an effect

- Subscriptions to external stores (prefer `useSyncExternalStore`).
- Imperative DOM APIs (canvas, third-party widgets, focus traps).
- Analytics that must fire because the component displayed.
- Syncing to a non-React widget (e.g., initializing a Mapbox GL map).

## `useSyncExternalStore` (external stores)

The React-blessed way to subscribe to non-React state. Avoids tearing during concurrent rendering.

```tsx
import { useSyncExternalStore } from 'react';

function subscribe(callback: () => void): () => void {
  window.addEventListener('online', callback);
  window.addEventListener('offline', callback);
  return () => {
    window.removeEventListener('online', callback);
    window.removeEventListener('offline', callback);
  };
}

function getSnapshot(): boolean {
  return navigator.onLine;
}

function getServerSnapshot(): boolean {
  return true;  // SSR default
}

export function useOnlineStatus(): boolean {
  return useSyncExternalStore(subscribe, getSnapshot, getServerSnapshot);
}
```

## Custom hook patterns

### `useDebounce`

```tsx
function useDebounce<T>(value: T, delayMs: number): T {
  const [debounced, setDebounced] = useState(value);
  useEffect(() => {
    const t = setTimeout(() => setDebounced(value), delayMs);
    return () => clearTimeout(t);
  }, [value, delayMs]);
  return debounced;
}
```

### `useLocalStorage` (SSR-safe)

```tsx
function useLocalStorage<T>(key: string, initial: T) {
  const [value, setValue] = useState<T>(initial);

  useEffect(() => {
    if (typeof window === 'undefined') return;
    const stored = window.localStorage.getItem(key);
    if (stored) setValue(JSON.parse(stored) as T);
  }, [key]);

  useEffect(() => {
    if (typeof window === 'undefined') return;
    window.localStorage.setItem(key, JSON.stringify(value));
  }, [key, value]);

  return [value, setValue] as const;
}
```

### `useEventListener` (cleanup)

```tsx
function useEventListener<K extends keyof WindowEventMap>(
  event: K,
  handler: (e: WindowEventMap[K]) => void,
  target: EventTarget = window,
) {
  useEffect(() => {
    target.addEventListener(event, handler as EventListener);
    return () => target.removeEventListener(event, handler as EventListener);
  }, [event, handler, target]);
}
```

### `useMediaQuery`

```tsx
function useMediaQuery(query: string): boolean {
  const [matches, setMatches] = useState(false);
  useEffect(() => {
    const mql = window.matchMedia(query);
    setMatches(mql.matches);
    const onChange = (e: MediaQueryListEvent) => setMatches(e.matches);
    mql.addEventListener('change', onChange);
    return () => mql.removeEventListener('change', onChange);
  }, [query]);
  return matches;
}
```

## Effect cleanup patterns

### Subscription with explicit teardown

```tsx
useEffect(() => {
  const sub = api.subscribe(handleUpdate);
  return () => sub.unsubscribe();
}, []);
```

### Async with cancellation flag

```tsx
useEffect(() => {
  let cancelled = false;
  (async () => {
    const data = await loadData();
    if (!cancelled) setData(data);
  })();
  return () => { cancelled = true; };
}, []);
```

## Lifecycle → hook map (migration)

| Class lifecycle | Modern equivalent |
|-----------------|-------------------|
| `constructor` | `useState` initializer |
| `componentDidMount` | `useEffect(fn, [])` |
| `componentDidUpdate` | `useEffect(fn, [deps])` (rarely the right call — see anti-patterns) |
| `componentWillUnmount` | effect cleanup |
| `shouldComponentUpdate` | `React.memo` with comparator |
| `getDerivedStateFromProps` | render-time calculation (anti-pattern to keep) |
| `getSnapshotBeforeUpdate` | `useLayoutEffect` (rarely needed) |
| `componentDidCatch` | error boundary (still class until React ships hook) |

## Quick Reference

| Need | Use |
|------|-----|
| Derived value | compute in render |
| Expensive derived | `useMemo` |
| Reset all state on prop change | `key` prop |
| User event response | event handler |
| External system sync | `useEffect` with cleanup |
| External store subscribe | `useSyncExternalStore` |
| Cross-component state | lift up / Context / Zustand |
| Data fetch | TanStack Query / SWR / Server Component |

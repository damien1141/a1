# State Management: `useState`, Context, Zustand, Redux Toolkit, TanStack Query

Decision matrix: choose the simplest tool that fits the data's lifecycle.

| State type | Default tool | Upgrade when |
|------------|--------------|--------------|
| Local UI | `useState` / `useReducer` | — |
| Cross-tree, rarely changes | Context | Many consumers re-render unnecessarily |
| Cross-tree, frequent updates | Zustand | Need middleware (persist, devtools) or selectors |
| Complex app state, time-travel | Redux Toolkit | Need middleware, devtools, large team conventions |
| Server data (cache, mutations) | TanStack Query / SWR | Need stale-while-revalidate, invalidation, optimistic |

## `useState` / `useReducer` (local)

```tsx
function Counter() {
  const [count, setCount] = useState(0);
  const increment = () => setCount((c) => c + 1);  // functional form
  return <button onClick={increment}>{count}</button>;
}
```

`useReducer` for multi-field state transitions:
```tsx
type State = { items: CartItem[]; coupon: string | null };
type Action =
  | { type: 'add'; item: CartItem }
  | { type: 'remove'; id: string }
  | { type: 'applyCoupon'; code: string };

function cartReducer(state: State, action: Action): State {
  switch (action.type) {
    case 'add':
      return { ...state, items: [...state.items, action.item] };
    case 'remove':
      return { ...state, items: state.items.filter((i) => i.id !== action.id) };
    case 'applyCoupon':
      return { ...state, coupon: action.code };
    default:
      return state;
  }
}
```

## Context (simple global state)

```tsx
interface ThemeCtx { theme: 'light' | 'dark'; toggle: () => void }
const ThemeContext = createContext<ThemeCtx | null>(null);

function ThemeProvider({ children }: { children: React.ReactNode }) {
  const [theme, setTheme] = useState<'light' | 'dark'>('light');
  const toggle = useCallback(() => setTheme((t) => (t === 'light' ? 'dark' : 'light')), []);
  const value = useMemo(() => ({ theme, toggle }), [theme, toggle]);
  return <ThemeContext.Provider value={value}>{children}</ThemeContext.Provider>;
}

function useTheme() {
  const ctx = useContext(ThemeContext);
  if (!ctx) throw new Error('useTheme must be used inside <ThemeProvider>');
  return ctx;
}
```

**Context caveat:** every consumer re-renders when the provider value changes. Split contexts or use selectors (Zustand) for high-frequency updates.

## Zustand (recommended for medium complexity)

```tsx
import { create } from 'zustand';
import { persist, createJSONStorage } from 'zustand/middleware';

interface CartItem { id: string; name: string; price: number; qty: number }

interface CartStore {
  items: CartItem[];
  addItem: (item: CartItem) => void;
  removeItem: (id: string) => void;
  clear: () => void;
  total: () => number;
}

const useCartStore = create<CartStore>()(
  persist(
    (set, get) => ({
      items: [],
      addItem: (item) => set((s) => ({
        items: s.items.some((i) => i.id === item.id)
          ? s.items.map((i) => i.id === item.id ? { ...i, qty: i.qty + item.qty } : i)
          : [...s.items, item],
      })),
      removeItem: (id) => set((s) => ({ items: s.items.filter((i) => i.id !== id) })),
      clear: () => set({ items: [] }),
      total: () => get().items.reduce((sum, i) => sum + i.price * i.qty, 0),
    }),
    { name: 'cart-storage', storage: createJSONStorage(() => localStorage) },
  ),
);

// Selectors — only re-render when the selected slice changes
function CartTotal() {
  const total = useCartStore((s) => s.total());
  return <p>Total: ${total}</p>;
}

function Cart() {
  const items = useCartStore((s) => s.items);
  return <ul>{items.map((i) => <li key={i.id}>{i.name} × {i.qty}</li>)}</ul>;
}
```

## Redux Toolkit (complex state, devtools, middleware)

```tsx
import { createSlice, configureStore, type PayloadAction } from '@reduxjs/toolkit';
import { Provider, useSelector, useDispatch } from 'react-redux';

const counterSlice = createSlice({
  name: 'counter',
  initialState: { value: 0 },
  reducers: {
    increment: (s) => { s.value += 1; },
    decrement: (s) => { s.value -= 1; },
    incrementBy: (s, a: PayloadAction<number>) => { s.value += a.payload; },
  },
});

const store = configureStore({ reducer: { counter: counterSlice.reducer } });
type RootState = ReturnType<typeof store.getState>;
type AppDispatch = typeof store.dispatch;

const useAppSelector = useSelector.withTypes<RootState>();
const useAppDispatch = useDispatch.withTypes<AppDispatch>();

function Counter() {
  const count = useAppSelector((s) => s.counter.value);
  const dispatch = useAppDispatch();
  return <button onClick={() => dispatch(counterSlice.actions.increment())}>{count}</button>;
}

export function App() {
  return <Provider store={store}><Counter /></Provider>;
}
```

## TanStack Query v5 (server state)

```tsx
import {
  QueryClient, QueryClientProvider,
  useQuery, useMutation, useQueryClient,
} from '@tanstack/react-query';

const queryClient = new QueryClient();

export function App({ children }: { children: React.ReactNode }) {
  return <QueryClientProvider client={queryClient}>{children}</QueryClientProvider>;
}

interface User { id: string; name: string }

function UserProfile({ userId }: { userId: string }) {
  const qc = useQueryClient();
  const { data, isLoading, error } = useQuery({
    queryKey: ['user', userId],
    queryFn: () => fetch(`/api/users/${userId}`).then((r) => r.json() as Promise<User>),
    staleTime: 5 * 60 * 1000,    // 5 min
    gcTime: 30 * 60 * 1000,      // 30 min (was cacheTime in v4)
  });

  const mutation = useMutation({
    mutationFn: (patch: Partial<User>) =>
      fetch(`/api/users/${userId}`, { method: 'PATCH', body: JSON.stringify(patch) }).then((r) => r.json()),
    onMutate: async (patch) => {
      await qc.cancelQueries({ queryKey: ['user', userId] });
      const prev = qc.getQueryData<User>(['user', userId]);
      qc.setQueryData<User>(['user', userId], (old) => (old ? { ...old, ...patch } : old));
      return { prev };
    },
    onError: (_e, _patch, ctx) => {
      if (ctx?.prev) qc.setQueryData(['user', userId], ctx.prev);
    },
    onSettled: () => qc.invalidateQueries({ queryKey: ['user', userId] }),
  });

  if (isLoading) return <Skeleton />;
  if (error) return <ErrorState error={error} />;
  return <UserCard user={data} onSave={(p) => mutation.mutate(p)} />;
}
```

### Prefetch & SSR Hydration

```tsx
// app/users/page.tsx (Server Component)
import { HydrationBoundary, dehydrate, QueryClient } from '@tanstack/react-query';

export default async function UsersPage() {
  const qc = new QueryClient();
  await qc.prefetchQuery({
    queryKey: ['users'],
    queryFn: () => fetch('https://api.example.com/users').then((r) => r.json()),
  });
  return (
    <HydrationBoundary state={dehydrate(qc)}>
      <UsersList />
    </HydrationBoundary>
  );
}
```

## Migration notes

- **Vuex → Pinia** (Vue ecosystem): see `vue-pro`.
- **MobX**: still valid; prefer `observer` from `mobx-react-lite`. Most teams should pick Zustand instead for new code.
- **React Context + reducer**: fine up to ~3 consumers; switch to Zustand once re-renders show up in profiling.
- **`useState` in a Server Component**: not possible. Server Components hold no state. Promote to Client or use Server Actions.

## Quick Reference

| Solution | Best for | Boilerplate | Devtools |
|----------|----------|-------------|----------|
| `useState` | Local form/UI | None | React DevTools |
| `useReducer` | Multi-field state machine | Low | React DevTools |
| Context | Theme, auth, locale | Low | React DevTools |
| Zustand | Cart, UI store, persisted | Low | Zustand DevTools |
| Redux Toolkit | Large team, middleware | Medium | Redux DevTools |
| TanStack Query | Server data, cache, mutations | Low | React Query DevTools |

# Performance: `memo`, `lazy`, transitions, virtualization, Vercel rules

Consolidates React 19 + Next.js 15 performance rules from the Vercel Engineering best-practices guide plus canonical memo/lazy/transition patterns. Load when profiling re-renders, auditing bundle size, or fixing data waterfalls.

## Priority order (biggest wins first)

1. **Eliminate waterfalls** — parallelize `await` with `Promise.all`; start independent promises before awaiting dependent ones.
2. **Shrink the bundle** — direct imports (avoid barrel files), `next/dynamic` for heavy components, defer third-party analytics.
3. **Server-side** — `React.cache()` for per-request dedup, LRU for cross-request, minimize RSC serialization, parallelize server fetches via composition.
4. **Client data fetching** — TanStack Query / SWR for dedup; coalesce global event listeners.
5. **Re-renders** — `memo`, narrow `useEffect` deps, functional `setState`, lazy state init, `useTransition` for non-urgent updates.
6. **Rendering** — `content-visibility` for long lists, hoist static JSX, animate wrappers not SVGs, `@if`-style ternaries (not `&&`).
7. **JS micro** — `Set`/`Map` for O(1) lookups, hoist RegExp out of loops, `toSorted()` for immutability, combine filter/map passes.

## Parallel fetch (the #1 fix)

```tsx
// ❌ Bad — 3 round trips serialized
const user = await fetchUser();
const posts = await fetchPosts();
const comments = await fetchComments();

// ✅ Good — 1 round trip
const [user, posts, comments] = await Promise.all([
  fetchUser(),
  fetchPosts(),
  fetchComments(),
]);
```

### Defer `await` until the branch that needs it

```tsx
// ❌ Bad — fetches userData even on skip path
async function handle(userId: string, skip: boolean) {
  const user = await fetchUser(userId);
  if (skip) return { skipped: true };
  return process(user);
}

// ✅ Good
async function handle(userId: string, skip: boolean) {
  if (skip) return { skipped: true };
  const user = await fetchUser(userId);
  return process(user);
}
```

### Start promises early, await late

```tsx
export async function GET(req: Request) {
  const sessionPromise = auth();          // start now
  const configPromise = fetchConfig();    // start now
  const session = await sessionPromise;
  const [config, data] = await Promise.all([configPromise, fetchData(session.user.id)]);
  return Response.json({ data, config });
}
```

## Bundle size

### Avoid barrel imports

```tsx
// ❌ Bad — pulls entire lodash
import { debounce } from 'lodash';

// ✅ Good — tree-shaken
import debounce from 'lodash/debounce';
```

### `next/dynamic` for heavy components

```tsx
import dynamic from 'next/dynamic';

const HeavyChart = dynamic(() => import('@/components/heavy-chart'), {
  loading: () => <Skeleton />,
  ssr: false,  // for client-only components (e.g., charts that need window)
});

export default function Page() {
  return <Suspense fallback={<Skeleton />}><HeavyChart data={data} /></Suspense>;
}
```

### Defer third-party scripts

```tsx
// next/script — load analytics after hydration
import Script from 'next/script';

<Script src="/analytics.js" strategy="afterInteractive" />
```

## Server-side rules

### `React.cache()` — per-request dedup

```tsx
import { cache } from 'react';

export const getUser = cache(async (id: string) =>
  db.user.findUnique({ where: { id } }),
);
// Calling getUser('123') twice in one request → one DB hit.
```

### Minimize RSC serialization

Pass IDs, not full objects, across the Server → Client boundary:

```tsx
// ❌ Bad — serializes entire user (including sensitive fields)
<ClientForm user={user} />

// ✅ Good — pass what the client needs
<ClientForm userId={user.id} initialName={user.name} />
```

### `after()` for non-blocking work

```tsx
import { after } from 'next/server';

export async function POST(req: Request) {
  const data = await req.json();
  await db.order.create({ data });
  after(() => sendReceiptEmail(data.email));  // don't block the response
  return Response.json({ ok: true });
}
```

## Re-render optimization

### `React.memo` with intent

```tsx
const ExpensiveList = memo(function ExpensiveList({ items }: { items: Item[] }) {
  return <ul>{items.map((i) => <li key={i.id}>{i.name}</li>)}</ul>;
});

// Custom comparator only when default shallow compare is insufficient
const UserCard = memo(
  function UserCard({ user }: { user: User }) {
    return <div>{user.name}</div>;
  },
  (prev, next) => prev.user.id === next.user.id,
);
```

### Stable callbacks with `useCallback`

```tsx
const handleClick = useCallback((id: string) => {
  setSelected(id);
}, []);   // empty deps = stable reference
```

### Functional `setState` for stable callbacks

```tsx
// ❌ Bad — handler depends on `items`
const addItem = (item) => setItems([...items, item]);

// ✅ Good — stable, no items dep
const addItem = useCallback((item: Item) => {
  setItems((prev) => [...prev, item]);
}, []);
```

### Lazy state init for expensive defaults

```tsx
// ❌ Bad — parses JSON on every render
const [data, setData] = useState(JSON.parse(localStorage.getItem('big') ?? '{}'));

// ✅ Good — parses once
const [data, setData] = useState(() =>
  typeof window !== 'undefined'
    ? JSON.parse(localStorage.getItem('big') ?? '{}')
    : {},
);
```

### `useTransition` for non-urgent updates

```tsx
function Search() {
  const [query, setQuery] = useState('');
  const [results, setResults] = useState<Item[]>([]);
  const [isPending, startTransition] = useTransition();

  function handleChange(e: React.ChangeEvent<HTMLInputElement>) {
    setQuery(e.target.value);  // urgent: input updates now
    startTransition(() => {    // non-urgent: results can be interrupted
      setResults(filterThousandsOfItems(e.target.value));
    });
  }

  return (
    <>
      <input value={query} onChange={handleChange} />
      {isPending ? <Spinner /> : <Results items={results} />}
    </>
  );
}
```

## Rendering

### `content-visibility` for long lists

```css
.long-list-item {
  content-visibility: auto;
  contain-intrinsic-size: 0 80px;  /* approximate row height */
}
```

### Hoist static JSX out of components

```tsx
// ❌ Bad — allocates the same JSX every render
function Page() {
  return <Layout><BigStaticHeader /></Layout>;
}

// ✅ Good — hoist once
const HEADER = <BigStaticHeader />;
function Page() {
  return <Layout>{HEADER}</Layout>;
}
```

### Ternary, not `&&` (avoids `0` rendering)

```tsx
// ❌ Risky — renders "0" if count is 0
{count && <Badge count={count} />}

// ✅ Safe
{count > 0 ? <Badge count={count} /> : null}
```

## Virtualization for long lists

```tsx
import { useVirtualizer } from '@tanstack/react-virtual';

function BigList({ items }: { items: Item[] }) {
  const parentRef = useRef<HTMLDivElement>(null);
  const v = useVirtualizer({
    count: items.length,
    getScrollElement: () => parentRef.current,
    estimateSize: () => 56,
    overscan: 5,
  });

  return (
    <div ref={parentRef} style={{ height: 600, overflow: 'auto' }}>
      <div style={{ height: v.getTotalSize(), position: 'relative' }}>
        {v.getVirtualItems().map((vi) => (
          <div
            key={vi.key}
            style={{
              position: 'absolute',
              top: 0,
              left: 0,
              width: '100%',
              transform: `translateY(${vi.start}px)`,
            }}
          >
            {items[vi.index].name}
          </div>
        ))}
      </div>
    </div>
  );
}
```

## JS micro-optimizations

```tsx
// Use Map for repeated lookups
const byId = useMemo(() => new Map(items.map((i) => [i.id, i])), [items]);
const found = byId.get(id);  // O(1)

// Combine filter+map into one pass
const activeNames = items.reduce<string[]>((acc, i) => {
  if (i.active) acc.push(i.name);
  return acc;
}, []);

// Hoist RegExp out of loops
const EMAIL = /^[\w.+-]+@[\w-]+\.[\w.-]+$/;
const valid = emails.filter((e) => EMAIL.test(e));

// toSorted (immutable)
const sorted = items.toSorted((a, b) => a.name.localeCompare(b.name));
```

## Verification

Profile before optimizing. Capture a React DevTools flamechart and a Next.js build trace (`next build`'s output or `--profile`). A 1ms render is not worth a `useMemo`.

| Symptom | First check |
|---------|------------|
| Slow initial paint | Network waterfall — parallelize server fetches |
| Slow hydration | Bundle size — `next/dynamic` heavy components |
| Janky input | Long-running render — `useTransition` |
| Unwanted re-renders | Inline objects/functions — `useMemo`/`useCallback` or lift out |
| Long-list jank | Virtualization (`@tanstack/react-virtual`) |
| Slow route transitions | Prefetch links (`<Link prefetch />`) |

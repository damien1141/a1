---
name: react-pro
description: Use when building React 19 + Next.js 15 App Router apps, RSC/Server Actions, Suspense streaming, Zustand/Context/TanStack Query state, memo/lazy performance tuning, RTL tests, or React Native (Expo) mobile surfaces. Invoke for useActionState, use(), ref-as-prop, error.tsx/loading.tsx, generateMetadata, revalidation, FlatList optimization, or migrating class components to hooks.
license: MIT
metadata:
  author: super-skills
  version: "2.0.0"
  domain: frontend
  triggers: React, React 19, Next.js, App Router, RSC, Server Components, Server Actions, useActionState, useOptimistic, useFormStatus, use(), Suspense, Zustand, TanStack Query, Redux Toolkit, Vitest, Testing Library, Playwright, React Native, Expo, FlatList, use client, use server, revalidatePath, generateMetadata
  role: specialist
  scope: implementation
  output-format: code
  related-skills: vue-pro, angular-pro, frontend-design, testing-master, typescript-pro
---

# React Pro

Senior React 19 + Next.js 15 specialist covering React Server Components, Server Actions, modern hooks, state management, performance, RTL testing, and React Native (Expo) surfaces.

## When to Use

- Building Next.js 15 App Router apps (`app/`), layouts, route handlers, loading/error boundaries.
- Authoring React 19 features: `use()`, `useActionState`, `useFormStatus`, `useOptimistic`, `ref` as prop, Actions, `<form action>`.
- Choosing state: `useState`, Context, Zustand, Redux Toolkit, TanStack Query.
- Performance tuning: `memo`, `useMemo`, `useCallback`, `lazy()`, `useTransition`, virtualization, bundle splitting.
- Tuning effects: deciding when NOT to use `useEffect` (derived state, key resets, event handlers).
- RTL + Vitest + MSW component tests, Playwright e2e.
- React Native (Expo Router): FlatList, SafeArea, KeyboardAvoidingView, MMKV, platform splits.

## Operating Loop

1. **Scope** — Identify component boundary, server/client split, state shape, target platforms (web/Native).
2. **Recon** — Read `app/` routes, `tsconfig.json`, `next.config.ts`, lint/test setup. Confirm React 19 + Next.js 15 + TS strict.
3. **Implement** — Default to Server Components. Push `'use client'` to leaves. Use Actions for mutations, Suspense for streaming. TS-strict: no `any`, typed props.
4. **Verify** — Run gates in order; do not skip:
   - `pnpm tsc --noEmit` — zero type errors.
   - `pnpm eslint .` — zero lint errors.
   - `pnpm vitest run` — tests green.
   - `pnpm next build` — clean prod build, no RSC serialization errors.
   - `pnpm playwright test` (if e2e present).
   - Native: `npx expo doctor` and `pnpm tsc --noEmit` before pushing a build.
5. **Exit** — Report VERIFIED (gates ran clean) vs ASSUMED (gates not run, UX not checked). Never claim "done" without explicit gate output.

## Reference Guide

| Topic | Reference | Load When |
|-------|-----------|-----------|
| RSC, App Router, layouts, Suspense, parallel routes, metadata | `references/rsc-and-app-router.md` | Authoring `app/` routes, streaming, layouts, `loading.tsx`/`error.tsx`, `generateMetadata` |
| React 19 Actions, `useActionState`, `useFormStatus`, `useOptimistic`, `use()`, ref-as-prop | `references/react-19-actions.md` | Forms, mutations, optimistic UI, reading promises/context in render |
| State: `useState`, Context, Zustand, Redux Toolkit, TanStack Query | `references/state-and-data.md` | Choosing a state strategy, server state caching, persistence |
| Hooks, effects, `useSyncExternalStore`, derived state, anti-patterns | `references/hooks-and-effects.md` | Custom hooks, effect cleanup, deciding if you need an Effect |
| Performance: `memo`, `lazy`, transitions, virtualization, Vercel rules | `references/performance.md` | Re-render fires, bundle size, waterfalls, long lists |
| RTL + Vitest + MSW + Playwright | `references/testing.md` | Component tests, hook tests, mocking fetch, e2e |
| React Native + Expo Router: FlatList, SafeArea, MMKV, platform splits | `references/react-native.md` | Mobile screens, list perf, platform-specific code, storage |

## Constraints

### MUST DO
- Default to Server Components. Add `'use client'` only at the leaf where interactivity or hooks are required.
- Use the App Router (`app/`). Never start new work on the Pages Router.
- Type props with TypeScript interfaces; never `any`.
- Validate Server Action input with Zod. Revalidate with `revalidatePath`/`revalidateTag` after mutations.
- Wrap async UI in `<Suspense>` with a meaningful fallback. Add `loading.tsx`/`error.tsx` to async segments.
- Use `next/image`, `next/font`, `generateMetadata` for SEO — never hardcode `<title>`/`<meta>` in JSX.
- Cleanup effects: return a disposer; `AbortController` for fetches; `useSyncExternalStore` for external stores.
- Memoize with intent: `memo` for prop-stable children, `useMemo` only for expensive calculations, `useCallback` only when passing to memoized children.
- Use stable keys (`item.id`), never array index, for dynamic lists. Honor `prefers-reduced-motion`.

### MUST NOT DO
- Use class components for new work (exception: error boundaries until React ships a hook equivalent).
- Use deprecated lifecycles (`componentWillMount`, `componentWillReceiveProps`).
- Convert Server Components to Client Components just to access data — fetch server-side first.
- Use `useEffect` for derived state (compute in render), resetting state on prop change (use `key`), or responding to user events (use a handler).
- Mutate state directly. Use `forwardRef` in new React 19 code (`ref` is a regular prop). Mix Pages Router with App Router.
- Skip `await` cleanup in effects (race conditions). Use a cancelled flag or `AbortController`.
- Ship `any` types without a documented reason. Deploy without running `next build` and confirming zero errors.

## Code Examples

### Server Action + `useActionState` (canonical form)
```tsx
// app/posts/actions.ts — 'use server', Zod validate, revalidate, redirect
export async function createPost(prev: State, formData: FormData): Promise<State> {
  const session = await auth();
  if (!session) redirect('/login');
  const parsed = Schema.safeParse({ title: formData.get('title') });
  if (!parsed.success) return { ok: false, errors: parsed.error.flatten().fieldErrors };
  await db.post.create({ data: { ...parsed.data, authorId: session.user.id } });
  revalidatePath('/posts');
  redirect('/posts');
  return { ok: true };
}

// app/posts/new/page.tsx — 'use client', useActionState
export function NewPostForm() {
  const [state, action, pending] = useActionState(createPost, { ok: false });
  return (
    <form action={action}>
      <input name="title" required />
      {state.errors?.title?.[0] && <p role="alert">{state.errors.title[0]}</p>}
      <button type="submit" disabled={pending}>{pending ? 'Publishing…' : 'Publish'}</button>
    </form>
  );
}
```
Full Server Component + Suspense + `generateMetadata` patterns: `references/rsc-and-app-router.md`. Full Actions + `useOptimistic` recipes: `references/react-19-actions.md`.

### Don't use `useEffect` for derived state
```tsx
// ❌ Bad: extra render, stale state
function Filtered({ todos, filter }: Props) {
  const [visible, setVisible] = useState(todos);
  useEffect(() => { setVisible(applyFilter(todos, filter)); }, [todos, filter]);
  return <List items={visible} />;
}
// ✅ Good: compute in render (memoize if expensive)
function Filtered({ todos, filter }: Props) {
  const visible = useMemo(() => applyFilter(todos, filter), [todos, filter]);
  return <List items={visible} />;
}
```

### Reset state on identity change with `key`, not an effect
```tsx
function ProfilePage({ userId }: { userId: string }) {
  return <Profile key={userId} userId={userId} />;
}
```

## Output Template

When implementing a React/Next.js feature, deliver:

1. **Component file** — TS-strict, semantic HTML, ARIA where needed, `'use client'` only if required.
2. **Action/route handler** — Zod-validated, `revalidatePath`/`revalidateTag` after mutation, auth check.
3. **Test file** — RTL for components, `renderHook` for hooks, MSW for fetch. Co-located `*.test.tsx`.
4. **Metadata** — `generateMetadata` for dynamic routes; `loading.tsx`/`error.tsx` for async segments.
5. **Verification log** — exact commands run and their pass/fail outcome. Mark unverified items ASSUMED.

## Knowledge Reference

React 19 (`use()`, `useActionState`, `useFormStatus`, `useOptimistic`, ref-as-prop, Actions, `<form action>`), React Server Components, Next.js 15 App Router (layouts, templates, parallel/intercepting routes, route handlers, middleware, `generateMetadata`, `generateStaticParams`, route segment config), Suspense + streaming SSR, `revalidatePath`/`revalidateTag`/`unstable_cache`, Zod, Zustand, Redux Toolkit, TanStack Query v5, React Router v7, RTL, Vitest, MSW, Playwright, `@tanstack/react-virtual`, React Native 0.74+ / Expo SDK 51+ / Expo Router v3, MMKV, Reanimated 3.

# React 19 Actions, `useActionState`, `useOptimistic`, `use()`

React 19 introduces Actions (mutations over `<form action>`), `useActionState` for form state + pending, `useFormStatus` for child submit buttons, `useOptimistic` for optimistic UI, `use()` for reading promises/context in render, and `ref` as a regular prop (no `forwardRef`).

## `useActionState` (Server Actions with state)

`useActionState(action, initialState)` returns `[state, wrappedAction, isPending]`. The action signature is `(prevState, formData) => Promise<state>`.

```tsx
// app/newsletter/actions.ts
'use server';
import { z } from 'zod';
import { revalidatePath } from 'next/cache';
import { subscribe } from '@/lib/email';

const Schema = z.object({ email: z.string().email() });
export type State = { ok: boolean; error?: string };

export async function subscribeAction(prev: State, formData: FormData): Promise<State> {
  const parsed = Schema.safeParse({ email: formData.get('email') });
  if (!parsed.success) return { ok: false, error: 'Invalid email' };
  try {
    await subscribe(parsed.data.email);
    revalidatePath('/');
    return { ok: true };
  } catch (e) {
    return { ok: false, error: e instanceof Error ? e.message : 'Failed' };
  }
}
```

```tsx
// app/newsletter/subscribe-form.tsx
'use client';
import { useActionState } from 'react';
import { subscribeAction, type State } from './actions';

export function SubscribeForm() {
  const [state, action, pending] = useActionState<State, FormData>(
    subscribeAction,
    { ok: false },
  );
  return (
    <form action={action} aria-live="polite">
      <label htmlFor="email">Email</label>
      <input id="email" name="email" type="email" required disabled={pending} />
      <button type="submit" disabled={pending}>
        {pending ? 'Subscribing…' : 'Subscribe'}
      </button>
      {state.error && <p role="alert">{state.error}</p>}
      {state.ok && <p>Check your inbox to confirm.</p>}
    </form>
  );
}
```

### Drop into a Server Component (no JS for the form)

```tsx
// app/posts/new/page.tsx — Server Component, no 'use client'
import { createPost } from './actions';

export default function NewPostPage() {
  return (
    <form action={createPost}>
      <input name="title" required />
      <textarea name="content" required />
      <button type="submit">Create</button>
    </form>
  );
}
```

This works with progressive enhancement — the form submits even without JS.

## `useFormStatus` (child submit button)

`useFormStatus()` reads the state of the nearest `<form>` ancestor. Use it to build submit buttons that show pending state without prop-drilling. **Must be rendered inside a `<form>`.**

```tsx
'use client';
import { useFormStatus } from 'react-dom';

function SubmitButton() {
  const { pending, data, method, action } = useFormStatus();
  return (
    <button type="submit" disabled={pending}>
      {pending ? 'Saving…' : 'Save'}
    </button>
  );
}

export function ContactForm() {
  return (
    <form action={submitAction}>
      <input name="message" required />
      <SubmitButton />
    </form>
  );
}
```

## `useOptimistic` (instant UI updates)

`useOptimistic(state, reducer)` returns `[optimisticState, addOptimistic]`. Pairs with a Server Action to show the change immediately while the request is in flight; React automatically rolls back on completion or error.

```tsx
'use client';
import { useOptimistic } from 'react';
import { addTodo } from './actions';

interface Todo { id: string; text: string; completed: boolean }

export function TodoList({ todos }: { todos: Todo[] }) {
  const [optimisticTodos, addOptimisticTodo] = useOptimistic<Todo, Todo>(
    todos,
    (state, newTodo) => [...state, newTodo],
  );

  async function handleSubmit(formData: FormData) {
    const text = formData.get('text') as string;
    const temp: Todo = { id: crypto.randomUUID(), text, completed: false };
    addOptimisticTodo(temp);              // instant UI
    await addTodo(formData);              // server persistence
  }

  return (
    <>
      <ul>
        {optimisticTodos.map((t) => (
          <li key={t.id} style={{ opacity: t.id.startsWith('temp') ? 0.5 : 1 }}>
            {t.text}
          </li>
        ))}
      </ul>
      <form action={handleSubmit}>
        <input name="text" required />
        <button type="submit">Add</button>
      </form>
    </>
  );
}
```

## `use()` — Read Promises & Context in Render

`use(promise)` lets a component read a promise during render. The promise must come from a parent (never create a promise in the render body). With context, `use` can be called conditionally (unlike `useContext`).

```tsx
import { use, Suspense } from 'react';

// Parent creates the promise, child unwraps it.
function Comments({ commentsPromise }: { commentsPromise: Promise<Comment[]> }) {
  const comments = use(commentsPromise);  // suspends until resolved
  return <ul>{comments.map((c) => <li key={c.id}>{c.text}</li>)}</ul>;
}

export function Post({ postId }: { postId: string }) {
  // Kick off the fetch before rendering; React dedupes per request via cache().
  const commentsPromise = fetchComments(postId);
  return (
    <article>
      <PostContent id={postId} />
      <Suspense fallback={<CommentsSkeleton />}>
        <Comments commentsPromise={commentsPromise} />
      </Suspense>
    </article>
  );
}
```

Conditional context read:
```tsx
function ThemedBox({ enabled }: { enabled: boolean }) {
  if (!enabled) return null;
  const theme = use(ThemeContext);   // OK with use(); not OK with useContext()
  return <div className={theme}>…</div>;
}
```

## `ref` as a Regular Prop (no `forwardRef`)

In React 19, `ref` is just a prop. Drop `forwardRef` in new code.

```tsx
function Input({ ref, ...props }: { ref?: React.Ref<HTMLInputElement> } & React.InputHTMLAttributes<HTMLInputElement>) {
  return <input ref={ref} {...props} />;
}

function Form() {
  const inputRef = useRef<HTMLInputElement>(null);
  return <Input ref={inputRef} placeholder="Name" />;
}
```

For library code still on React 18, `forwardRef` is the bridge — but mark for migration.

## Server Action Patterns

### Validation + Auth + Redirect

```tsx
'use server';
import { z } from 'zod';
import { revalidatePath } from 'next/cache';
import { redirect } from 'next/navigation';
import { auth } from '@/lib/auth';
import { db } from '@/lib/db';

const Schema = z.object({
  title: z.string().min(3).max(120),
  content: z.string().min(10),
});

export async function createPost(prev: State, formData: FormData): Promise<State> {
  const session = await auth();
  if (!session) redirect('/login');

  const parsed = Schema.safeParse({
    title: formData.get('title'),
    content: formData.get('content'),
  });
  if (!parsed.success) {
    return { ok: false, errors: parsed.error.flatten().fieldErrors };
  }

  const post = await db.post.create({ data: { ...parsed.data, authorId: session.user.id } });
  revalidatePath('/posts');
  redirect(`/posts/${post.id}`);
  return { ok: true };  // unreachable; satisfies TS
}
```

### Programmatic invocation from a Client Component

```tsx
'use client';
import { deletePost } from './actions';

export function DeleteButton({ postId }: { postId: string }) {
  return (
    <button
      onClick={async () => {
        if (!confirm('Delete this post?')) return;
        await deletePost(postId);   // revalidateTag inside
      }}
    >
      Delete
    </button>
  );
}
```

### Inline action (Server Component with form)

```tsx
// app/posts/page.tsx
import { db } from '@/lib/db';
import { revalidatePath } from 'next/cache';

export default async function Posts() {
  const posts = await db.post.findMany();

  async function deletePost(formData: FormData) {
    'use server';
    const id = formData.get('id') as string;
    await db.post.delete({ where: { id } });
    revalidatePath('/posts');
  }

  return (
    <ul>
      {posts.map((p) => (
        <li key={p.id}>
          {p.title}
          <form action={deletePost}>
            <input type="hidden" name="id" value={p.id} />
            <button type="submit">Delete</button>
          </form>
        </li>
      ))}
    </ul>
  );
}
```

### File Upload

```tsx
'use server';
import { writeFile, mkdir } from 'node:fs/promises';
import { join } from 'node:path';

export async function uploadAvatar(formData: FormData) {
  const file = formData.get('avatar') as File;
  if (!file || file.size > 5 * 1024 * 1024) {
    return { error: 'File too large (max 5MB)' };
  }
  const bytes = Buffer.from(await file.arrayBuffer());
  const dir = join(process.cwd(), 'public', 'uploads');
  await mkdir(dir, { recursive: true });
  const path = join(dir, file.name);
  await writeFile(path, bytes);
  return { success: true, path: `/uploads/${file.name}` };
}
```

### Cookies

```tsx
'use server';
import { cookies } from 'next/headers';

export async function setTheme(theme: 'light' | 'dark') {
  const store = await cookies();
  store.set('theme', theme, {
    httpOnly: true,
    secure: process.env.NODE_ENV === 'production',
    sameSite: 'lax',
    maxAge: 60 * 60 * 24 * 365,
    path: '/',
  });
}

export async function getTheme(): Promise<'light' | 'dark'> {
  const store = await cookies();
  return (store.get('theme')?.value as 'light' | 'dark') ?? 'light';
}
```

## Quick Reference

| Hook | Purpose |
|------|---------|
| `useActionState(action, initial)` | Form action state + `isPending` |
| `useFormStatus()` | Pending state of nearest `<form>` (child only) |
| `useOptimistic(state, reducer)` | Optimistic UI; auto-rollback |
| `use(promise)` | Read promise in render (Suspense) |
| `use(context)` | Read context (callable conditionally) |
| `ref` (prop) | Direct ref prop, no `forwardRef` |

| Pattern | Use |
|---------|-----|
| `'use server'` file | Define Server Actions |
| `<form action={fn}>` | Progressive-enhancement mutation |
| `useActionState` + `useFormStatus` | Pending UI without prop drilling |
| `useOptimistic` | Instant feedback for mutations |
| `revalidatePath` / `revalidateTag` | Refresh cached data after mutation |
| `redirect()` | Post-mutation navigation (throws internally) |

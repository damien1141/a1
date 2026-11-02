# RSC, App Router, Suspense, Metadata

Consolidates React Server Component patterns, Next.js 15 App Router file conventions, streaming with Suspense, and the Metadata API. Load when authoring `app/` routes or wiring async data.

## Server vs Client Components

```tsx
// Server Component — default in App Router.
// Can: fetch data, access backend, use async/await, read cookies/headers.
// Cannot: use hooks, browser APIs, event handlers.
import { db } from '@/lib/db';

interface Product { id: string; name: string; price: number }

export default async function ProductsPage() {
  const products: Product[] = await db.product.findMany();
  return (
    <ul>{products.map((p) => <li key={p.id}>{p.name} — ${p.price}</li>)}</ul>
  );
}
```

```tsx
// Client Component — explicit boundary.
'use client';
import { useState } from 'react';

export function AddToCartButton({ productId, price }: { productId: string; price: number }) {
  const [pending, setPending] = useState(false);
  return (
    <button
      disabled={pending}
      onClick={async () => {
        setPending(true);
        await addToCart(productId);
        setPending(false);
      }}
    >
      {pending ? 'Adding…' : `Add $${price}`}
    </button>
  );
}
```

| Capability | Server | Client |
|------------|--------|--------|
| `async/await` data fetching | ✅ | ⚠️ via SWR/React Query |
| DB, fs, secrets | ✅ | ❌ |
| `useState`, `onClick`, browser APIs | ❌ | ✅ |
| Streaming via Suspense | ✅ | ❌ |
| Bundle cost | 0 KB | Adds to bundle |

**Push `'use client'` to leaves.** Fetch in Server Components, pass serializable props down. Nest Server Components inside Client Components via `children` (the only Server-Component-shaped prop a Client Component can take).

## File Conventions

```
app/
├── layout.tsx              # Root layout (required)
├── page.tsx                # Home (/)
├── loading.tsx             # Loading fallback (Suspense)
├── error.tsx               # Error boundary (must be 'use client')
├── not-found.tsx           # 404
├── template.tsx            # Re-mounted layout per navigation
├── (marketing)/            # Route group (no URL segment)
│   ├── layout.tsx
│   └── about/page.tsx      # /about
├── dashboard/
│   ├── layout.tsx
│   ├── page.tsx
│   └── @analytics/page.tsx # Parallel slot
├── blog/
│   ├── [slug]/page.tsx     # Dynamic
│   └── [...slug]/page.tsx  # Catch-all
└── api/users/route.ts      # Route Handler
```

### Root Layout (required)

```tsx
// app/layout.tsx
import type { Metadata } from 'next';
import { Inter } from 'next/font/google';
import './globals.css';

const inter = Inter({ subsets: ['latin'] });

export const metadata: Metadata = {
  title: { default: 'My App', template: '%s | My App' },
  description: 'Next.js 15 application',
};

export default function RootLayout({ children }: { children: React.ReactNode }) {
  return (
    <html lang="en">
      <body className={inter.className}>{children}</body>
    </html>
  );
}
```

### Layout with Auth Guard

```tsx
// app/dashboard/layout.tsx
import { redirect } from 'next/navigation';
import { auth } from '@/lib/auth';
import { Sidebar } from '@/components/sidebar';

export default async function DashboardLayout({ children }: { children: React.ReactNode }) {
  const session = await auth();
  if (!session) redirect('/login');
  return (
    <div className="flex">
      <Sidebar user={session.user} />
      <main className="flex-1">{children}</main>
    </div>
  );
}
```

### `loading.tsx` and `error.tsx` (per segment)

```tsx
// app/dashboard/loading.tsx
export default function Loading() {
  return <div className="animate-pulse">Loading dashboard…</div>;
}
```

```tsx
// app/error.tsx — MUST be 'use client'
'use client';
export default function Error({
  error, reset,
}: { error: Error & { digest?: string }; reset: () => void }) {
  return (
    <div>
      <h2>Something broke: {error.digest}</h2>
      <button onClick={reset}>Try again</button>
    </div>
  );
}
```

## Streaming with Suspense

Wrap slow async children in `<Suspense>` so the page shell renders immediately and the slow part streams in.

```tsx
import { Suspense } from 'react';

async function Reviews({ productId }: { productId: string }) {
  const reviews = await db.review.findMany({ where: { productId } }); // 800ms
  return <ul>{reviews.map((r) => <li key={r.id}>{r.text}</li>)}</ul>;
}

export default function Page({ params }: { params: Promise<{ id: string }> }) {
  return (
    <article>
      <h1>Product</h1>
      <Suspense fallback={<p>Loading reviews…</p>}>
        {/* @ts-expect-error Server Component */}
        <Reviews productId={(await params).id} />
      </Suspense>
    </article>
  );
}
```

Better pattern: resolve params at the top.

```tsx
export default async function Page({ params }: { params: Promise<{ id: string }> }) {
  const { id } = await params;
  return (
    <article>
      <Suspense fallback={<ReviewsSkeleton />}>
        <Reviews productId={id} />
      </Suspense>
    </article>
  );
}
```

## Parallel Data Fetching

```tsx
export default async function Dashboard({ params }: { params: Promise<{ id: string }> }) {
  const { id } = await params;
  // Promise.all — no waterfall
  const [user, orders, recs] = await Promise.all([
    db.user.findUnique({ where: { id } }),
    db.order.findMany({ where: { userId: id } }),
    db.recommendation.findMany({ where: { userId: id }, take: 5 }),
  ]);
  return (<><UserHeader user={user} /><OrderList orders={orders} /><Recs items={recs} /></>);
}
```

For partial dependencies use `Promise.all` with a chain that starts independent promises early:

```tsx
export default async function Page({ params }: { params: Promise<{ id: string }> }) {
  const { id } = await params;
  const userPromise = getUser(id);            // start now
  const configPromise = getConfig();          // start now, parallel
  const user = await userPromise;
  const orders = await getOrders(user.id);    // depends on user
  const config = await configPromise;
  return <Dashboard user={user} orders={orders} config={config} />;
}
```

## React `cache()` for Deduplication

```tsx
// lib/data.ts
import { cache } from 'react';
import { db } from '@/lib/db';

export const getUser = cache(async (id: string) =>
  db.user.findUnique({ where: { id } }),
);

// Two Server Components calling getUser('123') in one request → one DB hit.
```

## `generateMetadata` for Dynamic SEO

```tsx
// app/products/[id]/page.tsx
import type { Metadata } from 'next';

export async function generateMetadata(
  { params }: { params: Promise<{ id: string }> },
): Promise<Metadata> {
  const { id } = await params;
  const product = await db.product.findUnique({ where: { id } });
  if (!product) return { title: 'Not found' };
  return {
    title: product.name,
    description: product.description,
    openGraph: { title: product.name, images: [product.imageUrl] },
    alternates: { canonical: `/products/${id}` },
  };
}
```

Static metadata: `export const metadata: Metadata = { title: 'About' }`.

## `generateStaticParams` (SSG for dynamic routes)

```tsx
export async function generateStaticParams() {
  const posts = await db.post.findMany({ select: { slug: true } });
  return posts.map((p) => ({ slug: p.slug }));
}

// Route segment config
export const dynamic = 'force-static';   // 'auto' | 'force-static' | 'force-dynamic' | 'error'
export const revalidate = 3600;           // seconds (ISR)
export const runtime = 'nodejs';          // 'nodejs' | 'edge'
```

## `fetch` Cache Options

```tsx
// Force cache (SSG)
fetch(url, { cache: 'force-cache' });

// Always fresh (SSR)
fetch(url, { cache: 'no-store' });

// ISR — revalidate every 60s
fetch(url, { next: { revalidate: 60 } });

// Tag-based on-demand revalidation
fetch(url, { next: { tags: ['posts'] } });
```

Revalidate from a Server Action:
```tsx
import { revalidatePath, revalidateTag } from 'next/cache';

revalidatePath('/posts');                 // path
revalidatePath('/posts', 'layout');       // entire layout
revalidateTag('posts');                   // all fetches tagged 'posts'
```

## Route Handlers (API)

```ts
// app/api/users/route.ts
import { NextRequest, NextResponse } from 'next/server';

export async function GET(request: NextRequest) {
  const users = await db.user.findMany();
  return NextResponse.json(users);
}

export async function POST(request: NextRequest) {
  const body = await request.json();
  const user = await db.user.create({ data: body });
  return NextResponse.json(user, { status: 201 });
}
```

## Parallel & Intercepting Routes

```tsx
// app/dashboard/layout.tsx
export default function Layout({
  children, analytics, team,
}: { children: React.ReactNode; analytics: React.ReactNode; team: React.ReactNode }) {
  return <>{children}{analytics}{team}</>;
}

// app/photos/[id]/page.tsx     → full page
// app/@modal/(.)photos/[id]/page.tsx  → modal (intercepted)
```

## Quick Reference

| File | Purpose |
|------|---------|
| `layout.tsx` | Persistent UI across routes |
| `page.tsx` | Route UI |
| `loading.tsx` | Suspense fallback |
| `error.tsx` | Error boundary (must be `'use client'`) |
| `template.tsx` | Re-mounted layout (analytics, transitions) |
| `not-found.tsx` | 404 UI |
| `route.ts` | API handler |
| `[slug]/page.tsx` | Dynamic segment (params is now `Promise<{ slug: string }>` in Next 15) |
| `[...slug]/page.tsx` | Catch-all |
| `[[...slug]]/page.tsx` | Optional catch-all |
| `(group)/page.tsx` | Route group, no URL segment |
| `@slot/page.tsx` | Parallel slot |
